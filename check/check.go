package check

import (
	"context"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/beck-8/subs-check/check/platform"
	"github.com/beck-8/subs-check/config"
	proxyutils "github.com/beck-8/subs-check/proxy"
	"github.com/juju/ratelimit"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
)

// Result 存储节点检测结果
type Result struct {
	Proxy      map[string]any
	Openai     bool
	Youtube    string
	Netflix    bool
	Google     bool
	Cloudflare bool
	Disney     bool
	Gemini     bool
	TikTok     string
	IP         string
	IPRisk     string
	Country    string
}

// ProxyChecker 处理代理检测的主要结构体
type ProxyChecker struct {
	results     []Result
	proxyCount  int
	threadCount int
	progress    int32
	available   int32
	resultChan  chan Result
	tasks       chan map[string]any
}

var Progress atomic.Uint32
var Available atomic.Uint32
var ProxyCount atomic.Uint32
var TotalBytes atomic.Uint64

var ForceClose atomic.Bool

var Bucket *ratelimit.Bucket

// NewProxyChecker 创建新的检测器实例
func NewProxyChecker(proxyCount int) *ProxyChecker {
	threadCount := config.GlobalConfig.Concurrent
	if proxyCount < threadCount {
		threadCount = proxyCount
	}

	ProxyCount.Store(uint32(proxyCount))
	return &ProxyChecker{
		results:     make([]Result, 0),
		proxyCount:  proxyCount,
		threadCount: threadCount,
		resultChan:  make(chan Result),
		tasks:       make(chan map[string]any, 1),
	}
}

// Check 执行代理检测的主函数
func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()
	ForceClose.Store(false)

	ProxyCount.Store(0)
	Available.Store(0)
	Progress.Store(0)

	TotalBytes.Store(0)

	// 之前好的节点前置
	var proxies []map[string]any
	if config.GlobalConfig.KeepSuccessProxies {
		slog.Info(fmt.Sprintf("添加之前测试成功的节点，数量: %d", len(config.GlobalProxies)))
		proxies = append(proxies, config.GlobalProxies...)
	}
	tmp, err := proxyutils.GetProxies()
	if err != nil {
		return nil, fmt.Errorf("获取节点失败: %w", err)
	}
	proxies = append(proxies, tmp...)
	slog.Info(fmt.Sprintf("获取节点数量: %d", len(proxies)))

	// 重置全局节点
	config.GlobalProxies = make([]map[string]any, 0)

	proxies = proxyutils.DeduplicateProxies(proxies)
	slog.Info(fmt.Sprintf("去重后节点数量: %d", len(proxies)))

	checker := NewProxyChecker(len(proxies))
	return checker.run(proxies)
}

// Run 运行检测流程
func (pc *ProxyChecker) run(proxies []map[string]any) ([]Result, error) {
	if config.GlobalConfig.TotalSpeedLimit != 0 {
		Bucket = ratelimit.NewBucketWithRate(float64(config.GlobalConfig.TotalSpeedLimit*1024*1024), int64(config.GlobalConfig.TotalSpeedLimit*1024*1024/10))
	} else {
		Bucket = ratelimit.NewBucketWithRate(float64(math.MaxInt64), int64(math.MaxInt64))
	}

	slog.Info("开始检测节点")
	slog.Info("当前参数", "timeout", config.GlobalConfig.Timeout, "concurrent", config.GlobalConfig.Concurrent, "enable-speedtest", config.GlobalConfig.SpeedTestUrl != "", "min-speed", config.GlobalConfig.MinSpeed, "download-timeout", config.GlobalConfig.DownloadTimeout, "download-mb", config.GlobalConfig.DownloadMB, "total-speed-limit", config.GlobalConfig.TotalSpeedLimit)

	done := make(chan bool)
	if config.GlobalConfig.PrintProgress {
		go pc.showProgress(done)
	}
	var wg sync.WaitGroup
	// 启动工作线程
	for i := 0; i < pc.threadCount; i++ {
		wg.Add(1)
		go pc.worker(&wg)
	}

	// 发送任务
	go pc.distributeProxies(proxies)
	slog.Debug(fmt.Sprintf("发送任务: %d", len(proxies)))

	// 收集结果 - 添加一个 WaitGroup 来等待结果收集完成
	var collectWg sync.WaitGroup
	collectWg.Add(1)
	go func() {
		pc.collectResults()
		collectWg.Done()
	}()

	wg.Wait()
	close(pc.resultChan)

	// 等待结果收集完成
	collectWg.Wait()
	// 等待进度条显示完成
	time.Sleep(100 * time.Millisecond)

	if config.GlobalConfig.PrintProgress {
		done <- true
	}

	if config.GlobalConfig.SuccessLimit > 0 && pc.available >= config.GlobalConfig.SuccessLimit {
		slog.Warn(fmt.Sprintf("达到节点数量限制: %d", config.GlobalConfig.SuccessLimit))
	}
	slog.Info(fmt.Sprintf("可用节点数量: %d", len(pc.results)))
	slog.Info(fmt.Sprintf("测试总消耗流量: %.3fGB", float64(TotalBytes.Load())/1024/1024/1024))

	// 检查订阅成功率并发出警告
	pc.checkSubscriptionSuccessRate(proxies)

	return pc.results, nil
}

