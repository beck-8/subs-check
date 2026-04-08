package check

import (
	"fmt"
	"strings"

	"github.com/beck-8/subs-check/config"
	proxyutils "github.com/beck-8/subs-check/proxy"
)

// RenderName 根据 Result 的结构化字段构造展示名。
//
// 这是整个项目唯一的"节点名生成"出口,纯函数:
//   - 无 I/O,无 goroutine
//   - 不读写 proxy map 的 name 字段,不修改 Result
//   - 仅依赖传入的 Result 和 config.GlobalConfig
//
// includeSpeed 为 true 时追加速度标签,只在最终输出 all.yaml 时用。
// filter 阶段应该传 false,因为此时尚未测速。
func RenderName(r Result, includeSpeed bool) string {
	// 1. base 名字
	var base string
	if config.GlobalConfig.RenameNode && r.Country != "" {
		base = config.GlobalConfig.NodePrefix + proxyutils.Rename(r.Country)
	} else if r.Proxy != nil {
		if n, ok := r.Proxy["name"].(string); ok {
			base = strings.TrimSpace(n)
		}
	}

	// 2. 按 config.Platforms 顺序收集媒体标签
	var tags []string
	for _, plat := range config.GlobalConfig.Platforms {
		if tag := mediaTagFor(plat, &r); tag != "" {
			tags = append(tags, tag)
		}
	}

	// 3. 速度标签(仅 includeSpeed 且有速度时追加)
	if includeSpeed && config.GlobalConfig.SpeedTestUrl != "" && r.Speed > 0 {
		tags = append(tags, formatSpeedTag(r.Speed))
	}

	// 4. sub_tag 追加到最后
	if r.Proxy != nil {
		if t, ok := r.Proxy["sub_tag"].(string); ok && t != "" {
			tags = append(tags, t)
		}
	}

	if len(tags) == 0 {
		return base
	}
	return base + "|" + strings.Join(tags, "|")
}

// mediaTagFor 返回单个平台的展示标签,未命中返回空字符串。
// 新增平台时只需在这里加一个 case 和对应的 Result 字段。
func mediaTagFor(plat string, r *Result) string {
	switch plat {
	case "openai":
		if r.Openai != nil {
			if r.Openai.Full {
				if r.Openai.Region != "" {
					return fmt.Sprintf("GPT⁺-%s", r.Openai.Region)
				}
				return "GPT⁺"
			}
			if r.Openai.Web {
				if r.Openai.Region != "" {
					return fmt.Sprintf("GPT-%s", r.Openai.Region)
				}
				return "GPT"
			}
		}
	case "netflix":
		if r.Netflix != nil {
			if r.Netflix.Full {
				if r.Netflix.Region != "" {
					return fmt.Sprintf("NF-%s", r.Netflix.Region)
				}
				return "NF"
			}
			if r.Netflix.OriginalsOnly {
				return "NF"
			}
		}
	case "disney":
		if r.Disney {
			return "D+"
		}
	case "gemini":
		if r.Gemini != "" {
			return fmt.Sprintf("GM-%s", r.Gemini)
		}
	case "claude":
		if r.Claude != "" {
			return fmt.Sprintf("CL-%s", r.Claude)
		}
	case "spotify":
		if r.Spotify != "" {
			return fmt.Sprintf("SP-%s", r.Spotify)
		}
	case "iprisk":
		if r.IPRisk != "" {
			return r.IPRisk
		}
	case "youtube":
		if r.Youtube != "" {
			return fmt.Sprintf("YT-%s", r.Youtube)
		}
	case "tiktok":
		if r.TikTok != "" {
			return fmt.Sprintf("TK-%s", r.TikTok)
		}
	}
	return ""
}

// formatSpeedTag 把测速结果(KB/s)格式化为展示字符串。
//
//	<1024 → "NKB/s"
//	>=1024 → "X.XMB/s"
func formatSpeedTag(speed int) string {
	if speed < 1024 {
		return fmt.Sprintf("%dKB/s", speed)
	}
	return fmt.Sprintf("%.1fMB/s", float64(speed)/1024)
}
