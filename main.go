package main

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"github.com/beck-8/subs-check/app"
)

func main() {
	application := app.New(fmt.Sprintf("%s-%s", Version, CurrentCommit))
	slog.Info(fmt.Sprintf("当前版本: %s-%s", Version, CurrentCommit))
	if strings.Contains(Version, "dev") {
		port := "8299"
		var pids []string
		if os.Getenv("OS") == "Windows_NT" {
			out, _ := exec.Command("cmd", "/C", fmt.Sprintf("netstat -ano | findstr :%s", port)).Output()
			for line := range strings.SplitSeq(string(out), "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 5 {
					pids = append(pids, fields[4])
				}
			}
			for _, pid := range pids {
				_ = exec.Command("taskkill", "/PID", pid, "/F").Run()
			}
		} else {
			out, _ := exec.Command("sh", "-c", fmt.Sprintf("lsof -ti:%s", port)).Output()
			for _, pid := range strings.Fields(string(out)) {
				_ = exec.Command("kill", "-9", pid).Run()
			}
		}
	}

	if err := application.Initialize(); err != nil {
		slog.Error(fmt.Sprintf("初始化失败: %v", err))
		os.Exit(1)
	}

	application.Run()
}
