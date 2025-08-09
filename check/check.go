package check

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/beck-8/subs-check/assets"
	"github.com/beck-8/subs-check/check/platform"
	"github.com/beck-8/subs-check/config"
	proxyutils "github.com/beck-8/subs-check/proxy"
	"github.com/juju/ratelimit"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
	"github.com/oschwald/maxminddb-golang/v2"
)

// 预编译的正则表达式，避免重复编译
var (
	speedRegex    = regexp.MustCompile(`\s*\|(?:\s*[\d.]+[KM]B/s)`)
	platformRegex = regexp.MustCompile(`\s*\|(?:NF|D\+|GPT⁺|GPT|X|GM|YT-[^|]+|TK-[^|]+|\d+%)`)

	// Result对象池，用于复用Result对象
	resultPool = sync.Pool{
		New: func() any {
			return &Result{}
		},
	}
)

// Result 存储节点检测结果
type Result struct {
	Proxy          map[string]any
	Openai         bool
	OpenaiWeb      bool
	X              bool
	Youtube        string
	Netflix        bool
	Google         bool
	Cloudflare     bool
	Disney         bool
	Gemini         bool
	TikTok         string
	IP             string
	IPRisk         string
	Country        string
	CountryCodeTag string
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
		results:     make([]Result, 0, proxyCount/4), // 预分配容量，假设约25%的节点会成功
		proxyCount:  proxyCount,
		threadCount: threadCount,
		resultChan:  make(chan Result, threadCount*2), // 增加缓冲区大小
		tasks:       make(chan map[string]any, threadCount),
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

	// 并发进程外加载 MaxMind 数据库,避免频繁打开
	var geoDB *maxminddb.Reader
	var err error

	// 如果为""会自动使用 subs-check 内置数据库
	geoDB, err = assets.OpenMaxMindDB(config.GlobalConfig.MaxMindDBPath)

	if err != nil {
		slog.Debug(fmt.Sprintf("打开 MaxMind 数据库失败: %v", err))
		geoDB = nil
	}

	// 确保数据库在函数结束时关闭
	if geoDB != nil {
		defer func() {
			if err := geoDB.Close(); err != nil {
				slog.Debug(fmt.Sprintf("关闭 MaxMind 数据库失败: %v", err))
			}
		}()
	}

	slog.Info("开始检测节点")
	slog.Info("当前参数", "timeout", config.GlobalConfig.Timeout, "concurrent", config.GlobalConfig.Concurrent, "enable-speedtest", config.GlobalConfig.SpeedTestUrl != "", "min-speed", config.GlobalConfig.MinSpeed, "download-timeout", config.GlobalConfig.DownloadTimeout, "download-mb", config.GlobalConfig.DownloadMB, "total-speed-limit", config.GlobalConfig.TotalSpeedLimit, "drop-bad-cf-nodes", config.GlobalConfig.DropBadCfNodes)

	done := make(chan bool)
	if config.GlobalConfig.PrintProgress {
		go pc.showProgress(done)
	}
	var wg sync.WaitGroup
	// 启动工作线程
	for i := 0; i < pc.threadCount; i++ {
		wg.Add(1)
		go pc.worker(&wg, geoDB)
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

	// 保存结果副本，避免被cleanup清空
	results := make([]Result, len(pc.results))
	copy(results, pc.results)
	pc.results = nil

	// 清理内存
	pc.cleanup()

	return results, nil
}

// worker 处理单个代理检测的工作线程
func (pc *ProxyChecker) worker(wg *sync.WaitGroup, db *maxminddb.Reader) {
	defer wg.Done()
	for proxy := range pc.tasks {
		// 检查是否达到成功限制，如果达到则跳过当前任务
		if config.GlobalConfig.SuccessLimit > 0 && atomic.LoadInt32(&pc.available) >= config.GlobalConfig.SuccessLimit {
			pc.incrementProgress()
			continue
		}

		if result := pc.checkProxy(proxy, db); result != nil {
			// 将指针转换为值类型发送到channel
			pc.resultChan <- *result
			// 将对象放回池中
			resultPool.Put(result)
		}
		pc.incrementProgress()
	}
}

// checkProxy 检测单个代理
func (pc *ProxyChecker) checkProxy(proxy map[string]any, db *maxminddb.Reader) *Result {
	// 从对象池获取Result对象
	res := resultPool.Get().(*Result)
	// 重置Result对象
	*res = Result{
		Proxy: proxy,
	}

	if os.Getenv("SUB_CHECK_SKIP") != "" {
		// slog.Debug(fmt.Sprintf("跳过检测代理: %v", proxy["name"]))
		return res
	}

	httpClient := CreateClient(proxy)
	if httpClient == nil {
		slog.Debug(fmt.Sprintf("创建代理Client失败: %v", proxy["name"]))
		// 将对象放回池中
		resultPool.Put(res)
		return nil
	}
	defer httpClient.Close()

	gstatic, err := platform.CheckGstatic(httpClient.Client)
	if err != nil || !gstatic {
		slog.Debug(fmt.Sprintf("无法访问Gstatic: %v", proxy["name"]))
		// 将对象放回池中
		resultPool.Put(res)
		return nil
	}

	google, err := platform.CheckGoogle(httpClient.Client)
	if err != nil || !google {
		// 将对象放回池中
		resultPool.Put(res)
		return nil
	}

	if config.GlobalConfig.DropBadCfNodes {
		if cloudflare, _, _ := platform.CheckCloudflare(httpClient.Client); !cloudflare {
			// 节点可用，但无法访问cloudflare，说明是未正确设置proxyip的cf节点
			slog.Debug(fmt.Sprintf("%v 无法访问Cloudflare, 已丢弃", proxy["name"]))
			// 将对象放回池中
			resultPool.Put(res)
			return nil
		}
	}

	var speed int
	if config.GlobalConfig.SpeedTestUrl != "" {
		speed, _, err = platform.CheckSpeed(httpClient.Client, Bucket)
		if err != nil || speed < config.GlobalConfig.MinSpeed {
			// 将对象放回池中
			resultPool.Put(res)
			return nil
		}
	}

	if config.GlobalConfig.MediaCheck {
		cloudflare, _, _ := platform.CheckCloudflare(httpClient.Client)
		// 遍历需要检测的平台
		for _, plat := range config.GlobalConfig.Platforms {
			if cloudflare {
				// openAI和X都使用了 Cloudflare 的CDN
				switch plat {
				case "openai":
					cookiesOK, clientOK := platform.CheckOpenAI(httpClient.Client)
					if clientOK && cookiesOK {
						res.Openai = true
					} else if cookiesOK || clientOK {
						res.OpenaiWeb = true
					}
				case "x":
					ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
					defer cancel()
					loc, ip := platform.GetCFProxy(httpClient.Client, ctx, "https://x.com")
					if loc != "" && ip != "" {
						res.X = true
					}
				}
			}

			switch plat {
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
			case "tiktok":
				if region, _ := platform.CheckTikTok(httpClient.Client); region != "" {
					res.TikTok = region
				}
			case "iprisk":
				country, ip, countryCode_tag, _ := proxyutils.GetProxyCountry(httpClient.Client, db)
				if ip == "" {
					break
				}

				res.IP = ip
				res.Country = country
				res.CountryCodeTag = countryCode_tag

				risk, err := platform.CheckIPRisk(httpClient.Client, ip)
				if err == nil {
					res.IPRisk = risk
				} else {
					// 失败的可能性高，所以放上日志
					slog.Debug(fmt.Sprintf("查询IP风险失败: %v", err))
				}
			}
		}
	}
	// 更新代理名称
	pc.updateProxyName(res, httpClient, speed, db)
	pc.incrementAvailable()
	return res
}

// updateProxyName 更新代理名称
func (pc *ProxyChecker) updateProxyName(res *Result, httpClient *ProxyClient, speed int, db *maxminddb.Reader) {
	// 以节点IP查询位置重命名节点
	if config.GlobalConfig.RenameNode {
		if res.Country != "" {
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(res.Country, res.CountryCodeTag)
		} else {
			country, _, countryCode_tag, _ := proxyutils.GetProxyCountry(httpClient.Client, db)
			res.Proxy["name"] = config.GlobalConfig.NodePrefix + proxyutils.Rename(country, countryCode_tag)
		}
	}

	name := res.Proxy["name"].(string)
	name = strings.TrimSpace(name)

	var tags []string
	// 获取速度
	if config.GlobalConfig.SpeedTestUrl != "" {
		name = speedRegex.ReplaceAllString(name, "")
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
		name = platformRegex.ReplaceAllString(name, "")
	}

	// 按用户输入顺序定义
	for _, plat := range config.GlobalConfig.Platforms {
		switch plat {
		case "openai":
			if res.Openai {
				tags = append(tags, "GPT⁺")
			} else if res.OpenaiWeb {
				tags = append(tags, "GPT")
			}
		case "x":
			if res.X {
				tags = append(tags, "X")
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
				// TODO: 位置准确之后，除了CN之外，似乎没必要加后缀了
				tags = append(tags, fmt.Sprintf("YT-%s", res.Youtube))
			}
		case "tiktok":
			if res.TikTok != "" {
				tags = append(tags, fmt.Sprintf("TK-%s", res.TikTok))
			}
		}
	}

	if tag, ok := res.Proxy["sub_tag"].(string); ok && tag != "" {
		tags = append(tags, tag)
	}

	// 将所有标记添加到名称中，使用strings.Builder优化字符串拼接
	if len(tags) > 0 {
		var builder strings.Builder
		builder.WriteString(name)
		builder.WriteString("|")
		builder.WriteString(strings.Join(tags, "|"))
		res.Proxy["name"] = builder.String()
	} else {
		res.Proxy["name"] = name
	}
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
			fmt.Printf("\r进度: [%-42s] %.1f%% (%d/%d) 可用: %d",
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
			slog.Debug("达到成功节点数量限制，停止派发新任务")
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
		if subUrl, ok := proxy["sub_url"].(string); ok {
			stats := subStats[subUrl]
			stats.total++
			subStats[subUrl] = stats
		}
	}

	// 统计成功节点的订阅来源
	for _, result := range pc.results {
		if result.Proxy != nil {
			if subUrl, ok := result.Proxy["sub_url"].(string); ok {
				stats := subStats[subUrl]
				stats.success++
				subStats[subUrl] = stats
			}
			delete(result.Proxy, "sub_url")
			delete(result.Proxy, "sub_tag")
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

// cleanup 清理内存和资源
func (pc *ProxyChecker) cleanup() {
	// 清理结果切片，释放内存
	if len(pc.results) > 0 {
		// 清空结果切片，但保留容量以避免重新分配
		pc.results = pc.results[:0]
	}

	// 强制垃圾回收
	runtime.GC()

	// 释放操作系统内存
	debug.FreeOSMemory()
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
		// 清理Transport资源
		if pc.Transport.Base != nil {
			if transport, ok := pc.Transport.Base.(*http.Transport); ok {
				transport.CloseIdleConnections()
			}
		}
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
