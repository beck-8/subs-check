package check

import (
	"context"
	"fmt"
	"log/slog"
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
	OpenaiWeb  bool
	Youtube    string
	Netflix    bool
	Google     bool
	Cloudflare bool
	Disney     bool
	Gemini     string
	TikTok     string
	IP         string
	IPRisk     string
	Country    string
}

// aliveResult 存活检测通过的中间结果
type aliveResult struct {
	Proxy map[string]any
}

// speedResult 测速通过的中间结果
type speedResult struct {
	Proxy map[string]any
	Speed int
}

// ProxyChecker 处理代理检测的主要结构体
type ProxyChecker struct {
	results    []Result
	proxyCount int
	progress   int32
	available  int32
}

var Progress atomic.Uint32
var Available atomic.Uint32
var ProxyCount atomic.Uint32
var TotalBytes atomic.Uint64
var Phase atomic.Uint32 // 0=idle, 1=alive, 2=speed, 3=media

var ForceClose atomic.Bool
var progressPaused atomic.Bool
var progressRendered atomic.Bool

var Bucket *ratelimit.Bucket

// effectiveConcurrency 计算阶段实际并发数
func effectiveConcurrency(phaseConcurrency, fallback, itemCount int) int {
	c := phaseConcurrency
	if c <= 0 {
		c = fallback
	}
	if itemCount < c {
		c = itemCount
	}
	if c < 1 {
		c = 1
	}
	return c
}

// Check 执行代理检测的主函数
func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()
	ForceClose.Store(false)

	ProxyCount.Store(0)
	Available.Store(0)
	Progress.Store(0)
	Phase.Store(0)

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

	checker := &ProxyChecker{
		results: make([]Result, 0),
	}
	return checker.run(proxies)
}

