package main

import (
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/lmittmann/tint"
	mihomoLog "github.com/metacubex/mihomo/log"
)

var CurrentCommit = "unknown"

func init() {
	// 设置依赖库日志级别
	// 如果要深入排查协议问题，后边可能要动态调整这个参数
	mihomoLog.SetLevel(mihomoLog.SILENT)

	// 获取日志级别
	logLevel := getLogLevel()

	// 创建带有颜色金额日志级别的 Handler
	handler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      logLevel,
		TimeFormat: "2006-01-02 15:04:05",
	})
	logger := slog.New(handler)

	// 设置为全局日志记录器
	slog.SetDefault(logger)

	fmt.Println("==================== WARNING ====================")
	fmt.Println("⚠️  重要提示：")
	fmt.Println("1. 本项目完全开源免费，请勿相信任何收费版本")
	fmt.Println("2. 本项目仅供学习交流，请勿用于非法用途")
	fmt.Println("3. 项目地址：https://github.com/beck-8/subs-check")
	fmt.Println("4. 镜像地址：ghcr.io/beck-8/subs-check:latest")
	fmt.Println("==================================================")

	if strings.ToLower(os.Getenv("SUB_CHECK_PPROF")) != "" {
		// 在调试模式下启动 pprof 服务器
		go func() {
			slog.Info("Starting pprof server on localhost:61000")
			if err := http.ListenAndServe("localhost:61000", nil); err != nil {
				slog.Error("Failed to start pprof server", "error", err)
			}
		}()
	}
	// 添加内存使用情况监控
	if strings.ToLower(os.Getenv("SUB_CHECK_MEM_MONITOR")) != "" {
		go func() {
			var m runtime.MemStats
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			for range ticker.C {
				runtime.ReadMemStats(&m)
				slog.Info("内存使用情况",
					"Alloc", formatBytes(m.Alloc),
					"TotalAlloc", formatBytes(m.TotalAlloc),
					"Sys", formatBytes(m.Sys),
					"HeapAlloc", formatBytes(m.HeapAlloc),
					"HeapSys", formatBytes(m.HeapSys),
					"HeapInuse", formatBytes(m.HeapInuse),
					"HeapIdle", formatBytes(m.HeapIdle),
					"HeapReleased", formatBytes(m.HeapReleased),
					"HeapObjects", m.HeapObjects,
					"StackInuse", formatBytes(m.StackInuse),
					"StackSys", formatBytes(m.StackSys),
					"MSpanInuse", formatBytes(m.MSpanInuse),
					"MSpanSys", formatBytes(m.MSpanSys),
					"MCacheInuse", formatBytes(m.MCacheInuse),
					"MCacheSys", formatBytes(m.MCacheSys),
					"BuckHashSys", formatBytes(m.BuckHashSys),
					"GCSys", formatBytes(m.GCSys),
					"OtherSys", formatBytes(m.OtherSys),
					"NextGC", formatBytes(m.NextGC),
					"LastGC", time.Unix(0, int64(m.LastGC)).Format("15:04:05"),
					"PauseTotalNs", m.PauseTotalNs,
					"NumGC", m.NumGC,
					"NumForcedGC", m.NumForcedGC,
					"GCCPUFraction", m.GCCPUFraction,
				)
			}
		}()
	}
	// termux环境
	os.Setenv("SSL_CERT_FILE", "/data/data/com.termux/files/usr/etc/tls/cert.pem")
}

func getLogLevel() slog.Level {
	levelStr := strings.ToLower(os.Getenv("LOG_LEVEL")) // 读取环境变量
	switch levelStr {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo // 默认 INFO 级别
	}
}

// formatBytes 将字节数格式化为人类可读的形式
func formatBytes(bytes uint64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := uint64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.2f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
