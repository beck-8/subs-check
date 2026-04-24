package check

import (
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/beck-8/subs-check/config"
	"github.com/mattn/go-isatty"
)

// ttyReady reports whether stdout is a terminal that can render in-place
// ANSI updates. Evaluated once at package init; on Windows this also
// flips the console into virtual-terminal mode.
var ttyReady = func() bool {
	fd := os.Stdout.Fd()
	if !isatty.IsTerminal(fd) {
		return false
	}
	return enableVTMode(fd)
}()

// showProgress drives the progress UI until done is signalled.
// Chooses between three-line in-place ANSI rendering (tty) and
// periodic info logs (non-tty — pipes, docker logs, redirected files).
func (pc *ProxyChecker) showProgress(done chan bool) {
	if ttyReady {
		pc.showProgressANSI(done)
		return
	}
	pc.showProgressLog(done)
}

// showProgressANSI renders three stacked progress lines (alive / media / speed),
// updating in place. The first frame prints three fresh lines; subsequent
// frames move the cursor back up three rows and rewrite them.
func (pc *ProxyChecker) showProgressANSI(done chan bool) {
	const frameInterval = 200 * time.Millisecond
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	firstFrame := true
	for {
		select {
		case <-done:
			if progressRendered.Load() {
				// render loop owns the last newline slot; emit one so any
				// subsequent log line starts on a clean row
				fmt.Println()
				progressRendered.Store(false)
			}
			return
		case <-ticker.C:
			if progressPaused.Load() {
				continue
			}
			if ProxyCount.Load() == 0 {
				continue
			}
			if !firstFrame {
				// cursor up 3 lines, column 0
				fmt.Print("\x1b[3A\r")
			}
			pc.renderFrame()
			firstFrame = false
			progressRendered.Store(true)
		}
	}
}

// showProgressLog emits a compact single-line info every 5 seconds so
// non-tty consumers (pipes, docker logs) get periodic visibility
// without scroll spam.
func (pc *ProxyChecker) showProgressLog(done chan bool) {
	const interval = 5 * time.Second
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			if progressPaused.Load() || ProxyCount.Load() == 0 {
				continue
			}
			slog.Info(pc.formatPipelineOneLine())
		}
	}
}

// renderFrame writes three progress lines. Each line is prefixed by
// \x1b[2K (erase line) so varying-width numbers don't leave stale chars.
func (pc *ProxyChecker) renderFrame() {
	hasSpeed := config.GlobalConfig.SpeedTestUrl != ""
	limit := config.GlobalConfig.SuccessLimit

	aliveTotal := ProxyCount.Load()
	aliveDone := Progress.Load()
	aliveOk := Available.Load()
	mediaDone := MediaDone.Load()
	filterPass := FilterPassed.Load()
	speedDone := SpeedDone.Load()
	speedOk := SpeedOk.Load()

	// line 1: alive
	fmt.Printf("\x1b[2K\r%s\n", formatStageLine("测活", aliveDone, aliveTotal, "存活", aliveOk, false))

	// line 2: media + inline filter outcome
	fmt.Printf("\x1b[2K\r%s\n", formatStageLine("媒体", mediaDone, aliveOk, "通过", filterPass, false))

	// line 3: speed (when enabled) or a summary placeholder
	if hasSpeed {
		limitHit := limit > 0 && int32(speedOk) >= limit
		fmt.Printf("\x1b[2K\r%s\n", formatStageLine("测速", speedDone, filterPass, "通过", speedOk, limitHit))
	} else {
		limitHit := limit > 0 && int32(filterPass) >= limit
		fmt.Printf("\x1b[2K\r%s\n", formatStageLine("输出", filterPass, aliveOk, "通过", filterPass, limitHit))
	}
}

// formatStageLine renders one progress row:
//
//	[name] [bar] pct%  done/total  label: pass [✓]
//
// Widths are fixed so numbers wiggling between 1-4 digits don't shift columns.
func formatStageLine(name string, done, total uint32, passLabel string, pass uint32, limitHit bool) string {
	const barWidth = 30
	var percent float64
	if total > 0 {
		percent = float64(done) / float64(total) * 100
	}
	filled := int(percent / 100 * float64(barWidth))
	if filled > barWidth {
		filled = barWidth
	}
	bar := strings.Repeat("=", filled)
	if filled < barWidth {
		bar += ">"
	}
	bar += strings.Repeat(" ", barWidth-len(bar))

	mark := ""
	if limitHit {
		mark = " ✓"
	}
	return fmt.Sprintf("[%s] [%s] %5.1f%% %6d/%-6d %s: %d%s",
		name, bar, percent, done, total, passLabel, pass, mark)
}

// formatPipelineOneLine collapses pipeline state into a single info line,
// used by the non-tty log renderer.
func (pc *ProxyChecker) formatPipelineOneLine() string {
	hasSpeed := config.GlobalConfig.SpeedTestUrl != ""
	aliveTotal := ProxyCount.Load()
	aliveDone := Progress.Load()
	aliveOk := Available.Load()
	mediaDone := MediaDone.Load()
	filterPass := FilterPassed.Load()
	speedOk := SpeedOk.Load()

	if hasSpeed {
		return fmt.Sprintf("流水线: 测活 %d/%d (存活:%d) | 媒体 %d/%d (通过:%d) | 测速 通过:%d",
			aliveDone, aliveTotal, aliveOk,
			mediaDone, aliveOk, filterPass,
			speedOk)
	}
	return fmt.Sprintf("流水线: 测活 %d/%d (存活:%d) | 媒体 %d/%d (通过:%d)",
		aliveDone, aliveTotal, aliveOk,
		mediaDone, aliveOk, filterPass)
}