// run 运行三阶段检测流程
func (pc *ProxyChecker) run(proxies []map[string]any) ([]Result, error) {
	if config.GlobalConfig.TotalSpeedLimit != 0 {
		Bucket = ratelimit.NewBucketWithRate(float64(config.GlobalConfig.TotalSpeedLimit*1024*1024), int64(config.GlobalConfig.TotalSpeedLimit*1024*1024/10))
	} else {
		Bucket = ratelimit.NewBucketWithRate(float64(math.MaxInt64), int64(math.MaxInt64))
	}

	slog.Info("开始检测节点")
	slog.Info("当前参数", "timeout", config.GlobalConfig.Timeout, "concurrent", config.GlobalConfig.Concurrent, "speed-concurrent", config.GlobalConfig.SpeedConcurrent, "media-concurrent", config.GlobalConfig.MediaConcurrent, "enable-speedtest", config.GlobalConfig.SpeedTestUrl != "", "min-speed", config.GlobalConfig.MinSpeed, "download-timeout", config.GlobalConfig.DownloadTimeout, "download-mb", config.GlobalConfig.DownloadMB, "total-speed-limit", config.GlobalConfig.TotalSpeedLimit)

	done := make(chan bool)
	if config.GlobalConfig.PrintProgress {
		go pc.showProgress(done)
	}

	// === Phase 1: 测活 ===
	Phase.Store(1)
	pc.resetPhaseCounters(len(proxies))

	hasSpeedTest := config.GlobalConfig.SpeedTestUrl != ""
	aliveConcurrency := effectiveConcurrency(config.GlobalConfig.Concurrent, config.GlobalConfig.Concurrent, len(proxies))
	slog.Info(fmt.Sprintf("阶段1-测活: 节点数=%d, 并发数=%d", len(proxies), aliveConcurrency))
	resumeProgress()
	// 没有测速阶段时，SuccessLimit 在测活阶段生效
	aliveResults := pc.runAlivePhase(proxies, aliveConcurrency, !hasSpeedTest)
	pauseProgress()
	slog.Info(fmt.Sprintf("存活节点数量: %d", len(aliveResults)))

	// === Phase 2: 测速 (可选) ===
	var speedResults []speedResult
	if hasSpeedTest && len(aliveResults) > 0 {
		Phase.Store(2)
		pc.resetPhaseCounters(len(aliveResults))

		speedConcurrency := effectiveConcurrency(config.GlobalConfig.SpeedConcurrent, config.GlobalConfig.Concurrent, len(aliveResults))
		slog.Info(fmt.Sprintf("阶段2-测速: 节点数=%d, 并发数=%d", len(aliveResults), speedConcurrency))
		resumeProgress()
		speedResults = pc.runSpeedPhase(aliveResults, speedConcurrency)
		pauseProgress()
		slog.Info(fmt.Sprintf("测速通过节点数量: %d", len(speedResults)))
	} else {
		// 无测速：直接转换
		for _, a := range aliveResults {
			speedResults = append(speedResults, speedResult{Proxy: a.Proxy, Speed: 0})
		}
	}

	// === Phase 3: 流媒体检测 + 重命名（不淘汰节点，ForceClose 也需执行以保留已有结果） ===
	if len(speedResults) > 0 {
		Phase.Store(3)
		pc.resetPhaseCounters(len(speedResults))

		mediaConcurrency := effectiveConcurrency(config.GlobalConfig.MediaConcurrent, config.GlobalConfig.Concurrent, len(speedResults))
		slog.Info(fmt.Sprintf("阶段3-流媒体+重命名: 节点数=%d, 并发数=%d", len(speedResults), mediaConcurrency))
		resumeProgress()
		pc.results = pc.runMediaPhase(speedResults, mediaConcurrency)
		pauseProgress()
	}

	if config.GlobalConfig.PrintProgress {
		done <- true
	}

	Phase.Store(0)

	if config.GlobalConfig.SuccessLimit > 0 && pc.available >= config.GlobalConfig.SuccessLimit {
		slog.Warn(fmt.Sprintf("达到节点数量限制: %d", config.GlobalConfig.SuccessLimit))
	}
	slog.Info(fmt.Sprintf("可用节点数量: %d", len(pc.results)))
	slog.Info(fmt.Sprintf("测试总消耗流量: %.3fGB", float64(TotalBytes.Load())/1024/1024/1024))

	// 检查订阅成功率并发出警告
	pc.checkSubscriptionSuccessRate(proxies)

	// 应用节点名称过滤规则
	filteredResults := FilterResults(pc.results)

	return filteredResults, nil
}

// runAlivePhase 执行测活阶段
// applySuccessLimit: 当没有测速阶段时，SuccessLimit 在此阶段生效
func (pc *ProxyChecker) runAlivePhase(proxies []map[string]any, concurrency int, applySuccessLimit bool) []aliveResult {
	var results []aliveResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	tasks := make(chan map[string]any, 1)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for proxy := range tasks {
				if r := pc.checkAlive(proxy); r != nil {
					mu.Lock()
					results = append(results, *r)
					mu.Unlock()
					pc.incrementAvailable()
				}
				pc.incrementProgress()
			}
		}()
	}

	go func() {
		for _, proxy := range proxies {
			if applySuccessLimit && config.GlobalConfig.SuccessLimit > 0 && atomic.LoadInt32(&pc.available) >= config.GlobalConfig.SuccessLimit {
				slog.Warn(fmt.Sprintf("达到存活成功数量限制: %d，停止派发", config.GlobalConfig.SuccessLimit))
				break
			}
			if ForceClose.Load() {
				slog.Warn("收到强制关闭信号，停止派发任务")
				break
			}
			tasks <- proxy
		}
		close(tasks)
	}()

	wg.Wait()
	return results
}

