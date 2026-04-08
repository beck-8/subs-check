package check

import (
	"fmt"
	"log/slog"
	"regexp"

	"github.com/beck-8/subs-check/config"
)

// FilterResults 根据配置的正则表达式过滤节点。
//
// 只有渲染后的展示名(不含速度标签)匹配任一正则的节点才会被保留。
// 这里用 RenderName(r, false) 而不是 r.Proxy["name"] 是为了让 filter 能看到
// 国家+媒体标签的完整视图,同时保持 proxy["name"] 不被修改。
func FilterResults(results []Result) []Result {
	if len(config.GlobalConfig.Filter) == 0 {
		return results
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

	if len(patterns) == 0 {
		slog.Warn("所有过滤正则表达式编译失败，跳过过滤")
		return results
	}

	slog.Info(fmt.Sprintf("应用节点过滤规则，共 %d 个正则表达式", len(patterns)))

	var filtered []Result
	for _, r := range results {
		if r.Proxy == nil {
			continue
		}
		name := RenderName(r, false)
		for _, re := range patterns {
			if re.MatchString(name) {
				filtered = append(filtered, r)
				break
			}
		}
	}

	slog.Info(fmt.Sprintf("过滤后节点数量: %d (过滤前: %d)", len(filtered), len(results)))
	return filtered
}
