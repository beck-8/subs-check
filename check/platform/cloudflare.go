package platform

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/metacubex/mihomo/common/convert"
)

var cfCdnAPIs = []string{
	"https://www.cloudflare.com",
	"https://api.ipify.org",
	"https://ip.122911.xyz",
	"https://iplark.com",
}

// 请求头，避免被 ban
func getCommonHeaders() map[string]string {
	return map[string]string{
		"User-Agent":      convert.RandUserAgent(),
		"Accept-Language": "en-US,en;q=0.5",
		// "Accept":             "*/*",
		"Sec-Ch-Ua":          "\"Chromium\";v=\"122\", \"Google Chrome\";v=\"122\", \"Not A(Brand\";v=\"99\"",
		"Sec-Ch-Ua-Mobile":   "?0",
		"Sec-Ch-Ua-Platform": "\"Windows\"",
		"Accept":             "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
		"Connection":         "close",
	}
}

// CheckCloudflare 检测能否访问 Cloudflare
func CheckCloudflare(httpClient *http.Client) (bool, string, string) {
	cfRelayLoc, cfRelayIP := FetchCFProxy(httpClient)

	if cfRelayLoc != "" && cfRelayIP != "" {
		slog.Debug(fmt.Sprintf("Cloudflare CDN 检测成功: loc=%s, ip=%s", cfRelayLoc, cfRelayIP))
		return true, cfRelayLoc, cfRelayIP
	}

	ok, err := checkCloudflareEndpoint(httpClient, "https://cloudflare.com", 200)
	if err == nil && ok {
		slog.Debug("Cloudflare 可达，但未获取到 loc/ip")
		return true, "", ""
	}

	return false, "", ""
}

// checkCloudflareEndpoint 检查 cloudflare.com 是否可访问
func checkCloudflareEndpoint(httpClient *http.Client, url string, expectedStatus int) (bool, error) {
	req, err := http.NewRequest("HEAD", url, nil)
	if err != nil {
		return false, err
	}

	// 设置请求头
	for key, value := range getCommonHeaders() {
		req.Header.Set(key, value)
	}

	// 忽略 TLS 错误
	transport := httpClient.Transport
	if transport == nil {
		transport = &http.Transport{}
	}
	if t, ok := transport.(*http.Transport); ok {
		sni := req.URL.Hostname()
		t.TLSClientConfig = &tls.Config{
			InsecureSkipVerify: true,
			ServerName:         sni,
		}
		httpClient.Transport = t
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		errStr := err.Error()

		// 检测是否为典型的 Cloudflare 拒绝自身加速请求的错误
		//  strings.Contains(errStr, "EOF") ||
		// 	strings.Contains(errStr, "tls:") ||
		// 	strings.Contains(errStr, "ws closed: 1005") ||
		// 	strings.Contains(errStr, "connection reset") ||

		if strings.Contains(errStr, "EOF") ||
			strings.Contains(errStr, "tls:") ||
			strings.Contains(errStr, "connection reset") {

			slog.Debug("Cloudflare 连接异常，但可能可以访问，暂时返回 true", "error", errStr)
			return true, nil
		}
		return false, err
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != expectedStatus {
		if resp.StatusCode == 403 {
			slog.Debug("放行状态码", "code", resp.StatusCode)
			return true, nil
		} else {
			slog.Warn("cloudflare.com 返回非预期状态码", "code", resp.StatusCode)
		}
		return false, nil
	}
	return true, nil
}

// FetchCFProxy：并发获取 Cloudflare 节点信息
func FetchCFProxy(httpClient *http.Client) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	type result struct {
		loc string
		ip  string
	}

	resultChan := make(chan result, 1)
	var once sync.Once

	var wg sync.WaitGroup

	wg.Add(len(cfCdnAPIs))

	for _, url := range cfCdnAPIs {
		go func(url string) {
			defer wg.Done()

			select {
			case <-ctx.Done():
				return
			default:
			}

			// 重试 2 次
			for i := 0; i < 2; i++ {
				select {
				case <-ctx.Done():
					return
				default:
					loc, ip := GetCFProxy(httpClient, ctx, url)
					if loc != "" && ip != "" {
						once.Do(func() {
							select {
							case resultChan <- result{loc, ip}:
								cancel()
							case <-ctx.Done():
								// 如果context已经被取消，直接返回
								return
							}
						})
						return
					}
				}
			}
		}(url)
	}

	// 等待所有goroutine完成或超时
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	select {
	case r := <-resultChan:
		return r.loc, r.ip
	case <-ctx.Done():
		return "", ""
	}
}

// GetCFProxy 通过cdn-cgi/trace 获取cloudflare cdn节点位置
func GetCFProxy(httpClient *http.Client, ctx context.Context, baseURL string) (string, string) {
	url := fmt.Sprintf("%s/cdn-cgi/trace", baseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", ""
	}

	for key, value := range getCommonHeaders() {
		req.Header.Set(key, value)
	}

	req.Header.Set("Origin", baseURL)
	req.Header.Set("Referer", baseURL)

	resp, err := httpClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return "", ""
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", ""
	}

	var loc, ip string
	for _, line := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(line, "loc=") {
			loc = strings.TrimPrefix(line, "loc=")
		}
		if strings.HasPrefix(line, "ip=") {
			ip = strings.TrimPrefix(line, "ip=")
		}
	}
	return loc, ip
}
