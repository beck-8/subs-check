package app

import (
	"bytes"
	"crypto/subtle"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/beck-8/subs-check/check"
	"github.com/beck-8/subs-check/config"
	"github.com/beck-8/subs-check/save/method"
	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
)

// initHttpServer 初始化HTTP服务器
func (app *App) initHttpServer() error {
	gin.SetMode(gin.ReleaseMode)
	// Route gin's access log and panic stacks into their own temp file
	// (same directory as the app log but a separate file). Keeps stdout
	// clean for the CLI progress renderer and keeps the web UI's log
	// viewer free of HTTP request noise. gin.Default() snapshots
	// DefaultWriter / DefaultErrorWriter at call time, so assign first.
	gin.DefaultWriter = GinFileLogger
	gin.DefaultErrorWriter = GinFileLogger
	router := gin.Default()

	saver, err := method.NewLocalSaver()
	if err != nil {
		return fmt.Errorf("获取http监听目录失败: %w", err)
	}

	// 静态文件路由 - 订阅服务相关，始终启用
	// 最初不应该不带路径，现在保持兼容
	router.StaticFile("/all.yaml", saver.OutputPath+"/all.yaml")
	router.StaticFile("/all.txt", saver.OutputPath+"/all.txt")
	router.StaticFile("/base64.txt", saver.OutputPath+"/base64.txt")
	router.StaticFile("/mihomo.yaml", saver.OutputPath+"/mihomo.yaml")
	router.StaticFile("/ACL4SSR_Online_Full.yaml", saver.OutputPath+"/ACL4SSR_Online_Full.yaml")
	// CM佬用的布丁狗
	router.StaticFile("/bdg.yaml", saver.OutputPath+"/bdg.yaml")

	router.Static("/sub/", saver.OutputPath)

	// pprof 路由，空闲时不消耗性能
	pprof.Register(router)

	// 根据配置决定是否启用Web控制面板
	if config.GlobalConfig.EnableWebUI {
		if config.GlobalConfig.APIKey == "" {
			if apiKey := os.Getenv("API_KEY"); apiKey != "" {
				config.GlobalConfig.APIKey = apiKey
			} else {
				config.GlobalConfig.APIKey = GenerateSimpleKey()
				slog.Warn("未设置api-key，已生成一个随机api-key", "api-key", config.GlobalConfig.APIKey)
			}
		}
		slog.Info("启用Web控制面板", "path", "http://ip:port/admin", "api-key", config.GlobalConfig.APIKey)

		// 设置模板加载 - 只有在启用Web控制面板时才加载
		router.SetHTMLTemplate(template.Must(template.New("").ParseFS(configFS, "templates/*.html")))

		// API路由
		api := router.Group("/api")
		api.Use(app.authMiddleware(config.GlobalConfig.APIKey)) // 添加认证中间件
		{
			// 配置相关API
			api.GET("/config", app.getConfig)
			api.POST("/config", app.updateConfig)

			// 状态相关API
			api.GET("/status", app.getStatus)
			api.POST("/trigger-check", app.triggerCheckHandler)
			api.POST("/force-close", app.forceCloseHandler)
			// 版本相关API
			api.GET("/version", app.getVersion)

			// 日志相关API
			api.GET("/logs", app.getLogs)
		}

		// 配置页面
		router.GET("/admin", func(c *gin.Context) {
			c.HTML(http.StatusOK, "admin.html", gin.H{
				"configPath": app.configPath,
			})
		})
	} else {
		slog.Info("Web控制面板已禁用")
	}

	// 启动HTTP服务器
	go func() {
		for {
			if err := router.Run(config.GlobalConfig.ListenPort); err != nil {
				slog.Error(fmt.Sprintf("HTTP服务器启动失败，正在重启中: %v", err))
			}
			time.Sleep(30 * time.Second)
		}
	}()
	slog.Info("HTTP服务器启动", "port", config.GlobalConfig.ListenPort)
	return nil
}

// authMiddleware API认证中间件
func (app *App) authMiddleware(key string) gin.HandlerFunc {
	return func(c *gin.Context) {
		apiKey := c.GetHeader("X-API-Key")
		if subtle.ConstantTimeCompare([]byte(apiKey), []byte(key)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "无效的API密钥"})
			return
		}
		c.Next()
	}
}

// getConfig 获取配置文件内容
func (app *App) getConfig(c *gin.Context) {
	configData, err := os.ReadFile(app.configPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取配置文件失败: %v", err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"content": string(configData),
	})
}

