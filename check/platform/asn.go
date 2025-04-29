package platfrom

import (
	"compress/gzip"
	"compress/zlib"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// IPDataResponse 表示从ipdata.co返回的JSON数据结构
type IPDataResponse struct {
	IP      string `json:"ip"`
	IsEU    bool   `json:"is_eu"`
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country_name"`
	ASN     struct {
		ASN    string `json:"asn"`
		Name   string `json:"name"`
		Domain string `json:"domain"`
		Route  string `json:"route"`
		Type   string `json:"type"`
	} `json:"asn"`
}

// CheckASN 查询节点的ASN信息
func CheckASN(httpClient *http.Client) (string, error) {
	// 设置重试参数
	maxRetries := 3
	retryDelay := 2 * time.Second

	var lastErr error
	// 重试循环
	for retry := 0; retry < maxRetries; retry++ {
		if retry > 0 {
			// 非首次尝试，等待一段时间
			time.Sleep(retryDelay)
		}

		// 尝试获取ASN信息
		asn, err := tryGetASN(httpClient)
		if err == nil {
			// 成功获取ASN
			return asn, nil
		}

		// 记录错误并继续重试
		lastErr = err
	}

	// 所有重试都失败
	return "", fmt.Errorf("无法获取ASN信息 (已重试%d次): %v", maxRetries, lastErr)
}

// tryGetASN 尝试单次获取ASN信息
func tryGetASN(httpClient *http.Client) (string, error) {
	// 请求ipdata.co API
	req, err := http.NewRequest("GET", "https://api.ipdata.co/?api-key=eca677b284b3bac29eb72f5e496aa9047f26543605efe99ff2ce35c9", nil)
	if err != nil {
		return "", err
	}

	// 设置必要的请求头
	req.Header.Set("accept", "*/*")
	req.Header.Set("accept-encoding", "gzip, deflate") // 仅支持gzip和deflate
	req.Header.Set("accept-language", "en-US,en;q=0.9,th-TH;q=0.8,th;q=0.7,zh-CN;q=0.6,zh-TW;q=0.5,zh;q=0.4")
	req.Header.Set("cache-control", "no-cache")
	req.Header.Set("dnt", "1")
	req.Header.Set("origin", "https://ipdata.co")
	req.Header.Set("pragma", "no-cache")
	req.Header.Set("priority", "u=1, i")
	req.Header.Set("referer", "https://ipdata.co/")
	req.Header.Set("sec-ch-ua", "\"Google Chrome\";v=\"135\", \"Not-A.Brand\";v=\"8\", \"Chromium\";v=\"135\"")
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", "\"macOS\"")
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-site")
	req.Header.Set("user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		// 处理可能的压缩响应
		var reader io.ReadCloser
		switch strings.ToLower(resp.Header.Get("Content-Encoding")) {
		case "gzip":
			reader, err = gzip.NewReader(resp.Body)
			if err != nil {
				return "", fmt.Errorf("创建gzip reader失败: %v", err)
			}
			defer reader.Close()
		case "deflate":
			reader, err = zlib.NewReader(resp.Body)
			if err != nil {
				return "", fmt.Errorf("创建deflate reader失败: %v", err)
			}
			defer reader.Close()
		default:
			reader = resp.Body
		}

		// 读取响应内容
		body, err := io.ReadAll(reader)
		if err != nil {
			return "", fmt.Errorf("读取响应内容失败: %v", err)
		}

		var response IPDataResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return "", fmt.Errorf("解析JSON失败: %v", err)
		}

		// 提取ASN号码，格式为 "AS45629"
		if response.ASN.ASN != "" {
			// 去掉ASN前缀，只保留数字部分
			if len(response.ASN.ASN) > 2 && response.ASN.ASN[:2] == "AS" {
				return response.ASN.ASN[2:], nil
			}
			return response.ASN.ASN, nil
		}

		// ASN字段为空
		return "", errors.New("API返回数据中没有ASN信息")
	}

	// 处理非200响应
	return "", fmt.Errorf("API返回状态码: %d", resp.StatusCode)
}
