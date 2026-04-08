package check

import (
	"testing"

	"github.com/beck-8/subs-check/check/platform"
	"github.com/beck-8/subs-check/config"
	proxyutils "github.com/beck-8/subs-check/proxy"
)

// withConfig 临时替换 config.GlobalConfig 的内容,测试结束后还原。
// GlobalConfig 是 *Config 指针,这里通过指针解引用赋值,保证持有同一指针的代码照常工作。
func withConfig(t *testing.T, cfg config.Config, fn func()) {
	t.Helper()
	old := *config.GlobalConfig
	*config.GlobalConfig = cfg
	defer func() { *config.GlobalConfig = old }()
	fn()
}

func TestRenderName_RenameOff_NoTags(t *testing.T) {
	withConfig(t, config.Config{
		RenameNode: false,
		Platforms:  []string{"openai", "netflix"},
	}, func() {
		r := Result{
			Proxy: map[string]any{"name": "🇭🇰香港01"},
		}
		got := RenderName(r, false)
		want := "🇭🇰香港01"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_RenameOff_PreservesOriginalWithPipes(t *testing.T) {
	// 机场原名里就带 | 的情况,不能破坏它
	withConfig(t, config.Config{
		RenameNode: false,
		Platforms:  []string{},
	}, func() {
		r := Result{
			Proxy: map[string]any{"name": "🇺🇸美国01-0.1倍 | 电信联通移动推荐"},
		}
		got := RenderName(r, false)
		want := "🇺🇸美国01-0.1倍 | 电信联通移动推荐"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_RenameOff_WithMediaTags(t *testing.T) {
	withConfig(t, config.Config{
		RenameNode: false,
		Platforms:  []string{"openai", "netflix", "disney"},
	}, func() {
		r := Result{
			Proxy:   map[string]any{"name": "🇭🇰香港01"},
			Openai:  &platform.OpenAIResult{Full: true, Region: "HK"},
			Netflix: &platform.NetflixResult{Full: true, Region: "HK"},
			Disney:  true,
		}
		got := RenderName(r, false)
		want := "🇭🇰香港01|GPT⁺-HK|NF-HK|D+"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_PlatformsOrderMatters(t *testing.T) {
	// 标签顺序严格遵循 config.Platforms
	withConfig(t, config.Config{
		RenameNode: false,
		Platforms:  []string{"netflix", "openai"}, // 与上一个测试顺序相反
	}, func() {
		r := Result{
			Proxy:   map[string]any{"name": "n"},
			Openai:  &platform.OpenAIResult{Full: true, Region: "HK"},
			Netflix: &platform.NetflixResult{Full: true, Region: "HK"},
		}
		got := RenderName(r, false)
		want := "n|NF-HK|GPT⁺-HK"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_IncludeSpeedTrue(t *testing.T) {
	withConfig(t, config.Config{
		RenameNode:   false,
		SpeedTestUrl: "https://example.com/file",
		Platforms:    []string{},
	}, func() {
		r := Result{
			Proxy: map[string]any{"name": "n"},
			Speed: 5120, // 5.0 MB/s
		}
		got := RenderName(r, true)
		want := "n|5.0MB/s"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_IncludeSpeedFalse_NoSpeedTag(t *testing.T) {
	withConfig(t, config.Config{
		RenameNode:   false,
		SpeedTestUrl: "https://example.com/file",
		Platforms:    []string{},
	}, func() {
		r := Result{
			Proxy: map[string]any{"name": "n"},
			Speed: 5120,
		}
		got := RenderName(r, false) // filter 阶段调用时传 false
		want := "n"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_SpeedTagFormat_KB(t *testing.T) {
	withConfig(t, config.Config{
		RenameNode:   false,
		SpeedTestUrl: "https://example.com/file",
		Platforms:    []string{},
	}, func() {
		r := Result{
			Proxy: map[string]any{"name": "n"},
			Speed: 512, // < 1024,展示 KB/s
		}
		got := RenderName(r, true)
		want := "n|512KB/s"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_SpeedZero_NoSpeedTag(t *testing.T) {
	// Speed=0 表示未测速(ForceClose 场景),即使 includeSpeed=true 也不加标签
	withConfig(t, config.Config{
		RenameNode:   false,
		SpeedTestUrl: "https://example.com/file",
		Platforms:    []string{},
	}, func() {
		r := Result{
			Proxy: map[string]any{"name": "n"},
			Speed: 0,
		}
		got := RenderName(r, true)
		want := "n"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_SubTagAppendedLast(t *testing.T) {
	withConfig(t, config.Config{
		RenameNode: false,
		Platforms:  []string{"disney"},
	}, func() {
		r := Result{
			Proxy:  map[string]any{"name": "n", "sub_tag": "my-sub"},
			Disney: true,
		}
		got := RenderName(r, false)
		want := "n|D+|my-sub"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_IPRiskTag(t *testing.T) {
	withConfig(t, config.Config{
		RenameNode: false,
		Platforms:  []string{"iprisk"},
	}, func() {
		r := Result{
			Proxy:  map[string]any{"name": "n"},
			IPRisk: "5%",
		}
		got := RenderName(r, false)
		want := "n|5%"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

func TestRenderName_RenameOnWithCountry(t *testing.T) {
	proxyutils.ResetRenameCounter()
	withConfig(t, config.Config{
		RenameNode: true,
		NodePrefix: "PREFIX-",
		Platforms:  []string{},
	}, func() {
		r := Result{
			Proxy:   map[string]any{"name": "original"},
			Country: "HK",
		}
		got := RenderName(r, false)
		if got == "original" {
			t.Errorf("RenderName() should not use original name when RenameNode=true, got %q", got)
		}
		if len(got) < len("PREFIX-") || got[:len("PREFIX-")] != "PREFIX-" {
			t.Errorf("RenderName() should start with prefix, got %q", got)
		}
		if !stringContains(got, "HK") {
			t.Errorf("RenderName() should contain country code HK, got %q", got)
		}
	})
}

func TestRenderName_RenameOnButEmptyCountry_FallsBackToOriginal(t *testing.T) {
	// 重命名开启但 Country 为空(Phase 2 查询失败),应回退到原名
	withConfig(t, config.Config{
		RenameNode: true,
		NodePrefix: "PREFIX-",
		Platforms:  []string{},
	}, func() {
		r := Result{
			Proxy:   map[string]any{"name": "original"},
			Country: "",
		}
		got := RenderName(r, false)
		want := "original"
		if got != want {
			t.Errorf("RenderName() = %q, want %q", got, want)
		}
	})
}

// 辅助函数
func stringContains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