// worker 处理单个代理检测的工作线程
func (pc *ProxyChecker) worker(wg *sync.WaitGroup) {
	defer wg.Done()
	for proxy := range pc.tasks {
		if result := pc.checkProxy(proxy); result != nil {
			pc.resultChan <- *result
		}
		pc.incrementProgress()
	}
}

// checkProxy 检测单个代理
func (pc *ProxyChecker) checkProxy(proxy map[string]any) *Result {
	res := &Result{
		Proxy: proxy,
	}

	if os.Getenv("SUB_CHECK_SKIP") != "" {
		// slog.Debug(fmt.Sprintf("跳过检测代理: %v", proxy["name"]))
		return res
	}

	httpClient := CreateClient(proxy)
	if httpClient == nil {
		slog.Debug(fmt.Sprintf("创建代理Client失败: %v", proxy["name"]))
		return nil
	}
	defer httpClient.Close()

	cloudflare, err := platform.CheckCloudflare(httpClient.Client)
	if err != nil || !cloudflare {
		return nil
	}

	google, err := platform.CheckGoogle(httpClient.Client)
	if err != nil || !google {
		return nil
	}

	var speed int
	if config.GlobalConfig.SpeedTestUrl != "" {
		speed, _, err = platform.CheckSpeed(httpClient.Client, Bucket)
		if err != nil || speed < config.GlobalConfig.MinSpeed {
			return nil
		}
	}

	if config.GlobalConfig.MediaCheck {
		// 遍历需要检测的平台
		for _, plat := range config.GlobalConfig.Platforms {
			switch plat {
			case "openai":
				if ok, _ := platform.CheckOpenai(httpClient.Client); ok {
					res.Openai = true
				}
			case "youtube":
				if region, _ := platform.CheckYoutube(httpClient.Client); region != "" {
					res.Youtube = region
				}
			case "netflix":
				if ok, _ := platform.CheckNetflix(httpClient.Client); ok {
					res.Netflix = true
				}
			case "disney":
				if ok, _ := platform.CheckDisney(httpClient.Client); ok {
					res.Disney = true
				}
			case "gemini":
				if ok, _ := platform.CheckGemini(httpClient.Client); ok {
					res.Gemini = true
				}
			case "iprisk":
				country, ip := proxyutils.GetProxyCountry(httpClient.Client)
				if ip == "" {
					break
				}
				res.IP = ip
				res.Country = country
				risk, err := platform.CheckIPRisk(httpClient.Client, ip)
				if err == nil {
					res.IPRisk = risk
				} else {
					// 失败的可能性高，所以放上日志
					slog.Debug(fmt.Sprintf("查询IP风险失败: %v", err))
				}
			case "tiktok":
				if region, _ := platform.CheckTikTok(httpClient.Client); region != "" {
					res.TikTok = region
				}
			}
		}
	}
	// 更新代理名称
	pc.updateProxyName(res, httpClient, speed)
	pc.incrementAvailable()
	return res
}

