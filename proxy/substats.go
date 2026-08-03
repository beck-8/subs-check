package proxies

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/beck-8/subs-check/save/method"
)

const subStatsFileName = "sub_stats.json"

// SubUrlStat 记录单个订阅链接的历史请求统计
type SubUrlStat struct {
	URL             string    `json:"url"`
	Source          string    `json:"source"`
	TotalRequests   int64     `json:"total_requests"`
	FailedRequests  int64     `json:"failed_requests"`
	LastFailTime    time.Time `json:"last_fail_time,omitzero"`
	LastFailReason  string    `json:"last_fail_reason,omitempty"`
	LastSuccessTime time.Time `json:"last_success_time,omitzero"`
}

var (
	subStatsMu    sync.Mutex
	subStats      = make(map[string]*SubUrlStat)
	subStatsOnce  sync.Once
	subStatsDirty bool
)

// RecordSubUrlResult 记录一次订阅链接的请求结果，用于统计长期不可用的订阅链接
func RecordSubUrlResult(url, source string, err error) {
	loadSubUrlStatsOnce()

	subStatsMu.Lock()
	defer subStatsMu.Unlock()

	stat, ok := subStats[url]
	if !ok {
		stat = &SubUrlStat{URL: url}
		subStats[url] = stat
	}
	// source 可能会因为远程清单调整而变化，始终以最新的为准
	stat.Source = source
	stat.TotalRequests++
	if err != nil {
		stat.FailedRequests++
		stat.LastFailTime = time.Now()
		stat.LastFailReason = err.Error()
	} else {
		stat.LastSuccessTime = time.Now()
	}
	subStatsDirty = true
}

// GetSubUrlStats 返回所有订阅链接的统计信息，按失败次数从高到低排序
func GetSubUrlStats() []SubUrlStat {
	loadSubUrlStatsOnce()

	subStatsMu.Lock()
	defer subStatsMu.Unlock()

	out := make([]SubUrlStat, 0, len(subStats))
	for _, s := range subStats {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FailedRequests != out[j].FailedRequests {
			return out[i].FailedRequests > out[j].FailedRequests
		}
		return out[i].URL < out[j].URL
	})
	return out
}

// SaveSubUrlStats 将当前的订阅链接统计信息持久化到磁盘
func SaveSubUrlStats() {
	subStatsMu.Lock()
	if !subStatsDirty {
		subStatsMu.Unlock()
		return
	}
	list := make([]*SubUrlStat, 0, len(subStats))
	for _, s := range subStats {
		list = append(list, s)
	}
	subStatsDirty = false
	subStatsMu.Unlock()

	path, err := subStatsFilePath()
	if err != nil {
		slog.Warn("获取订阅统计文件路径失败", "err", err)
		return
	}

	data, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		slog.Warn("序列化订阅统计信息失败", "err", err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		slog.Warn("创建订阅统计目录失败", "err", err)
		return
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		slog.Warn("保存订阅统计信息失败", "err", err)
	}
}

func loadSubUrlStatsOnce() {
	subStatsOnce.Do(func() {
		path, err := subStatsFilePath()
		if err != nil {
			return
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return
		}
		var list []*SubUrlStat
		if err := json.Unmarshal(data, &list); err != nil {
			slog.Warn("解析订阅统计信息失败", "err", err)
			return
		}
		subStatsMu.Lock()
		defer subStatsMu.Unlock()
		for _, s := range list {
			subStats[s.URL] = s
		}
	})
}

func subStatsFilePath() (string, error) {
	saver, err := method.NewLocalSaver()
	if err != nil {
		return "", fmt.Errorf("获取输出目录失败: %w", err)
	}
	return filepath.Join(saver.OutputPath, subStatsFileName), nil
}