// updateConfig 更新配置文件内容
func (app *App) updateConfig(c *gin.Context) {
	var req struct {
		Content string `json:"content"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的请求格式"})
		return
	}
	// 验证YAML格式
	var yamlData map[string]any
	if err := yaml.Unmarshal([]byte(req.Content), &yamlData); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("YAML格式错误: %v", err)})
		return
	}

	// 写入新配置
	if err := os.WriteFile(app.configPath, []byte(req.Content), 0644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("保存配置文件失败: %v", err)})
		return
	}

	// 配置文件监听器会自动重新加载配置
	c.JSON(http.StatusOK, gin.H{"message": "配置已更新"})
}

// getStatus 获取应用状态
func (app *App) getStatus(c *gin.Context) {
	phaseResults := make(map[string]*check.PhaseResult, 3)
	for i := 1; i <= 3; i++ {
		phaseResults[fmt.Sprintf("%d", i)] = check.GetPhaseResult(i)
	}
	// Pipeline stages run concurrently, so a single `phase` value is no
	// longer expressive enough. Emit a flat pipeline snapshot alongside
	// the legacy fields; the admin UI renders from `pipeline` when present
	// and falls back to `phase` / `progress` / `available` otherwise.
	pipeline := gin.H{
		"total":      check.ProxyCount.Load(),
		"aliveDone":  check.Progress.Load(),
		"alivePass":  check.Available.Load(),
		"mediaDone":  check.MediaDone.Load(),
		"filterPass": check.FilterPassed.Load(),
		"speedDone":  check.SpeedDone.Load(),
		"speedPass":  check.SpeedOk.Load(),
	}
	c.JSON(http.StatusOK, gin.H{
		"checking":      app.checking.Load(),
		"proxyCount":    check.ProxyCount.Load(),
		"available":     check.Available.Load(),
		"progress":      check.Progress.Load(),
		"phase":         check.Phase.Load(),
		"phaseResults":  phaseResults,
		"pipeline":      pipeline,
		"hasSpeedTest":  config.GlobalConfig.SpeedTestUrl != "",
	})
}

// triggerCheckHandler 手动触发检测
func (app *App) triggerCheckHandler(c *gin.Context) {
	app.TriggerCheck()
	c.JSON(http.StatusOK, gin.H{"message": "已触发检测"})
}

// forceCloseHandler 强制关闭
func (app *App) forceCloseHandler(c *gin.Context) {
	check.RequestCancel()
	c.JSON(http.StatusOK, gin.H{"message": "已强制关闭"})
}

// getLogs 获取最近日志
func (app *App) getLogs(c *gin.Context) {
	// 简单实现，从日志文件读取最后xx行
	logPath := TempLog()

	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		c.JSON(http.StatusOK, gin.H{"logs": []string{}})
		return
	}
	lines, err := ReadLastNLines(logPath, 100)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("读取日志失败: %v", err)})
		return
	}
	c.JSON(http.StatusOK, gin.H{"logs": lines})
}

// getLogs 获取最近日志
func (app *App) getVersion(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"version": app.version})
}

// ReadLastNLines returns up to n trailing lines of filePath in file order.
// Reads the file backwards in chunks so the scan cost is O(n) instead of
// O(file size) — important because lumberjack lets the log reach 10MB and
// the admin UI polls /api/logs every 10 seconds.
func ReadLastNLines(filePath string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return nil, err
	}
	if size == 0 {
		return nil, nil
	}

	// Walk backwards, stopping once we have strictly more than n newlines
	// in hand: that guarantees the buffer contains all of the last n
	// complete lines plus at least one partial/boundary line before them,
	// which the final slice discards.
	const chunkSize int64 = 8192
	var buf []byte
	off := size
	for off > 0 {
		readSize := chunkSize
		if off < readSize {
			readSize = off
		}
		off -= readSize

		tmp := make([]byte, readSize)
		if _, err := f.ReadAt(tmp, off); err != nil && err != io.EOF {
			return nil, err
		}
		buf = append(tmp, buf...)

		if int64(bytes.Count(buf, []byte{'\n'})) > int64(n) {
			break
		}
	}

	// Drop trailing newline(s) so Split doesn't produce a spurious empty
	// last element (logs always terminate lines with \n).
	buf = bytes.TrimRight(buf, "\n")
	if len(buf) == 0 {
		return nil, nil
	}

	lines := strings.Split(string(buf), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	// Strip CR for CRLF-terminated logs (bufio.Scanner did this implicitly).
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, "\r")
	}
	return lines, nil
}

func GenerateSimpleKey() string {
	return fmt.Sprintf("%06d", time.Now().UnixNano()%1000000)
}