// updateProxyName 更新代理名称
func (pc *ProxyChecker) updateProxyName(res *Result, httpClient *ProxyClient, speed int) {
	// 以节点IP查询位置重命名节点
	if config.GlobalConfig.RenameNode {
		if res.Country != "" {
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(res.Country)
		} else {
			country, _ := proxyutils.GetProxyCountry(httpClient.Client)
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(country)
		}
	}

	name := res.Proxy["name"].(string)
	name = strings.TrimSpace(name)

	var tags []string
	// 获取速度
	if config.GlobalConfig.SpeedTestUrl != "" {
		name = regexp.MustCompile(`\s*\|(?:\s*[\d.]+[KM]B/s)`).ReplaceAllString(name, "")
		var speedStr string
		if speed < 1024 {
			speedStr = fmt.Sprintf("%dKB/s", speed)
		} else {
			speedStr = fmt.Sprintf("%.1fMB/s", float64(speed)/1024)
		}
		tags = append(tags, speedStr)
	}

	if config.GlobalConfig.MediaCheck {
		// 移除已有的标记（IPRisk和平台标记）
		name = regexp.MustCompile(`\s*\|(?:NF|D\+|GPT|GM|YT-[^|]+|TK-[^|]+|\d+%)`).ReplaceAllString(name, "")
	}

	// 按用户输入顺序定义
	for _, plat := range config.GlobalConfig.Platforms {
		switch plat {
		case "openai":
			if res.Openai {
				tags = append(tags, "GPT")
			}
		case "netflix":
			if res.Netflix {
				tags = append(tags, "NF")
			}
		case "disney":
			if res.Disney {
				tags = append(tags, "D+")
			}
		case "gemini":
			if res.Gemini {
				tags = append(tags, "GM")
			}
		case "iprisk":
			if res.IPRisk != "" {
				tags = append(tags, res.IPRisk)
			}
		case "youtube":
			if res.Youtube != "" {
				tags = append(tags, fmt.Sprintf("YT-%s", res.Youtube))
			}
		case "tiktok":
			if res.TikTok != "" {
				tags = append(tags, fmt.Sprintf("TK-%s", res.TikTok))
			}
		}
	}

	// 将所有标记添加到名称中
	if len(tags) > 0 {
		name += " |" + strings.Join(tags, "|")
	}

	res.Proxy["name"] = name

}