// runSpeedPhase 执行测速阶段
// ForceClose 时未测速的节点以 Speed=0 直接加入结果，不丢弃
// SuccessLimit 时未测速的节点直接丢弃
func (pc *ProxyChecker) runSpeedPhase(alive []aliveResult, concurrency int) []speedResult {
	var results []speedResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	tasks := make(chan aliveResult, 1)
	var distributed int32
	var stoppedByForceClose atomic.Bool

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for a := range tasks {
				if r := pc.checkSpeed(a); r != nil {
					mu.Lock()
					results = append(results, *r)
					mu.Unlock()
					pc.incrementAvailable()
				}
				pc.incrementProgress()
			}
		}()
	}

	go func() {
		for i, a := range alive {
			if config.GlobalConfig.SuccessLimit > 0 && atomic.LoadInt32(&pc.available) >= config.GlobalConfig.SuccessLimit {
				slog.Warn(fmt.Sprintf("达到测速成功数量限制: %d，停止派发", config.GlobalConfig.SuccessLimit))
				atomic.StoreInt32(&distributed, int32(i))
				break
			}
			if ForceClose.Load() {
				slog.Warn("收到强制关闭信号，停止派发测速任务，未测速节点将直接保留")
				stoppedByForceClose.Store(true)
				atomic.StoreInt32(&distributed, int32(i))
				break
			}
			tasks <- a
			atomic.StoreInt32(&distributed, int32(i+1))
		}
		close(tasks)
	}()

	wg.Wait()

	// 仅 ForceClose 时保留未测速节点，SuccessLimit 时不保留
	if stoppedByForceClose.Load() {
		skipped := alive[atomic.LoadInt32(&distributed):]
		for _, a := range skipped {
			results = append(results, speedResult{Proxy: a.Proxy, Speed: 0})
		}
	}

	return results
}

// runMediaPhase 执行流媒体检测+重命名阶段
// ForceClose 时未检测的节点直接保留，不丢弃
func (pc *ProxyChecker) runMediaPhase(speed []speedResult, concurrency int) []Result {
	var results []Result
	var mu sync.Mutex
	var wg sync.WaitGroup
	tasks := make(chan speedResult, 1)
	var distributed int32

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for sr := range tasks {
				if r := pc.checkMedia(sr); r != nil {
					mu.Lock()
					results = append(results, *r)
					mu.Unlock()
				}
				pc.incrementProgress()
			}
		}()
	}

	go func() {
		for i, sr := range speed {
			if ForceClose.Load() {
				slog.Warn("收到强制关闭信号，停止派发流媒体任务，未检测节点将直接保留")
				atomic.StoreInt32(&distributed, int32(i))
				break
			}
			tasks <- sr
			atomic.StoreInt32(&distributed, int32(i+1))
		}
		close(tasks)
	}()

	wg.Wait()

	// 将未派发的节点直接加入结果（无流媒体标签和重命名）
	skipped := speed[atomic.LoadInt32(&distributed):]
	for _, sr := range skipped {
		results = append(results, Result{Proxy: sr.Proxy})
	}

	return results
}

// checkAlive 检测单个代理是否存活
func (pc *ProxyChecker) checkAlive(proxy map[string]any) *aliveResult {
	if os.Getenv("SUB_CHECK_SKIP") != "" {
		return &aliveResult{Proxy: proxy}
	}

	httpClient := CreateClient(proxy)
	if httpClient == nil {
		slog.Debug(fmt.Sprintf("创建代理Client失败: %v", proxy["name"]))
		return nil
	}
	defer httpClient.Close()

	alive, err := platform.CheckAlive(httpClient.Client)
	if err != nil || !alive {
		return nil
	}

	return &aliveResult{Proxy: proxy}
}

// checkSpeed 对存活代理执行测速
func (pc *ProxyChecker) checkSpeed(a aliveResult) *speedResult {
	httpClient := CreateClient(a.Proxy)
	if httpClient == nil {
		slog.Debug(fmt.Sprintf("创建代理Client失败: %v", a.Proxy["name"]))
		return nil
	}
	defer httpClient.Close()

	speed, _, err := platform.CheckSpeed(httpClient.Client, Bucket, httpClient.BytesRead)
	if err != nil || speed < config.GlobalConfig.MinSpeed {
		return nil
	}

	return &speedResult{Proxy: a.Proxy, Speed: speed}
}

