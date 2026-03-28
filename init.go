package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	_ "net/http/pprof"
	"os"
	"strings"

	"github.com/beck-8/subs-check/app"
	"github.com/lmittmann/tint"
	mihomoLog "github.com/metacubex/mihomo/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

var Version = "dev"
var CurrentCommit = "unknown"

var TempLog string

func init() {
	// 设置依赖库日志级别
	if os.Getenv("MIHOMO_DEBUG") != "" {
		mihomoLog.SetLevel(mihomoLog.DEBUG)
	} else {
		mihomoLog.SetLevel(mihomoLog.SILENT)
	}

	// 获取日志级别
	logLevel := getLogLevel()

	// 配置日志文件
	fileLogger := &lumberjack.Logger{
		Filename:   app.TempLog(),
		MaxSize:    10,
		MaxBackups: 3,
		MaxAge:     7,
	}

	// 创建两个单独的handler
	// 1. 终端输出 - 带颜色
	consoleHandler := tint.NewHandler(os.Stdout, &tint.Options{
		Level:      logLevel,
		TimeFormat: "2006-01-02 15:04:05",
	})

	// 2. 文件输出 - 不带颜色
	fileHandler := tint.NewHandler(fileLogger, &tint.Options{
		Level:      logLevel,
		TimeFormat: "2006-01-02 15:04:05",
		NoColor:    true, // 禁用颜色
	})

	// 创建一个自定义的Slog处理器，将日志同时发送到两个处理器
	handler := &multiHandler{
		console: consoleHandler,
		file:    fileHandler,
	}

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

	// pprof 在空闲时不消耗性能，默认启动
	go func() {
		slog.Info("Starting pprof server on :8199")
		if err := http.ListenAndServe(":8199", nil); err != nil {
			slog.Error("Failed to start pprof server", "error", err)
		}
	}()
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

// 多输出处理器 - 简化版本
type multiHandler struct {
	console slog.Handler
	file    slog.Handler
}

func (h *multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.console.Enabled(ctx, level) || h.file.Enabled(ctx, level)
}

func (h *multiHandler) Handle(ctx context.Context, r slog.Record) error {
	// 复制记录，避免竞态条件
	r2 := r.Clone()

	// 终端输出 - 带颜色
	if err := h.console.Handle(ctx, r); err != nil {
		return err
	}

	// 文件输出 - 不带颜色
	return h.file.Handle(ctx, r2)
}

func (h *multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &multiHandler{
		console: h.console.WithAttrs(attrs),
		file:    h.file.WithAttrs(attrs),
	}
}

func (h *multiHandler) WithGroup(name string) slog.Handler {
	return &multiHandler{
		console: h.console.WithGroup(name),
		file:    h.file.WithGroup(name),
	}
}
