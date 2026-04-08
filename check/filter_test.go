package check

import (
	"testing"

	"github.com/beck-8/subs-check/check/platform"
	"github.com/beck-8/subs-check/config"
)

func TestFilterResults_NoFilter_PassesAll(t *testing.T) {
	withConfig(t, config.Config{
		Filter:    nil,
		Platforms: []string{},
	}, func() {
		results := []Result{
			{Proxy: map[string]any{"name": "a"}},
			{Proxy: map[string]any{"name": "b"}},
		}
		got := FilterResults(results)
		if len(got) != 2 {
			t.Errorf("expected 2 results passthrough, got %d", len(got))
		}
	})
}

func TestFilterResults_MatchByOriginalName(t *testing.T) {
	// 关闭 rename,filter 按原名里的关键字匹配
	withConfig(t, config.Config{
		RenameNode: false,
		Filter:     []string{"香港|HK"},
		Platforms:  []string{},
	}, func() {
		results := []Result{
			{Proxy: map[string]any{"name": "🇭🇰香港01"}},
			{Proxy: map[string]any{"name": "🇺🇸美国01"}},
			{Proxy: map[string]any{"name": "HK-singapore-mix"}},
		}
		got := FilterResults(results)
		if len(got) != 2 {
			t.Fatalf("expected 2 matches (HK and 香港), got %d", len(got))
		}
	})
}

func TestFilterResults_MatchByMediaTag(t *testing.T) {
	// 不靠原名,靠 Phase 2 产出的 Netflix 标签匹配
	withConfig(t, config.Config{
		RenameNode: false,
		Filter:     []string{`NF-US`},
		Platforms:  []string{"netflix"},
	}, func() {
		results := []Result{
			{
				Proxy:   map[string]any{"name": "jp-node"},
				Netflix: &platform.NetflixResult{Full: true, Region: "US"},
			},
			{
				Proxy:   map[string]any{"name": "hk-node"},
				Netflix: &platform.NetflixResult{Full: true, Region: "HK"},
			},
			{
				Proxy: map[string]any{"name": "no-nf-node"},
			},
		}
		got := FilterResults(results)
		if len(got) != 1 {
			t.Fatalf("expected 1 match (NF-US), got %d", len(got))
		}
		if got[0].Proxy["name"].(string) != "jp-node" {
			t.Errorf("expected jp-node to be kept, got %q", got[0].Proxy["name"])
		}
	})
}

func TestFilterResults_DoesNotMutateName(t *testing.T) {
	// filter 调 RenderName 只是临时算字符串,不能修改 proxy["name"]
	withConfig(t, config.Config{
		RenameNode: false,
		Filter:     []string{"NF"},
		Platforms:  []string{"netflix"},
	}, func() {
		results := []Result{
			{
				Proxy:   map[string]any{"name": "pristine-name"},
				Netflix: &platform.NetflixResult{Full: true, Region: "US"},
			},
		}
		_ = FilterResults(results)
		if results[0].Proxy["name"] != "pristine-name" {
			t.Errorf("FilterResults should not mutate proxy[\"name\"], got %q", results[0].Proxy["name"])
		}
	})
}