// checkMedia 执行流媒体检测和重命名
// 此阶段不会丢弃节点，即使创建Client失败或流媒体检测失败，节点仍会保留
func (pc *ProxyChecker) checkMedia(sr speedResult) *Result {
	res := &Result{
		Proxy: sr.Proxy,
	}

	httpClient := CreateClient(sr.Proxy)
	if httpClient == nil {
		slog.Debug(fmt.Sprintf("创建代理Client失败，跳过流媒体检测: %v", sr.Proxy["name"]))
		// 仍然保留节点，仅跳过流媒体检测和重命名
		pc.incrementAvailable()
		return res
	}
	defer httpClient.Close()

	if config.GlobalConfig.MediaCheck {
		mediaTimeout := config.GlobalConfig.MediaCheckTimeout
		if mediaTimeout <= 0 {
			mediaTimeout = 10
		}
		mediaClient := &http.Client{
			Transport: httpClient.Client.Transport,
			Timeout:   time.Duration(mediaTimeout) * time.Second,
		}

		// 遍历需要检测的平台
		for _, plat := range config.GlobalConfig.Platforms {
			switch plat {
			case "openai":
				cookiesOK, clientOK := platform.CheckOpenAI(mediaClient)
				if clientOK && cookiesOK {
					res.Openai = true
				} else if cookiesOK || clientOK {
					res.OpenaiWeb = true
				}
			case "youtube":
				if region, _ := platform.CheckYoutube(mediaClient); region != "" {
					res.Youtube = region
				}
			case "netflix":
				if ok, _ := platform.CheckNetflix(mediaClient); ok {
					res.Netflix = true
				}
			case "disney":
				if ok, _ := platform.CheckDisney(mediaClient); ok {
					res.Disney = true
				}
			case "gemini":
				if region, _ := platform.CheckGemini(mediaClient); region != "" {
					res.Gemini = region
				}
			case "iprisk":
				country, ip := proxyutils.GetProxyCountry(mediaClient)
				if ip == "" {
					break
				}
				res.IP = ip
				res.Country = country
				risk, err := platform.CheckIPRisk(mediaClient, ip)
				if err == nil {
					res.IPRisk = risk
				} else {
					slog.Debug(fmt.Sprintf("查询IP风险失败: %v", err))
				}
			case "tiktok":
				if region, _ := platform.CheckTikTok(mediaClient); region != "" {
					res.TikTok = region
				}
			}
		}
	}

	// 更新代理名称
	pc.updateProxyName(res, httpClient, sr.Speed)
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
		name = regexp.MustCompile(`\s*\|(?:NF|D\+|GPT⁺|GPT|GM|YT-[^|]+|TK-[^|]+|\d+%)`).ReplaceAllString(name, "")
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
		case "netflix":
			if res.Netflix {
				tags = append(tags, "NF")
			}
		case "disney":
			if res.Disney {
				tags = append(tags, "D+")
			}
		case "gemini":
			if res.Gemini != "" {
				tags = append(tags, fmt.Sprintf("GM-%s", res.Gemini))
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

	if tag, ok := res.Proxy["sub_tag"].(string); ok && tag != "" {
		tags = append(tags, tag)
	}

	// 将所有标记添加到名称中
	if len(tags) > 0 {
		name += "|" + strings.Join(tags, "|")
	}

	res.Proxy["name"] = name

}

// showProgress 显示进度条
func (pc *ProxyChecker) showProgress(done chan bool) {
	type phaseInfo struct {
		name        string
		countLabel  string
	}
	phases := map[uint32]phaseInfo{
		1: {"测活", "存活"},
		2: {"测速", "通过"},
		3: {"流媒体", "完成"},
	}
	for {
		select {
		case <-done:
			fmt.Println()
			return
		default:
			if progressPaused.Load() {
				time.Sleep(100 * time.Millisecond)
				break
			}

			current := atomic.LoadInt32(&pc.progress)
			available := atomic.LoadInt32(&pc.available)

			if pc.proxyCount == 0 {
				time.Sleep(100 * time.Millisecond)
				break
			}

			info, ok := phases[Phase.Load()]
			if !ok {
				info = phaseInfo{"检测", "可用"}
			}

			// if 0/0 = NaN ,shoule panic
			percent := float64(current) / float64(pc.proxyCount) * 100
			progressRendered.Store(true)
			fmt.Printf("\r[%s] 进度: [%-45s] %.1f%% (%d/%d) %s: %d",
				info.name,
				strings.Repeat("=", int(percent/2))+">",
				percent,
				current,
				pc.proxyCount,
				info.countLabel,
				available)
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// pauseProgress 暂停进度条并换行，确保后续日志不会与进度条混在一行
func pauseProgress() {
	progressPaused.Store(true)
	time.Sleep(150 * time.Millisecond) // 等待进度条goroutine停止输出
	if progressRendered.Load() {
		fmt.Println() // 仅在进度条实际输出过时才换行
	}
}

// resumeProgress 恢复进度条显示
func resumeProgress() {
	progressRendered.Store(false)
	progressPaused.Store(false)
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

func (pc *ProxyChecker) resetPhaseCounters(count int) {
	ForceClose.Store(false)
	pc.proxyCount = count
	atomic.StoreInt32(&pc.progress, 0)
	atomic.StoreInt32(&pc.available, 0)
	Progress.Store(0)
	Available.Store(0)
	ProxyCount.Store(uint32(count))
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
			// 可以保持127.0.0.1:8199/sub/all.yaml中的节点tag
			if subTag, ok := result.Proxy["sub_tag"].(string); ok {
				if subTag == "" {
					delete(result.Proxy, "sub_tag")
				}
			}
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

// statsConn wraps net.Conn to count bytes read and apply rate limiting
type statsConn struct {
	net.Conn
	bytesRead *uint64
	bucket    *ratelimit.Bucket
}

func (c *statsConn) Read(b []byte) (n int, err error) {
	// 速度限制（全局）
	if c.bucket != nil {
		c.bucket.Wait(int64(len(b)))
	}

	n, err = c.Conn.Read(b)
	atomic.AddUint64(c.bytesRead, uint64(n))

	return n, err
}

// CreateClient creates and returns an http.Client with a Close function
type ProxyClient struct {
	*http.Client
	proxy     constant.Proxy
	BytesRead *uint64
}

func CreateClient(mapping map[string]any) *ProxyClient {
	proxy, err := adapter.ParseProxy(mapping)
	if err != nil {
		slog.Debug(fmt.Sprintf("底层mihomo创建代理Client失败: %v", err))
		return nil
	}

	var bytesRead uint64
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
			conn, err := proxy.DialContext(ctx, &constant.Metadata{
				Host:    host,
				DstPort: u16Port,
			})
			if err != nil {
				return nil, err
			}
			return &statsConn{
				Conn:      conn,
				bytesRead: &bytesRead,
				bucket:    Bucket,
			}, nil
		},
		DisableKeepAlives: true,
	}

	return &ProxyClient{
		Client: &http.Client{
			Timeout:   time.Duration(config.GlobalConfig.Timeout) * time.Millisecond,
			Transport: baseTransport,
		},
		proxy:     proxy,
		BytesRead: &bytesRead,
	}
}

// Close closes the proxy client and cleans up resources
// 防止底层库有一些泄露，所以这里手动关闭
func (pc *ProxyClient) Close() {
	if pc.Client != nil {
		pc.Client.CloseIdleConnections()
	}

	// 即使这里不关闭，底层GC的时候也会自动关闭
	// 这里及时的关闭，方便内存回收
	if pc.proxy != nil {
		pc.proxy.Close()
	}
	pc.Client = nil

	if pc.BytesRead != nil {
		TotalBytes.Add(atomic.LoadUint64(pc.BytesRead))
	}
}
