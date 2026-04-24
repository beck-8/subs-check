package check

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"os"
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
	Openai     *platform.OpenAIResult
	Youtube    string
	Netflix    *platform.NetflixResult
	Google     bool
	Cloudflare bool
	Disney     bool
	Gemini     string
	TikTok     string
	Claude     string
	Spotify    string
	IP         string
	IPRisk     string
	Country    string
	Speed      int // KB/s, 0 表示未测速或测速未通过
}

// aliveResult 存活检测通过的中间结果
type aliveResult struct {
	Proxy map[string]any
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
var Phase atomic.Uint32 // 0=idle, 1=alive, 2=media, 3=speed

// PhaseResult 保存单个阶段的最终结果
type PhaseResult struct {
	Available uint32 `json:"available"`
	Total     uint32 `json:"total"`
}

// PhaseResults 保存各阶段最终结果，供前端展示历史数据
var PhaseResults [4]atomic.Pointer[PhaseResult] // index 1-3 对应三个阶段

func SavePhaseResult(phase int, available, total uint32) {
	if phase >= 1 && phase <= 3 {
		PhaseResults[phase].Store(&PhaseResult{Available: available, Total: total})
	}
}

func GetPhaseResult(phase int) *PhaseResult {
	if phase >= 1 && phase <= 3 {
		return PhaseResults[phase].Load()
	}
	return nil
}

func ResetPhaseResults() {
	for i := 1; i <= 3; i++ {
		PhaseResults[i].Store(nil)
	}
}

var progressPaused atomic.Bool
var progressRendered atomic.Bool

// activeCancelMu guards activeCancel.
var activeCancelMu sync.Mutex

// activeCancel cancels the currently running phase. nil when idle or
// between phases; the per-phase dispatcher installs and clears it.
var activeCancel context.CancelFunc

// RequestCancel aborts the currently running check phase, if any.
// Safe to call from any goroutine; no-op when idle.
// Phases installed after this call are unaffected (per-phase scope,
// matching the pre-context ForceClose-reset-between-phases behaviour).
func RequestCancel() {
	activeCancelMu.Lock()
	defer activeCancelMu.Unlock()
	if activeCancel != nil {
		activeCancel()
	}
}

// installPhaseCancel registers cancel as the active phase canceller
// and returns a cleanup closure that cancels and clears it.
// Call the returned closure from a defer in the phase function.
func installPhaseCancel(cancel context.CancelFunc) func() {
	activeCancelMu.Lock()
	activeCancel = cancel
	activeCancelMu.Unlock()
	return func() {
		activeCancelMu.Lock()
		activeCancel = nil
		activeCancelMu.Unlock()
		cancel()
	}
}

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

	ProxyCount.Store(0)
	Available.Store(0)
	Progress.Store(0)
	Phase.Store(0)

	TotalBytes.Store(0)

	// keep-days 历史节点前置
	var proxies []map[string]any
	if len(config.GlobalProxies) > 0 {
		slog.Info(fmt.Sprintf("添加历史待测节点，数量: %d", len(config.GlobalProxies)))
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

// run 运行三阶段检测流程:测活 → 流媒体+重命名 → filter → 测速
func (pc *ProxyChecker) run(proxies []map[string]any) ([]Result, error) {
	if config.GlobalConfig.TotalSpeedLimit != 0 {
		Bucket = ratelimit.NewBucketWithRate(float64(config.GlobalConfig.TotalSpeedLimit*1024*1024), int64(config.GlobalConfig.TotalSpeedLimit*1024*1024/10))
	} else {
		Bucket = ratelimit.NewBucketWithRate(float64(math.MaxInt64), int64(math.MaxInt64))
	}

	slog.Info("开始检测节点")
	slog.Info("当前参数", "timeout", config.GlobalConfig.Timeout, "concurrent", config.GlobalConfig.Concurrent, "speed-concurrent", config.GlobalConfig.SpeedConcurrent, "media-concurrent", config.GlobalConfig.MediaConcurrent, "enable-speedtest", config.GlobalConfig.SpeedTestUrl != "", "min-speed", config.GlobalConfig.MinSpeed, "download-timeout", config.GlobalConfig.DownloadTimeout, "download-mb", config.GlobalConfig.DownloadMB, "total-speed-limit", config.GlobalConfig.TotalSpeedLimit)

	ResetPhaseResults()

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
	SavePhaseResult(1, Available.Load(), ProxyCount.Load())
	slog.Info(fmt.Sprintf("存活节点数量: %d", len(aliveResults)))

	// === Phase 2: 流媒体检测 + 国家查询 ===
	var mediaResults []Result
	if len(aliveResults) > 0 {
		Phase.Store(2)
		pc.resetPhaseCounters(len(aliveResults))

		mediaConcurrency := effectiveConcurrency(config.GlobalConfig.MediaConcurrent, config.GlobalConfig.Concurrent, len(aliveResults))
		slog.Info(fmt.Sprintf("阶段2-流媒体+重命名: 节点数=%d, 并发数=%d", len(aliveResults), mediaConcurrency))
		resumeProgress()
		mediaResults = pc.runMediaPhase(aliveResults, mediaConcurrency)
		pauseProgress()
		SavePhaseResult(2, Available.Load(), ProxyCount.Load())
		slog.Info(fmt.Sprintf("流媒体阶段节点数量: %d", len(mediaResults)))
	}

	// === Filter: 在测速之前过滤 ===
	filteredResults := FilterResults(mediaResults)

	// === Phase 3: 测速 (可选) ===
	if hasSpeedTest && len(filteredResults) > 0 {
		Phase.Store(3)
		pc.resetPhaseCounters(len(filteredResults))

		speedConcurrency := effectiveConcurrency(config.GlobalConfig.SpeedConcurrent, config.GlobalConfig.Concurrent, len(filteredResults))
		slog.Info(fmt.Sprintf("阶段3-测速: 节点数=%d, 并发数=%d", len(filteredResults), speedConcurrency))
		resumeProgress()
		pc.results = pc.runSpeedPhase(filteredResults, speedConcurrency)
		pauseProgress()
		SavePhaseResult(3, Available.Load(), ProxyCount.Load())
		slog.Info(fmt.Sprintf("测速通过节点数量: %d", len(pc.results)))
	} else {
		// 无测速：过滤后直接作为最终结果
		pc.results = filteredResults
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

	return pc.results, nil
}

// runAlivePhase 执行测活阶段
// applySuccessLimit: 当没有测速阶段时，SuccessLimit 在此阶段生效
// 采用分槽法保序:worker 写入预分配的固定下标,最终按 proxies 输入顺序 flatten,
// 保证输出与 GetProxies 的订阅顺序一致(本地订阅在前)。
func (pc *ProxyChecker) runAlivePhase(proxies []map[string]any, concurrency int, applySuccessLimit bool) []aliveResult {
	type aliveTask struct {
		idx   int
		proxy map[string]any
	}
	out := make([]*aliveResult, len(proxies))
	var wg sync.WaitGroup
	tasks := make(chan aliveTask, 1)

	// Per-phase cancellation scope. RequestCancel only affects the
	// currently installed phase; later phases start with a fresh context.
	ctx, cancel := context.WithCancel(context.Background())
	defer installPhaseCancel(cancel)()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				if r := pc.checkAlive(t.proxy); r != nil {
					out[t.idx] = r
					pc.incrementAvailable()
				}
				pc.incrementProgress()
			}
		}()
	}

	go func() {
		for i, proxy := range proxies {
			if applySuccessLimit && config.GlobalConfig.SuccessLimit > 0 && atomic.LoadInt32(&pc.available) >= config.GlobalConfig.SuccessLimit {
				slog.Warn(fmt.Sprintf("达到存活成功数量限制: %d，停止派发", config.GlobalConfig.SuccessLimit))
				break
			}
			if ctx.Err() != nil {
				slog.Warn("收到强制关闭信号，停止派发任务")
				break
			}
			tasks <- aliveTask{idx: i, proxy: proxy}
		}
		close(tasks)
	}()

	wg.Wait()

	results := make([]aliveResult, 0, len(out))
	for _, r := range out {
		if r != nil {
			results = append(results, *r)
		}
	}
	return results
}

