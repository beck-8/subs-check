package check

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"log/slog"

	"github.com/beck-8/subs-check/check/platfrom"
	"github.com/beck-8/subs-check/config"
	proxyutils "github.com/beck-8/subs-check/proxy"
	"github.com/metacubex/mihomo/adapter"
	"github.com/metacubex/mihomo/constant"
	"github.com/pion/stun"
)

// Result 存储节点检测结果
type Result struct {
	Proxy      map[string]any
	Openai     bool
	Youtube    bool
	Netflix    bool
	Google     bool
	Cloudflare bool
	Disney     bool
	UDPResult  UDPCheckResult // 新增: 存储 UDP 检查结果
}

// UDPCheckResult 存储 UDP 检查的详细结果
type UDPCheckResult struct {
	Supported  bool   // UDP 是否支持
	ExternalIP string // 通过 STUN 获取的外部 IP
	Error      error  // 检查过程中发生的错误
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

// NewProxyChecker 创建新的检测器实例
func NewProxyChecker(proxyCount int) *ProxyChecker {
	threadCount := config.GlobalConfig.Concurrent
	if proxyCount < threadCount {
		threadCount = proxyCount
	}

	return &ProxyChecker{
		results:     make([]Result, 0),
		proxyCount:  proxyCount,
		threadCount: threadCount,
		resultChan:  make(chan Result),
		tasks:       make(chan map[string]any, proxyCount),
	}
}

// Check 执行代理检测的主函数
func Check() ([]Result, error) {
	proxyutils.ResetRenameCounter()

	proxies, err := proxyutils.GetProxies()
	if err != nil {
		return nil, fmt.Errorf("获取节点失败: %w", err)
	}
	slog.Info(fmt.Sprintf("获取节点数量: %d", len(proxies)))

	if config.GlobalConfig.KeepSuccessProxies {
		slog.Info(fmt.Sprintf("添加之前测试成功的节点，数量: %d", len(config.GlobalProxies)))
		proxies = append(proxies, config.GlobalProxies...)
	}
	// 重置全局节点
	config.GlobalProxies = make([]map[string]any, 0)

	proxies = proxyutils.DeduplicateProxies(proxies)
	slog.Info(fmt.Sprintf("去重后节点数量: %d", len(proxies)))

	checker := NewProxyChecker(len(proxies))
	return checker.run(proxies)
}

// Run 运行检测流程
func (pc *ProxyChecker) run(proxies []map[string]any) ([]Result, error) {
	slog.Info("开始检测节点")
	slog.Info(fmt.Sprintf("启动工作线程: %d", pc.threadCount))

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
	slog.Info(fmt.Sprintf("可用节点数量: %d", len(pc.results)))
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

	// 基础连通性检查 (Cloudflare & Google)
	cloudflare, err := platfrom.CheckCloudflare(httpClient.Client)
	if err != nil || !cloudflare {
		slog.Debug("Cloudflare 检查失败", "proxy", proxy["name"], "error", err)
		return nil
	}

	google, err := platfrom.CheckGoogle(httpClient.Client)
	if err != nil || !google {
		slog.Debug("Google 检查失败", "proxy", proxy["name"], "error", err)
		return nil
	}
	res.Cloudflare = cloudflare
	res.Google = google

	// 流媒体检查 (如果启用)
	if config.GlobalConfig.MediaCheck {
		openai, _ := platfrom.CheckOpenai(httpClient.Client)
		youtube, _ := platfrom.CheckYoutube(httpClient.Client)
		netflix, _ := platfrom.CheckNetflix(httpClient.Client)
		disney, _ := platfrom.CheckDisney(httpClient.Client)
		res.Openai = openai
		res.Youtube = youtube
		res.Netflix = netflix
		res.Disney = disney
	}

	// 测速 (如果启用)
	var speed int
	if config.GlobalConfig.SpeedTestUrl != "" {
		speed, err = platfrom.CheckSpeed(httpClient.Client)
		if err != nil || speed < config.GlobalConfig.MinSpeed {
			slog.Debug("测速失败或速度过低", "proxy", proxy["name"], "speed", speed, "error", err)
			return nil
		}
	}

	// UDP 检查 (如果启用)
	if config.GlobalConfig.UDPCheck {
		res.UDPResult = checkUDP(proxy)
	}

	// 更新代理名称 (包含 UDP 标志)
	pc.updateProxyName(res, speed)
	pc.incrementAvailable()
	return res
}

// updateProxyName 更新代理名称
func (pc *ProxyChecker) updateProxyName(res *Result, speed int) {
	// 从 res.Proxy["name"] 开始，避免使用未定义的 proxyMap
	proxyNameValue, ok := res.Proxy["name"]
	if !ok {
		proxyNameValue = "unknown"
	}
	initialProxyName := fmt.Sprintf("%v", proxyNameValue)
	currentProxyName := initialProxyName // 作为基础名称

	// 检查IP风险 (如果启用)
	var ipRisk string
	if config.GlobalConfig.IPRiskCheck {
		ipRiskHttpClient := CreateClient(res.Proxy)
		if ipRiskHttpClient == nil {
			slog.Debug("为 IP 风险检查创建代理 Client 失败", "proxy", currentProxyName)
		} else {
			defer ipRiskHttpClient.Close()
			_, ip := proxyutils.GetProxyCountry(ipRiskHttpClient.Client)
			if ip != "" {
				risk, err := platfrom.CheckIPRisk(ipRiskHttpClient.Client, ip)
				if err == nil {
					ipRisk = risk
				} else {
					slog.Debug("查询 IP 欺诈失败", "proxy", currentProxyName, "ip", ip, "error", err)
				}
			} else {
				slog.Debug("获取 IP 地址失败，无法检查风险", "proxy", currentProxyName)
			}
		}
	}

	// 以节点IP查询位置重命名节点 (如果启用)
	if config.GlobalConfig.RenameNode {
		renameHttpClient := CreateClient(res.Proxy)
		if renameHttpClient == nil {
			slog.Debug("为重命名创建代理 Client 失败", "proxy", currentProxyName)
		} else {
			defer renameHttpClient.Close()
			country, _ := proxyutils.GetProxyCountry(renameHttpClient.Client)
			if country == "" {
				country = "未识别"
			}
			// 使用重命名后的国家作为新的基础名称
			res.Proxy["name"] = proxyutils.Rename(country)
		}
	}

	// 添加速度
	if config.GlobalConfig.SpeedTestUrl != "" {
		var speedStr string
		if speed < 1024 {
			speedStr = fmt.Sprintf("%dKB/s", speed)
		} else {
			speedStr = fmt.Sprintf("%.1fMB/s", float64(speed)/1024)
		}
		// 防止重复添加速度
		res.Proxy["name"] = strings.Split(strings.TrimSpace(res.Proxy["name"].(string)), " | ⬇️ ")[0] + " | ⬇️ " + speedStr
	}

	// 添加IP风险标志
	if config.GlobalConfig.IPRiskCheck && ipRisk != "" {
		res.Proxy["name"] = res.Proxy["name"].(string) + " |" + ipRisk
	}

	// 添加 UDP 标志
	if config.GlobalConfig.UDPCheck && res.UDPResult.Supported {
		if config.GlobalConfig.UDPFlagText != "" {
			res.Proxy["name"] = strings.ReplaceAll(strings.TrimSpace(res.Proxy["name"].(string)), "|UDP", "") + "|" + config.GlobalConfig.UDPFlagText
		}
	}

	// 添加流媒体标志
	if config.GlobalConfig.MediaCheck {
		if res.Netflix {
			res.Proxy["name"] = strings.ReplaceAll(strings.TrimSpace(res.Proxy["name"].(string)), "|Netflix", "") + "|Netflix"
		}
		if res.Disney {
			res.Proxy["name"] = strings.ReplaceAll(strings.TrimSpace(res.Proxy["name"].(string)), "|Disney", "") + "|Disney"
		}
		if res.Youtube {
			res.Proxy["name"] = strings.ReplaceAll(strings.TrimSpace(res.Proxy["name"].(string)), "|Youtube", "") + "|Youtube"
		}
		if res.Openai {
			res.Proxy["name"] = strings.ReplaceAll(strings.TrimSpace(res.Proxy["name"].(string)), "|Openai", "") + "|Openai"
		}
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
}

func (pc *ProxyChecker) incrementAvailable() {
	atomic.AddInt32(&pc.available, 1)
}

// distributeProxies 分发代理任务
func (pc *ProxyChecker) distributeProxies(proxies []map[string]any) {
	for _, proxy := range proxies {
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

// CreateClient creates and returns an http.Client with a Close function
type ProxyClient struct {
	*http.Client
}

func CreateClient(mapping map[string]any) *ProxyClient {
	proxy, err := adapter.ParseProxy(mapping)
	if err != nil {
		slog.Debug(fmt.Sprintf("底层mihomo创建代理Client失败: %v", err))
		return nil
	}

	transport := &http.Transport{
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
		IdleConnTimeout:   time.Duration(config.GlobalConfig.Timeout) * time.Millisecond,
		DisableKeepAlives: true, // 短连接，减少资源占用
	}

	return &ProxyClient{
		Client: &http.Client{
			Timeout:   time.Duration(config.GlobalConfig.Timeout) * time.Millisecond,
			Transport: transport,
		},
	}
}

// Close closes the proxy client and cleans up resources
// 防止底层库有一些泄露，所以这里手动关闭
func (pc *ProxyClient) Close() {
	if pc.Client != nil {
		pc.CloseIdleConnections()
	}
	pc.Client = nil // 明确置 nil，便于 GC
}

// stunConnectionWrapper 包装 net.PacketConn 以实现 stun.Connection 接口
type stunConnectionWrapper struct {
	net.PacketConn
	remoteAddr net.Addr // 存储目标 STUN 服务器地址
	localAddr  net.Addr // 存储本地地址 (PacketConn 的 LocalAddr)
}

// Read 实现 stun.Connection 接口的 Read 方法
// STUN 通常在 UDP 上运行，Read 不太适用，但接口需要它。
// 我们尝试从 PacketConn 读取，但这可能阻塞或不按预期工作。
// 更可靠的方式可能是 stun.Client 直接使用 WriteTo/ReadFrom，
// 但 NewClient 需要 stun.Connection。
// 这个实现可能不完美，需要测试。
func (c *stunConnectionWrapper) Read(b []byte) (int, error) {
	// PacketConn 的 ReadFrom 更适合 UDP，但接口需要 Read
	// 尝试 ReadFrom 并丢弃来源地址，这可能不是最优的
	n, _, err := c.PacketConn.ReadFrom(b)
	if err != nil {
		// 可能需要处理特定错误，例如超时
		slog.Debug("stunConnectionWrapper Read (via ReadFrom) error", "error", err)
	}
	return n, err
}

// Write 实现 stun.Connection 接口的 Write 方法
// 将数据写入到存储的目标地址
func (c *stunConnectionWrapper) Write(b []byte) (int, error) {
	return c.PacketConn.WriteTo(b, c.remoteAddr)
}

// LocalAddr 实现 stun.Connection 接口的 LocalAddr 方法
func (c *stunConnectionWrapper) LocalAddr() net.Addr {
	return c.localAddr
}

// RemoteAddr 实现 stun.Connection 接口的 RemoteAddr 方法
func (c *stunConnectionWrapper) RemoteAddr() net.Addr {
	return c.remoteAddr
}

// Close 实现 stun.Connection 接口的 Close 方法
func (c *stunConnectionWrapper) Close() error {
	return c.PacketConn.Close()
}

// SetDeadline 实现 stun.Connection 接口的 SetDeadline 方法
func (c *stunConnectionWrapper) SetDeadline(t time.Time) error {
	return c.PacketConn.SetDeadline(t)
}

// SetReadDeadline 实现 stun.Connection 接口的 SetReadDeadline 方法
func (c *stunConnectionWrapper) SetReadDeadline(t time.Time) error {
	return c.PacketConn.SetReadDeadline(t)
}

// SetWriteDeadline 实现 stun.Connection 接口的 SetWriteDeadline 方法
func (c *stunConnectionWrapper) SetWriteDeadline(t time.Time) error {
	return c.PacketConn.SetWriteDeadline(t)
}

// checkUDP 通过代理使用 STUN 检测 UDP 连通性
func checkUDP(proxyMap map[string]any) (result UDPCheckResult) {
	defer func() {
		if r := recover(); r != nil {
			// 捕获到了 panic
			proxyNameValue, _ := proxyMap["name"]
			proxyName := fmt.Sprintf("%v", proxyNameValue)
			slog.Error("检测 UDP 时发生 Panic", "proxy", proxyName, "panic_value", r)
			// 获取 panic 时的堆栈信息
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			slog.Error("Panic Stack Trace", "stack", string(buf[:n]))
			// 设置返回结果为失败
			result.Supported = false
			result.Error = fmt.Errorf("panic during UDP check: %v", r)
			// 命名返回值 result 会被返回
		}
	}()

	result = UDPCheckResult{Supported: false}
	proxyNameValue, _ := proxyMap["name"]
	proxyName := fmt.Sprintf("%v", proxyNameValue)
	slog.Info("开始 UDP 检查", "proxy", proxyName, "stun", config.GlobalConfig.StunServer)

	// 1. 解析 STUN 服务器 host 和 port
	stunHost, stunPortStr, err := net.SplitHostPort(config.GlobalConfig.StunServer)
	if err != nil {
		slog.Error("解析 STUN 服务器地址失败", "proxy", proxyName, "server", config.GlobalConfig.StunServer, "error", err)
		result.Error = fmt.Errorf("解析 STUN 地址失败: %w", err)
		return result
	}
	slog.Debug("解析后的 STUN 服务器信息", "proxy", proxyName, "original", config.GlobalConfig.StunServer, "host", stunHost, "portStr", stunPortStr)

	stunPort, err := strconv.ParseUint(stunPortStr, 10, 16)
	if err != nil {
		slog.Error("解析 STUN 服务器端口失败", "proxy", proxyName, "port", stunPortStr, "error", err)
		result.Error = fmt.Errorf("解析 STUN 端口失败: %w", err)
		return result
	}

	// 2. 创建 Mihomo 代理适配器
	proxyAdapter, err := adapter.ParseProxy(proxyMap)
	if err != nil {
		slog.Error("创建 Mihomo 代理适配器失败 (UDP Check)", "proxy", proxyName, "error", err)
		result.Error = fmt.Errorf("创建 Mihomo 代理失败: %w", err)
		return result
	}

	// 3. 准备 DialUDP 的 Metadata
	metadata := &constant.Metadata{
		Host:    stunHost,
		DstPort: uint16(stunPort),
	}

	// 4. 通过代理进行 UDP 拨号
	slog.Debug("Dialing UDP via proxy", "proxy", proxyName, "target", config.GlobalConfig.StunServer)
	packetConn, err := proxyAdapter.DialUDP(metadata)
	if err != nil {
		slog.Info("通过代理进行 UDP 拨号失败", "proxy", proxyName, "stun", config.GlobalConfig.StunServer, "error", err)
		result.Error = fmt.Errorf("代理 UDP 拨号失败: %w", err)
		return result
	}
	defer packetConn.Close()
	slog.Debug("通过代理 UDP 拨号成功", "proxy", proxyName)

	// 5. 解析 STUN 服务器的 UDP 地址
	remoteUDPAddr, err := net.ResolveUDPAddr("udp", config.GlobalConfig.StunServer)
	if err != nil {
		slog.Error("解析 STUN 服务器 UDP 地址失败", "proxy", proxyName, "server", config.GlobalConfig.StunServer, "error", err)
		result.Error = fmt.Errorf("解析 STUN UDP 地址失败: %w", err)
		return result
	}

	// 6. 创建 stunConnectionWrapper
	stunConn := &stunConnectionWrapper{
		PacketConn: packetConn,
		remoteAddr: remoteUDPAddr,
		localAddr:  packetConn.LocalAddr(), // 获取实际的本地地址
	}

	// 7. 使用包装后的连接创建 STUN 客户端
	stunClient, err := stun.NewClient(stunConn)
	if err != nil {
		slog.Error("创建 STUN 客户端失败", "proxy", proxyName, "error", err)
		result.Error = fmt.Errorf("创建 STUN 客户端失败: %w", err)
		return result
	}
	defer stunClient.Close() // Close 会调用 stunConn.Close()
	slog.Debug("STUN 客户端创建成功", "proxy", proxyName)

	// 8. 构建并发送 STUN Binding Request
	message := stun.MustBuild(stun.TransactionID, stun.BindingRequest)

	// 9. 执行 STUN 请求并处理响应 (设置超时)
	callbackChan := make(chan UDPCheckResult, 1)
	go func() {
		internalResult := UDPCheckResult{Supported: false}
		// 使用包装器的 SetDeadline
		deadline := time.Now().Add(time.Duration(config.GlobalConfig.Timeout) * time.Millisecond)
		if err := stunConn.SetDeadline(deadline); err != nil {
			slog.Warn("设置 stunConn 超时失败", "proxy", proxyName, "error", err)
		}

		err := stunClient.Do(message, func(res stun.Event) {
			_ = stunConn.SetDeadline(time.Time{})
			if res.Error != nil {
				slog.Info("STUN 请求失败或超时", "proxy", proxyName, "stun", config.GlobalConfig.StunServer, "error", res.Error)
				internalResult.Error = res.Error
				callbackChan <- internalResult
				return
			}
			var xorAddr stun.XORMappedAddress
			if err := xorAddr.GetFrom(res.Message); err != nil {
				slog.Info("从 STUN 响应获取 IP 失败", "proxy", proxyName, "stun", config.GlobalConfig.StunServer, "error", err)
				internalResult.Error = fmt.Errorf("解析 STUN 响应失败: %w", err)
				callbackChan <- internalResult
				return
			}
			internalResult.Supported = true
			internalResult.ExternalIP = xorAddr.IP.String()
			slog.Info("UDP 检查成功", "proxy", proxyName, "stun", config.GlobalConfig.StunServer, "external_ip", internalResult.ExternalIP)
			callbackChan <- internalResult
		})

		if err != nil {
			_ = stunConn.SetDeadline(time.Time{})
			slog.Info("STUN Do 方法出错", "proxy", proxyName, "stun", config.GlobalConfig.StunServer, "error", err)
			internalResult.Error = err
			// 确保即使 Do 出错也尝试发送结果，避免 goroutine 泄漏
			// 使用 non-blocking send，如果 channel 已满则丢弃 (理论上不应发生)
			select {
			case callbackChan <- internalResult:
			default:
				slog.Warn("Callback channel full when sending Do error result", "proxy", proxyName)
			}
		}
	}()

	// 等待回调或超时
	select {
	case res := <-callbackChan:
		return res
	case <-time.After(time.Duration(config.GlobalConfig.Timeout)*time.Millisecond + 500*time.Millisecond):
		slog.Info("STUN 请求最终超时", "proxy", proxyName, "stun", config.GlobalConfig.StunServer)
		result.Error = fmt.Errorf("STUN 请求最终超时")
		return result
	}
}
