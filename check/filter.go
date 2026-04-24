package check

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/beck-8/subs-check/config"
)

// CompileFilterPatterns compiles the configured filter regex list.
// Invalid patterns are dropped with a warning; returns an empty slice
// when filtering is disabled or all patterns failed to compile.
func CompileFilterPatterns() []*regexp.Regexp {
	if len(config.GlobalConfig.Filter) == 0 {
		return nil
	}
	var patterns []*regexp.Regexp
	for _, pattern := range config.GlobalConfig.Filter {
		re, err := regexp.Compile(pattern)
		if err != nil {
			slog.Warn(fmt.Sprintf("过滤正则表达式编译失败，已跳过: %s, 错误: %v", pattern, err))
			continue
		}
		patterns = append(patterns, re)
	}
	if len(patterns) == 0 && len(config.GlobalConfig.Filter) > 0 {
		slog.Warn("所有过滤正则表达式编译失败，跳过过滤")
	}
	return patterns
}

// MatchesFilter reports whether r's rendered name (without speed tag)
// matches any pattern. An empty pattern slice counts as "passes".
func MatchesFilter(r Result, patterns []*regexp.Regexp) bool {
	if len(patterns) == 0 {
		return true
	}
	if r.Proxy == nil {
		return false
	}
	name := RenderName(r, false)
	for _, re := range patterns {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

// FilterResults 根据配置的正则表达式过滤节点。
//
// 只有渲染后的展示名(不含速度标签)匹配任一正则的节点才会被保留。
// 这里用 RenderName(r, false) 而不是 r.Proxy["name"] 是为了让 filter 能看到
// 国家+媒体标签的完整视图,同时保持 proxy["name"] 不被修改。
func FilterResults(results []Result) []Result {
	patterns := CompileFilterPatterns()
	if len(patterns) == 0 {
		return results
	}

	slog.Info(fmt.Sprintf("应用节点过滤规则，共 %d 个正则表达式", len(patterns)))

	var filtered []Result
	for _, r := range results {
		if MatchesFilter(r, patterns) {
			filtered = append(filtered, r)
		}
	}

	slog.Info(fmt.Sprintf("过滤后节点数量: %d (过滤前: %d)", len(filtered), len(results)))
	return filtered
}