// runSpeedPhase 执行测速阶段。
// 入参是已经通过 filter 的 Result 集合。
// 通过 min-speed 的节点填充 Result.Speed;未通过的节点被丢弃。
// 收到取消信号或 SuccessLimit 时未测速的节点都直接丢弃,
// 避免和已测速节点混在一起造成"有的有速度标签有的没有"的乱象。
// 采用分槽法保序:worker 写入预分配的固定下标,最终按 in 输入顺序 flatten。
func (pc *ProxyChecker) runSpeedPhase(in []Result, concurrency int) []Result {
	type speedTask struct {
		idx int
		r   Result
	}
	out := make([]*Result, len(in))
	var wg sync.WaitGroup
	tasks := make(chan speedTask, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer installPhaseCancel(cancel)()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				if updated := pc.checkSpeed(t.r); updated != nil {
					out[t.idx] = updated
					pc.incrementAvailable()
				}
				pc.incrementProgress()
			}
		}()
	}

	go func() {
		for i, r := range in {
			if config.GlobalConfig.SuccessLimit > 0 && atomic.LoadInt32(&pc.available) >= config.GlobalConfig.SuccessLimit {
				slog.Warn(fmt.Sprintf("达到测速成功数量限制: %d，停止派发", config.GlobalConfig.SuccessLimit))
				break
			}
			if ctx.Err() != nil {
				slog.Warn("收到强制关闭信号，停止派发测速任务，未测速节点将丢弃")
				break
			}
			tasks <- speedTask{idx: i, r: r}
		}
		close(tasks)
	}()

	wg.Wait()

	results := make([]Result, 0, len(out))
	for _, r := range out {
		if r != nil {
			results = append(results, *r)
		}
	}
	return results
}

