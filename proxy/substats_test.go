package proxies

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/beck-8/subs-check/config"
)

// resetSubStatsForTest 清空内存态统计并让下一次访问重新触发磁盘加载，
// 同时把输出目录指向一个隔离的临时目录，避免测试间相互影响。
func resetSubStatsForTest(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	origOutputDir := config.GlobalConfig.OutputDir
	config.GlobalConfig.OutputDir = dir
	t.Cleanup(func() {
		config.GlobalConfig.OutputDir = origOutputDir
	})

	subStatsMu.Lock()
	subStats = make(map[string]*SubUrlStat)
	subStatsDirty = false
	subStatsMu.Unlock()
	subStatsOnce = sync.Once{}
}

func TestRecordSubUrlResult_SuccessAndFailure(t *testing.T) {
	resetSubStatsForTest(t)

	RecordSubUrlResult("http://example.com/sub", "本地配置", nil)
	RecordSubUrlResult("http://example.com/sub", "本地配置", errors.New("timeout"))

	stats := GetSubUrlStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat entry, got %d", len(stats))
	}
	s := stats[0]
	if s.TotalRequests != 2 {
		t.Errorf("expected TotalRequests=2, got %d", s.TotalRequests)
	}
	if s.FailedRequests != 1 {
		t.Errorf("expected FailedRequests=1, got %d", s.FailedRequests)
	}
	if s.LastFailTime.IsZero() {
		t.Error("expected LastFailTime to be set")
	}
	if s.LastFailReason != "timeout" {
		t.Errorf("expected LastFailReason=%q, got %q", "timeout", s.LastFailReason)
	}
	if s.LastSuccessTime.IsZero() {
		t.Error("expected LastSuccessTime to be set")
	}
}

func TestGetSubUrlStats_SortedByFailures(t *testing.T) {
	resetSubStatsForTest(t)

	RecordSubUrlResult("http://a.example.com/sub", "src", nil)
	RecordSubUrlResult("http://b.example.com/sub", "src", errors.New("err"))
	RecordSubUrlResult("http://b.example.com/sub", "src", errors.New("err"))
	RecordSubUrlResult("http://c.example.com/sub", "src", errors.New("err"))

	stats := GetSubUrlStats()
	if len(stats) != 3 {
		t.Fatalf("expected 3 stat entries, got %d", len(stats))
	}
	if stats[0].URL != "http://b.example.com/sub" || stats[0].FailedRequests != 2 {
		t.Errorf("expected b.example.com with 2 failures first, got %+v", stats[0])
	}
}

func TestSaveAndLoadSubUrlStats_Persistence(t *testing.T) {
	resetSubStatsForTest(t)

	RecordSubUrlResult("http://persist.example.com/sub", "本地配置", errors.New("boom"))
	SaveSubUrlStats()

	path, err := subStatsFilePath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected stats file to exist at %s: %v", path, err)
	}

	// 模拟重启：清空内存态，但保留磁盘文件，重新加载后应恢复数据
	subStatsMu.Lock()
	subStats = make(map[string]*SubUrlStat)
	subStatsMu.Unlock()
	subStatsOnce = sync.Once{}

	stats := GetSubUrlStats()
	if len(stats) != 1 {
		t.Fatalf("expected 1 stat entry after reload, got %d", len(stats))
	}
	if stats[0].URL != "http://persist.example.com/sub" {
		t.Errorf("unexpected URL after reload: %s", stats[0].URL)
	}
	if stats[0].FailedRequests != 1 {
		t.Errorf("expected FailedRequests=1 after reload, got %d", stats[0].FailedRequests)
	}

	_ = filepath.Base(path)
}