// showProgress 显示进度条
func (pc *ProxyChecker) showProgress(done chan bool) {
	for {
		select {
		case <-done:
			fmt.Println()
			return
		default:
			current := atomic.LoadInt32(&pc.progress)
			available := atomic.LoadInt32(&pc.available)

			if pc.proxyCount == 0 {
				time.Sleep(100 * time.Millisecond)
				break
			}

			// if 0/0 = NaN ,shoule panic
			percent := float64(current) / float64(pc.proxyCount) * 100
			fmt.Printf("\r进度: [%-50s] %.1f%% (%d/%d) 可用: %d",
				strings.Repeat("=", int(percent/2))+">",
				percent,
				current,
				pc.proxyCount,
				available)
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// 辅助方法
func (pc *ProxyChecker) incrementProgress() {
	atomic.AddInt32(&pc.progress, 1)
	Progress.Add(1)
}

func (pc *ProxyChecker) incrementAvailable() {
	atomic.AddInt32(&pc.available, 1)
	Available.Add(1)
}

// distributeProxies 分发代理任务
func (pc *ProxyChecker) distributeProxies(proxies []map[string]any) {
	for _, proxy := range proxies {
		if config.GlobalConfig.SuccessLimit > 0 && atomic.LoadInt32(&pc.available) >= config.GlobalConfig.SuccessLimit {
			break
		}
		if ForceClose.Load() {
			slog.Warn("收到强制关闭信号，停止派发任务")
			break
		}
		pc.tasks <- proxy
	}
	close(pc.tasks)
}

// collectResults 收集检测结果
func (pc *ProxyChecker) collectResults() {
	for result := range pc.resultChan {
		pc.results = append(pc.results, result)
	}
}

// checkSubscriptionSuccessRate 检查订阅成功率并发出警告
func (pc *ProxyChecker) checkSubscriptionSuccessRate(allProxies []map[string]any) {
	// 统计每个订阅的节点总数和成功数
	subStats := make(map[string]struct {
		total   int
		success int
	})

	// 统计所有节点的订阅来源
	for _, proxy := range allProxies {
		if subUrl, ok := proxy["subscription_url"].(string); ok {
			stats := subStats[subUrl]
			stats.total++
			subStats[subUrl] = stats
		}
	}

	// 统计成功节点的订阅来源
	for _, result := range pc.results {
		if result.Proxy != nil {
			if subUrl, ok := result.Proxy["subscription_url"].(string); ok {
				stats := subStats[subUrl]
				stats.success++
				subStats[subUrl] = stats
			}
			delete(result.Proxy, "subscription_url")
		}
	}

	// 检查成功率并发出警告
	for subUrl, stats := range subStats {
		if stats.total > 0 {
			successRate := float32(stats.success) / float32(stats.total)

			// 如果成功率低于x，发出警告
			if successRate < config.GlobalConfig.SuccessRate {
				slog.Warn(fmt.Sprintf("订阅成功率过低: %s", subUrl),
					"总节点数", stats.total,
					"成功节点数", stats.success,
					"成功占比", fmt.Sprintf("%.2f%%", successRate*100))
			} else {
				slog.Debug(fmt.Sprintf("订阅节点统计: %s", subUrl),
					"总节点数", stats.total,
					"成功节点数", stats.success,
					"成功占比", fmt.Sprintf("%.2f%%", successRate*100))
			}
		}
	}
}

// CreateClient creates and returns an http.Client with a Close function
type ProxyClient struct {
	*http.Client
	proxy     constant.Proxy
	Transport *StatsTransport
}

func CreateClient(mapping map[string]any) *ProxyClient {
	proxy, err := adapter.ParseProxy(mapping)
	if err != nil {
		slog.Debug(fmt.Sprintf("底层mihomo创建代理Client失败: %v", err))
		return nil
	}

	baseTransport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, err
			}
			var u16Port uint16
			if port, err := strconv.ParseUint(port, 10, 16); err == nil {
				u16Port = uint16(port)
			}
			return proxy.DialContext(ctx, &constant.Metadata{
				Host:    host,
				DstPort: u16Port,
			})
		},
		DisableKeepAlives: true,
	}

	statsTransport := &StatsTransport{
		Base: baseTransport,
	}
	return &ProxyClient{
		Client: &http.Client{
			Timeout:   time.Duration(config.GlobalConfig.Timeout) * time.Millisecond,
			Transport: statsTransport,
		},
		proxy:     proxy,
		Transport: statsTransport,
	}
}

// Close closes the proxy client and cleans up resources
// 防止底层库有一些泄露，所以这里手动关闭
func (pc *ProxyClient) Close() {
	if pc.Client != nil {
		pc.Client.CloseIdleConnections()
	}

	// 即使这里不关闭，底层GC的时候也会自动关闭
	if pc.proxy != nil {
		pc.proxy.Close()
	}
	pc.Client = nil

	if pc.Transport != nil {
		TotalBytes.Add(atomic.LoadUint64(&pc.Transport.BytesRead))
	}
	pc.Transport = nil
}

type countingReadCloser struct {
	io.ReadCloser
	counter *uint64
}

func (c *countingReadCloser) Read(p []byte) (int, error) {
	n, err := c.ReadCloser.Read(p)
	atomic.AddUint64(c.counter, uint64(n))
	return n, err
}

type StatsTransport struct {
	Base      http.RoundTripper
	BytesRead uint64
}

func (s *StatsTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := s.Base.RoundTrip(req)
	if err != nil {
		return nil, err
	}

	resp.Body = &countingReadCloser{
		ReadCloser: resp.Body,
		counter:    &s.BytesRead,
	}
	return resp, nil
}
