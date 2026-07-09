package utils

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/beck-8/subs-check/config"
)

// NotifyRequest 定义发送通知的请求结构
type NotifyRequest struct {
	URLs  string `json:"urls"`  // 通知目标的 URL（如 mailto://、discord://）
	Body  string `json:"body"`  // 通知内容
	Title string `json:"title"` // 通知标题（可选）
}

// Notify 发送通知
func Notify(request NotifyRequest) error {
	// 构建请求体
	body, err := json.Marshal(request)
	if err != nil {
		return fmt.Errorf("构建请求体失败: %w", err)
	}

	// 发送请求
	resp, err := http.Post(config.GlobalConfig.AppriseApiServer, "application/json", bytes.NewBuffer(body))
	if err != nil {
		return fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("通知失败，状态码: %d, 响应: %s", resp.StatusCode, string(body))
	}

	return nil
}

// dingTalkRequest 钉钉机器人消息请求结构
type dingTalkRequest struct {
	MsgType  string              `json:"msgtype"`
	Markdown dingTalkMarkdownMsg `json:"markdown"`
}

type dingTalkMarkdownMsg struct {
	Title string `json:"title"`
	Text  string `json:"text"`
}

// dingTalkResponse 钉钉 API 响应结构
type dingTalkResponse struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
}

// SendDingTalk 直接发送钉钉机器人消息
func SendDingTalk(title, body string) error {
	webhook := config.GlobalConfig.DingTalkWebhook
	secret := config.GlobalConfig.DingTalkSecret

	// 如果配置了加签密钥，计算签名并拼入 URL
	if secret != "" {
		timestamp := fmt.Sprintf("%d", time.Now().UnixMilli())
		stringToSign := timestamp + "\n" + secret

		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(stringToSign))
		sign := url.QueryEscape(base64.StdEncoding.EncodeToString(mac.Sum(nil)))

		if strings.Contains(webhook, "?") {
			webhook += "&timestamp=" + timestamp + "&sign=" + sign
		} else {
			webhook += "?timestamp=" + timestamp + "&sign=" + sign
		}
	}

	// 构造 Markdown 消息
	msg := dingTalkRequest{
		MsgType: "markdown",
		Markdown: dingTalkMarkdownMsg{
			Title: title,
			Text:  fmt.Sprintf("### %s\n\n%s", title, body),
		},
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("构建钉钉请求体失败: %w", err)
	}

	resp, err := http.Post(webhook, "application/json", bytes.NewBuffer(payload))
	if err != nil {
		return fmt.Errorf("发送钉钉请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("读取钉钉响应失败: %w", err)
	}

	var result dingTalkResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return fmt.Errorf("解析钉钉响应失败: %w", err)
	}

	if result.ErrCode != 0 {
		return fmt.Errorf("钉钉返回错误: code=%d, msg=%s", result.ErrCode, result.ErrMsg)
	}

	return nil
}

func SendNotify(length int) {
	notifyBody := fmt.Sprintf("✅ 可用节点：%d\n\n🕒 %s", length, GetCurrentTime())
	notifyTitle := config.GlobalConfig.NotifyTitle

	// Apprise 通知
	if config.GlobalConfig.AppriseApiServer != "" {
		if len(config.GlobalConfig.RecipientUrl) == 0 {
			slog.Error("没有配置通知目标")
		} else {
			for _, u := range config.GlobalConfig.RecipientUrl {
				request := NotifyRequest{
					URLs:  u,
					Body:  fmt.Sprintf("✅ 可用节点：%d\n🕒 %s", length, GetCurrentTime()),
					Title: notifyTitle,
				}
				var err error
				for i := 0; i < config.GlobalConfig.SubUrlsReTry; i++ {
					err = Notify(request)
					if err == nil {
						slog.Info(fmt.Sprintf("%s 通知发送成功", strings.SplitN(u, "://", 2)[0]))
						break
					}
				}
				if err != nil {
					slog.Error(fmt.Sprintf("%s 发送通知失败: %v", strings.SplitN(u, "://", 2)[0], err))
				}
			}
		}
	}

	// 钉钉直接推送
	if config.GlobalConfig.DingTalkWebhook != "" {
		var err error
		for i := 0; i < config.GlobalConfig.SubUrlsReTry; i++ {
			err = SendDingTalk(notifyTitle, notifyBody)
			if err == nil {
				slog.Info("钉钉通知发送成功")
				break
			}
		}
		if err != nil {
			slog.Error(fmt.Sprintf("钉钉通知发送失败: %v", err))
		}
	}
}

func GetCurrentTime() string {
	return time.Now().Format("2006-01-02 15:04:05")
}

