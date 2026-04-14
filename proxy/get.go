package proxies

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	u "net/url"
	"strings"
	"sync"
	"time"

	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/utils"
	"github.com/metacubex/mihomo/common/convert"
	"github.com/samber/lo"
	"gopkg.in/yaml.v3"
)

type subEntry struct {
	url    string
	source string
}

func GetProxies() ([]map[string]any, error) {

	// 解析本地与远程订阅清单
	subUrls, localNum, remoteNum := resolveSubUrls()
	slog.Info("订阅链接数量", "本地", localNum, "远程", remoteNum, "总计", len(subUrls))

	if len(config.GlobalConfig.NodeType) > 0 {
		slog.Info("只筛选用户设置的协议", "type", config.GlobalConfig.NodeType)
	}

	var wg sync.WaitGroup
	proxyChan := make(chan map[string]any, 1)                              // 缓冲通道存储解析的代理
	concurrentLimit := make(chan struct{}, config.GlobalConfig.Concurrent) // 限制并发数

	// 启动收集结果的协程
	var mihomoProxies []map[string]any
	done := make(chan struct{})
	go func() {
		for proxy := range proxyChan {
			mihomoProxies = append(mihomoProxies, proxy)
		}
		done <- struct{}{}
	}()

	// 启动工作协程
	for _, subUrl := range subUrls {
		wg.Add(1)
		concurrentLimit <- struct{}{} // 获取令牌

		go func(e subEntry) {
			defer wg.Done()
			defer func() { <-concurrentLimit }() // 释放令牌

			url := e.url
			data, err := GetDateFromSubs(url)
			if err != nil {
				slog.Error(fmt.Sprintf("获取订阅链接错误跳过 [来源:%s]: %v", e.source, err))
				return
			}

			var tag string
			if d, err := u.Parse(url); err == nil {
				tag = d.Fragment
			}

			var con map[string]any
			err = yaml.Unmarshal(data, &con)
			if err != nil {
				proxyList, err := convert.ConvertsV2Ray(data)
				if err != nil {
					slog.Error(fmt.Sprintf("解析proxy错误: %v", err), "url", url)
					return
				}
				slog.Debug(fmt.Sprintf("获取订阅链接: %s，有效节点数量: %d", url, len(proxyList)))
				for _, proxy := range proxyList {
					// 只测试指定协议
					if t, ok := proxy["type"].(string); ok {
						if len(config.GlobalConfig.NodeType) > 0 && !lo.Contains(config.GlobalConfig.NodeType, t) {
							continue
						}
					}

					// 为每个节点添加订阅链接来源信息和备注
					proxy["sub_url"] = url
					if tag != "" {
						proxy["sub_tag"] = tag
					}
					proxyChan <- proxy
				}
				return
			}

			proxyInterface, ok := con["proxies"]
			if !ok || proxyInterface == nil {
				slog.Error(fmt.Sprintf("订阅链接没有proxies: %s", url))
				return
			}

			proxyList, ok := proxyInterface.([]any)
			if !ok {
				return
			}
			slog.Debug(fmt.Sprintf("获取订阅链接: %s，有效节点数量: %d", url, len(proxyList)))
			for _, proxy := range proxyList {
				if proxyMap, ok := proxy.(map[string]any); ok {
					if t, ok := proxyMap["type"].(string); ok {
						// 只测试指定协议
						if len(config.GlobalConfig.NodeType) > 0 && !lo.Contains(config.GlobalConfig.NodeType, t) {
							continue
						}
						// 虽然支持mihomo支持下划线，但是这里为了规范，还是改成横杠
						// todo: 不知道后边还有没有这类问题
						switch t {
						case "hysteria2", "hy2":
							if _, ok := proxyMap["obfs_password"]; ok {
								proxyMap["obfs-password"] = proxyMap["obfs_password"]
								delete(proxyMap, "obfs_password")
							}
						}
					}
					// 为每个节点添加订阅链接来源信息和备注
					proxyMap["sub_url"] = url
					if tag != "" {
						proxyMap["sub_tag"] = tag
					}
					proxyChan <- proxyMap
				}
			}
		}(subEntry{url: utils.WarpUrl(subUrl.url), source: subUrl.source})
	}

	// 等待所有工作协程完成
	wg.Wait()
	close(proxyChan)
	<-done // 等待收集完成

	return mihomoProxies, nil
}

