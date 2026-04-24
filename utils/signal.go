package utils

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

// SetupSignalHandler wires process signals to a cancel callback.
// SIGHUP (HUB) aborts the currently running check phase via onCancel;
// the program itself keeps running. SIGINT/SIGTERM are not handled
// here to avoid propagating to child processes.
func SetupSignalHandler(onCancel func()) {
	slog.Debug("设置信号处理器")

	hubSigChan := make(chan os.Signal, 1)
	signal.Notify(hubSigChan, syscall.SIGHUP)

	go func() {
		for sig := range hubSigChan {
			slog.Debug(fmt.Sprintf("收到 HUB 信号: %s", sig))
			onCancel()
			slog.Debug("HUB 模式: 已请求取消当前任务，程序继续运行")
		}
	}()
}