// runMediaPhase 执行流媒体检测 + 国家查询阶段。
// 不会修改 proxy["name"];检测结果写入 Result 的结构化字段。
// 收到取消信号时未派发的节点直接丢弃,只输出已完成检测的节点,
// 避免和已检测节点混在一起造成"有的有标签有的没有"的乱象。
// 采用分槽法保序:worker 写入预分配的固定下标,最终按 alive 输入顺序 flatten。
func (pc *ProxyChecker) runMediaPhase(alive []aliveResult, concurrency int) []Result {
	type mediaTask struct {
		idx int
		a   aliveResult
	}
	out := make([]*Result, len(alive))
	var wg sync.WaitGroup
	tasks := make(chan mediaTask, 1)

	ctx, cancel := context.WithCancel(context.Background())
	defer installPhaseCancel(cancel)()

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range tasks {
				out[t.idx] = pc.checkMedia(t.a)
				pc.incrementProgress()
			}
		}()
	}

	go func() {
		for i, a := range alive {
			if ctx.Err() != nil {
				slog.Warn("收到强制关闭信号，停止派发流媒体任务，未检测节点将丢弃")
				break
			}
			tasks <- mediaTask{idx: i, a: a}
		}
		close(tasks)
	}()

	wg.Wait()

	results := make([]Result, 0, len(out))
	for _, r := range out {
		if r != nil {
			results = append(results, *r)
		}
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
		return nil
	}
	defer httpClient.Close()

	alive, err := platform.CheckAlive(httpClient.Client)
	if err != nil || !alive {
		return nil
	}

	return &aliveResult{Proxy: proxy}
}

// checkSpeed 对已有的 Result 执行测速。
// 通过 min-speed 的节点填充 r.Speed 并返回;未通过的返回 nil。
// 不修改 proxy["name"]。
func (pc *ProxyChecker) checkSpeed(r Result) *Result {
	if os.Getenv("SUB_CHECK_SKIP") != "" {
		r.Speed = 0
		return &r
	}

	httpClient := CreateClient(r.Proxy)
	if httpClient == nil {
		return nil
	}
	defer httpClient.Close()

	speed, _, err := platform.CheckSpeed(httpClient.Client, Bucket, httpClient.BytesRead)
	if err != nil || speed < config.GlobalConfig.MinSpeed {
		return nil
	}

	r.Speed = speed
	return &r
}