// from 3k
// resolveSubUrls 合并本地与远程订阅清单并去重
func resolveSubUrls() ([]subEntry, int, int) {
	// 计数
	var localNum, remoteNum int
	localNum = len(config.GlobalConfig.SubUrls)

	entries := make([]subEntry, 0, len(config.GlobalConfig.SubUrls))
	// 本地配置
	for _, u := range config.GlobalConfig.SubUrls {
		entries = append(entries, subEntry{url: u, source: "本地配置"})
	}

	// 远程清单
	if len(config.GlobalConfig.SubUrlsRemote) != 0 {
		for _, d := range config.GlobalConfig.SubUrlsRemote {
			if remote, err := fetchRemoteSubUrls(utils.WarpUrl(d)); err != nil {
				slog.Warn("获取远程订阅清单失败，已忽略", "err", err)
			} else {
				remoteNum += len(remote)
				for _, u := range remote {
					entries = append(entries, subEntry{url: u, source: d})
				}
			}
		}
	}

	// 规范化与去重
	seen := make(map[string]struct{}, len(entries))
	out := make([]subEntry, 0, len(entries))
	for _, e := range entries {
		s := strings.TrimSpace(e.url)
		if s == "" || strings.HasPrefix(s, "#") { // 跳过空行与注释
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, subEntry{url: s, source: e.source})
	}
	return out, localNum, remoteNum
}

// fetchRemoteSubUrls 从远程地址读取订阅URL清单
// 支持两种格式：
// 1) 纯文本，按换行分隔，支持以 # 开头的注释与空行
// 2) YAML/JSON 的字符串数组
func fetchRemoteSubUrls(listURL string) ([]string, error) {
	if listURL == "" {
		return nil, errors.New("empty list url")
	}
	data, err := GetDateFromSubs(listURL)
	if err != nil {
		return nil, err
	}

	// 优先尝试解析为字符串数组（YAML/JSON兼容）
	var arr []string
	if err := yaml.Unmarshal(data, &arr); err == nil && len(arr) > 0 {
		return arr, nil
	}

	// 回退为按行解析
	res := make([]string, 0, 16)
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		res = append(res, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// 订阅链接中获取数据
func GetDateFromSubs(subUrl string) ([]byte, error) {
	maxRetries := config.GlobalConfig.SubUrlsReTry
	// 重试间隔
	retryInterval := config.GlobalConfig.SubUrlsRetryInterval
	if retryInterval == 0 {
		retryInterval = 1
	}
	// 超时时间
	timeout := config.GlobalConfig.SubUrlsTimeout
	if timeout == 0 {
		timeout = 10
	}
	var lastErr error

	client := &http.Client{
		Timeout: time.Duration(timeout) * time.Second,
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	for i := 0; i < maxRetries; i++ {
		if i > 0 {
			time.Sleep(time.Duration(retryInterval) * time.Second)
		}

		req, err := http.NewRequest("GET", subUrl, nil)
		if err != nil {
			lastErr = err
			continue
		}

		if config.GlobalConfig.SubUrlsGetUA == "random" {
			req.Header.Set("User-Agent", convert.RandUserAgent())
		} else {
			req.Header.Set("User-Agent", config.GlobalConfig.SubUrlsGetUA)
		}

		resp, err := client.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode != 200 {
			resp.Body.Close()
			lastErr = fmt.Errorf("订阅链接: %s 返回状态码: %d", subUrl, resp.StatusCode)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("读取订阅链接: %s 数据错误: %v", subUrl, err)
			continue
		}
		return body, nil
	}

	return nil, fmt.Errorf("重试%d次后失败: %v", maxRetries, lastErr)
}
