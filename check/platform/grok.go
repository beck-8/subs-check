package platform

import (
	"io"
	"net/http"
	"strings"
	"time"
)

// CheckGrok 检测代理是否解锁 Grok (xAI)，返回 true 表示解锁
func CheckGrok(client *http.Client) (bool, error) {
	// 测试 URL：Grok 官网和聊天入口（避免 API 以防滥用）
	urls := []string{
		"https://grok.x.ai/",     // 主页
		"https://x.com/i/grok",   // X 集成入口
	}

	// 复用 client 的 Transport，设置超时
	testClient := &http.Client{
		Transport:     client.Transport,
		Timeout:       10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // 捕获重定向
		},
	}

	for _, url := range urls {
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/91.0.4472.124 Safari/537.36")

		resp, err := testClient.Do(req)
		if err != nil {
			continue
		}
		defer resp.Body.Close()

		// 读取少量 body 检查关键词（避免大响应）
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		body := strings.ToLower(string(bodyBytes))

		// 成功条件：200 OK 且包含 Grok/xAI 关键词
		if resp.StatusCode == http.StatusOK && (strings.Contains(body, "grok") || strings.Contains(body, "xai")) {
			return true, nil
		}

		// 失败条件：重定向到登录/地区不可用，或 403/429
		if resp.StatusCode == http.StatusFound || resp.StatusCode == http.StatusForbidden {
			location := resp.Header.Get("Location")
			if strings.Contains(location, "login") || strings.Contains(location, "unavailable") || strings.Contains(location, "region") {
				return false, nil
			}
		}
	}

	return false, nil // 默认失败
}