// checkMedia 执行流媒体检测和必要的国家查询。
// 不会丢弃节点,不会修改 proxy["name"];检测结果写入 Result 的结构化字段。
func (pc *ProxyChecker) checkMedia(a aliveResult) *Result {
	res := &Result{Proxy: a.Proxy}

	if os.Getenv("SUB_CHECK_SKIP") != "" {
		pc.incrementAvailable()
		return res
	}

	httpClient := CreateClient(a.Proxy)
	if httpClient == nil {
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

		// 并行检测所有平台
		var mediaWg sync.WaitGroup
		for _, plat := range config.GlobalConfig.Platforms {
			switch plat {
			case "openai":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					res.Openai = platform.CheckOpenAI(mediaClient)
				}()
			case "youtube":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					if region, _ := platform.CheckYoutube(mediaClient); region != "" {
						res.Youtube = region
					}
				}()
			case "netflix":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					nf, _ := platform.CheckNetflix(mediaClient)
					res.Netflix = nf
				}()
			case "disney":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					if ok, _ := platform.CheckDisney(mediaClient); ok {
						res.Disney = true
					}
				}()
			case "gemini":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					if region, _ := platform.CheckGemini(mediaClient); region != "" {
						res.Gemini = region
					}
				}()
			case "claude":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					if region, _ := platform.CheckClaude(mediaClient); region != "" {
						res.Claude = region
					}
				}()
			case "spotify":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					if region, _ := platform.CheckSpotify(mediaClient); region != "" {
						res.Spotify = region
					}
				}()
			case "iprisk":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					country, ip := proxyutils.GetProxyCountry(mediaClient)
					if ip == "" {
						return
					}
					res.IP = ip
					res.Country = country
					risk, err := platform.CheckIPRisk(mediaClient, ip)
					if err == nil {
						res.IPRisk = risk
					} else {
						slog.Debug(fmt.Sprintf("查询IP风险失败: %v", err))
					}
				}()
			case "tiktok":
				mediaWg.Add(1)
				go func() {
					defer mediaWg.Done()
					if region, _ := platform.CheckTikTok(mediaClient); region != "" {
						res.TikTok = region
					}
				}()
			}
		}
		mediaWg.Wait()
	}

	// 如果没有通过 iprisk 得到 Country，而 RenameNode 开启，则显式查一次国家
	if res.Country == "" && config.GlobalConfig.RenameNode {
		country, _ := proxyutils.GetProxyCountry(httpClient.Client)
		res.Country = country
	}

	pc.incrementAvailable()
	return res
}

// showProgress 显示进度条
func (pc *ProxyChecker) showProgress(done chan bool) {
	type phaseInfo struct {
		name       string
		countLabel string
	}
	phases := map[uint32]phaseInfo{
		1: {"测活", "存活"},
		2: {"流媒体+重命名", "完成"},
		3: {"测速", "通过"},
	}
	for {
		select {
		case <-done:
			// pauseProgress 已经在阶段结束时把 \r 行收尾换行了,
			// 这里只在还有未收尾的进度行时才补换行,避免在已经干净的输出后多插一个空行
			if progressRendered.Load() {
				fmt.Println()
			}
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
		fmt.Println()                 // 仅在进度条实际输出过时才换行
		progressRendered.Store(false) // 标记换行已收尾,避免后续 done 信号重复换行
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
	// Cancellation is scoped per-phase via installPhaseCancel, no reset needed here.
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
		slog.Debug("创建mihomo Client失败", "proxy", mapping["name"], "err", err)
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
	// 某些底层传输协议的 Close 可能阻塞，超时后放弃等待交由 GC 回收
	if pc.proxy != nil {
		proxy := pc.proxy
		done := make(chan struct{})
		go func() {
			proxy.Close()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			slog.Debug(fmt.Sprintf("关闭代理连接超时，交由GC回收: %v", proxy))
		}
	}
	pc.Client = nil

	if pc.BytesRead != nil {
		TotalBytes.Add(atomic.LoadUint64(pc.BytesRead))
	}
}
