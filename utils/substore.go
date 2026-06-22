package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/beck-8/subs-check/config"
)

// ============================================================================
// sub-store 数据模型速览(后端是 schemaless 的,数据以 JSON 存盘,字段即下面这些)
//
// 两种顶层对象:
//   - subscription(订阅)  /api/sub/:name   —— 一组节点的来源。我们用名为 "sub"
//     的本地订阅(source=local)装检测后的节点(content 字段,yaml)。见 sub 结构体。
//   - file(文件)         /api/file/:name  —— 由某个 source 生成的产物文件。我们用
//     名为 "mihomo" 的 file(type=mihomoProfile, sourceName="sub")做 mihomo 覆写。
//     见 file 结构体。
//
// 两者都带一条 process 处理流水线(数组),每项是一个算子(Operator):
//   { type, args, disabled, customName, id }
//   - type:     算子类型,如 "Quick Setting Operator" / "Script Operator" / "Sort Operator"
//   - args:     参数,*类型随算子而变*(对象 / 字符串 / 数组都可能),所以这里用 any
//   - disabled: 是否禁用
//   - customName/id: 前端用的元数据。sub-store 后端处理时只读 type/args/disabled,
//     customName 仅作前端显示名,故我们借它当"这是 subs-check 的算子"的标记(见 overwriteOpMarker)
//
// 接口要点:
//   - 创建用 POST /api/subs、/api/files
//   - 更新用 PATCH /api/sub/:name、/api/file/:name,是*浅合并* {...old, ...body},
//     所以我们只发自己负责的字段,用户的其它改动会保留(见 updateSub / updatefile)
//   - 检查只看返回的 status 字段(见 statusResult)。不存在时两个接口都返回 HTTP 500
//     (sub-store 的 ResourceNotFoundError 把 404 误传成了 error.details,实际状态码回落到 500):
//     /api/sub 返回 500+HTML,/api/wholeFile 返回 500+JSON(status=failed)。故先判状态码再解析。
//   - 注意 /api/wholeFile 只返回存盘的原始对象、不做任何转换,所以 status!=success 只代表"文件不存在",
//     不是转换失败;真正会转换/可能转换失败的是 /api/file/:name(produce,失败时 500,不存在时 404)。
//
// 注:下面结构体里带 omitempty 但我们不写入的字段(displayName/ua/... )仅作字段说明用。
// 参考: ../sub-store/backend/src/restful/{subscriptions,file}.js
//        ../sub-store/backend/src/core/proxy-utils/processors/index.js
// ============================================================================

// 订阅对象，对应 sub-store /api/sub 的字段。
// 参考: ../sub-store/backend/src/restful/subscriptions.js
// 我们只写入需要的字段，其余加 omitempty 仅作字段说明，不会序列化。
type sub struct {
	Name                  string     `json:"name"`
	DisplayName           string     `json:"displayName,omitempty"`
	Source                string     `json:"source"` // local | remote
	URL                   string     `json:"url,omitempty"`
	Content               string     `json:"content,omitempty"`
	UA                    string     `json:"ua,omitempty"`
	MergeSources          string     `json:"mergeSources,omitempty"`
	IgnoreFailedRemoteSub bool       `json:"ignoreFailedRemoteSub,omitempty"`
	Proxy                 string     `json:"proxy,omitempty"`
	Process               []Operator `json:"process,omitempty"`
	Remark                string     `json:"remark,omitempty"`
	Tag                   []string   `json:"tag,omitempty"`
	SubscriptionTags      []string   `json:"subscriptionTags,omitempty"`
}

// 文件对象，对应 sub-store /api/file 的字段。
// 参考: ../sub-store/backend/src/restful/file.js
type file struct {
	Name                   string     `json:"name"`
	DisplayName            string     `json:"displayName,omitempty"`
	Source                 string     `json:"source"`               // local | remote
	SourceType             string     `json:"sourceType,omitempty"` // subscription | collection
	SourceName             string     `json:"sourceName,omitempty"`
	Type                   string     `json:"type"` // mihomoProfile | ...
	URL                    string     `json:"url,omitempty"`
	Content                string     `json:"content,omitempty"`
	UA                     string     `json:"ua,omitempty"`
	MergeSources           string     `json:"mergeSources,omitempty"`
	IgnoreFailedRemoteFile bool       `json:"ignoreFailedRemoteFile,omitempty"`
	Proxy                  string     `json:"proxy,omitempty"`
	Process                []Operator `json:"process,omitempty"`
	Remark                 string     `json:"remark,omitempty"`
}

// 处理算子 (process item)。sub-store 中 args 的类型随算子而变：
// Script/Quick Setting Operator 为对象，Sort Operator 为字符串 "asc"，
// Regex 系为字符串/数组等，故 Args 用 any。
// 参考: ../sub-store/backend/src/core/proxy-utils/processors/index.js
type Operator struct {
	Type       string `json:"type"`
	Args       any    `json:"args,omitempty"`
	Disabled   bool   `json:"disabled,omitempty"`
	CustomName string `json:"customName,omitempty"` // 用作我们覆写算子的识别标记
}

// Script Operator 的 args
type scriptArgs struct {
	Content string `json:"content"`
	Mode    string `json:"mode"`
}

// 仅用于检查接口返回是否成功；不解析 data，因为 Operator 的 args 类型不固定，强解析会失败。
type statusResult struct {
	Status string `json:"status"`
}

const (
	SubName    = "sub"
	MihomoName = "mihomo"
	// 我们覆写算子的识别标记，写在 process item 的 customName 上。
	// sub-store 处理时只读 type/args/disabled，customName 仅作前端展示，可安全用作标记。
	overwriteOpMarker = "subs-check专用,勿动"
)

// 用来判断用户是否在运行时更改了覆写订阅的url
var mihomoOverwriteUrl string

// 基础URL配置
var BaseURL string

func UpdateSubStore(yamlData []byte) {
	// 调试的时候等一等node启动
	if os.Getenv("SUB_CHECK_SKIP") != "" && config.GlobalConfig.SubStorePort != "" {
		time.Sleep(time.Second * 1)
	}
	// 处理用户输入的格式
	config.GlobalConfig.SubStorePort = formatPort(config.GlobalConfig.SubStorePort)
	// 设置基础URL
	BaseURL = fmt.Sprintf("http://127.0.0.1%s", config.GlobalConfig.SubStorePort)
	if config.GlobalConfig.SubStorePath != "" {
		BaseURL = fmt.Sprintf("%s%s", BaseURL, config.GlobalConfig.SubStorePath)
	}

	if err := checkSub(); err != nil {
		slog.Debug(fmt.Sprintf("检查sub配置文件失败: %v, 正在创建中...", err))
		if err := createSub(yamlData); err != nil {
			slog.Error(fmt.Sprintf("创建sub配置文件失败: %v", err))
			return
		}
	}
	if config.GlobalConfig.MihomoOverwriteUrl == "" {
		slog.Error("mihomo覆写订阅url未设置")
		return
	}
	if err := checkfile(); err != nil {
		slog.Debug(fmt.Sprintf("检查mihomo配置文件失败: %v, 正在创建中...", err))
		if err := createfile(); err != nil {
			slog.Error(fmt.Sprintf("创建mihomo配置文件失败: %v", err))
			return
		}
		mihomoOverwriteUrl = config.GlobalConfig.MihomoOverwriteUrl
	}
	if err := updateSub(yamlData); err != nil {
		slog.Error(fmt.Sprintf("更新sub配置文件失败: %v", err))
		return
	}
	if config.GlobalConfig.MihomoOverwriteUrl != mihomoOverwriteUrl {
		if err := updatefile(); err != nil {
			slog.Error(fmt.Sprintf("更新mihomo配置文件失败: %v", err))
			return
		}
		mihomoOverwriteUrl = config.GlobalConfig.MihomoOverwriteUrl
	}
	slog.Info("substore更新完成")
}
func checkSub() error {
	resp, err := http.Get(fmt.Sprintf("%s/api/sub/%s", BaseURL, SubName))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	// sub-store 对不存在的 sub 会返回 500 + HTML(而非干净的 JSON),
	// 先判状态码,避免把 HTML 丢给 json 解析报出 "invalid character '<'" 这种迷惑日志
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("sub 不存在或 sub-store 未就绪 (HTTP %d)", resp.StatusCode)
	}
	var result statusResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析 sub 响应失败: %w", err)
	}
	if result.Status != "success" {
		return fmt.Errorf("获取sub配置文件失败")
	}
	return nil
}
func createSub(data []byte) error {
	// sub-store 上传默认限制1MB
	sub := sub{
		Content: string(data),
		Name:    "sub",
		Remark:  "subs-check专用,勿动",
		Source:  "local",
		Process: []Operator{
			{Type: "Quick Setting Operator"},
		},
	}
	json, err := json.Marshal(sub)
	if err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("%s/api/subs", BaseURL), "application/json", bytes.NewBuffer(json))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("创建sub配置文件失败,错误码:%d", resp.StatusCode)
	}
	return nil
}

func updateSub(data []byte) error {
	// PATCH 是浅合并 ({...old, ...body})，只发 content 即可刷新节点，
	// 用户对 sub 的其它改动 (process/remark/tag 等) 都会保留。
	payload, err := json.Marshal(map[string]string{"content": string(data)})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("%s/api/sub/%s", BaseURL, SubName),
		bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("更新sub配置文件失败,错误码:%d", resp.StatusCode)
	}
	return nil
}

func checkfile() error {
	resp, err := http.Get(fmt.Sprintf("%s/api/wholeFile/%s", BaseURL, MihomoName))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("mihomo 文件不存在或 sub-store 未就绪 (HTTP %d)", resp.StatusCode)
	}
	var result statusResult
	if err := json.Unmarshal(body, &result); err != nil {
		return fmt.Errorf("解析 mihomo 文件响应失败: %w", err)
	}
	if result.Status != "success" {
		return fmt.Errorf("获取mihomo配置文件失败")
	}
	return nil
}
func createfile() error {
	sourceName := config.GlobalConfig.MihomoOverwriteName
	if sourceName == "" {
		sourceName = "sub"
	}
	sourceType := config.GlobalConfig.MihomoOverwriteType
	if sourceType == "" {
		sourceType = "subscription"
	}
	file := file{
		Name: MihomoName,
		Process: []Operator{
			{
				Type: "Script Operator",
				Args: scriptArgs{
					Content: WarpUrl(config.GlobalConfig.MihomoOverwriteUrl),
					Mode:    "link",
				},
				CustomName: overwriteOpMarker,
			},
		},
		Remark:     "subs-check专用,勿动",
		Source:     "local",
		SourceName: sourceName,
		SourceType: sourceType,
		Type:       "mihomoProfile",
	}
	json, err := json.Marshal(file)
	if err != nil {
		return err
	}
	resp, err := http.Post(fmt.Sprintf("%s/api/files", BaseURL), "application/json", bytes.NewBuffer(json))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("创建mihomo配置文件失败,错误码:%d", resp.StatusCode)
	}
	return nil
}

func updatefile() error {
	sourceName := config.GlobalConfig.MihomoOverwriteName
	if sourceName == "" {
		sourceName = "sub"
	}
	sourceType := config.GlobalConfig.MihomoOverwriteType
	if sourceType == "" {
		sourceType = "subscription"
	}
	// 把现有 file 整个拉出来，只改我们自己的 Script Operator 的覆写 URL，
	// 再用 PATCH (浅合并) 只发回 process。这样用户加的其它算子和文件的其它字段都保留。
	resp, err := http.Get(fmt.Sprintf("%s/api/wholeFile/%s", BaseURL, MihomoName))
	if err != nil {
		return err
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return err
	}
	var result struct {
		Data struct {
			Process []map[string]any `json:"process"`
		} `json:"data"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return err
	}
	if result.Status != "success" {
		return fmt.Errorf("获取mihomo配置文件失败")
	}

	newContent := WarpUrl(config.GlobalConfig.MihomoOverwriteUrl)
	process := result.Data.Process
	found := false
	changed := false

	// 1) 优先按标记找我们的算子，避免和用户自己加的 Script Operator 混淆
	for _, op := range process {
		if op["customName"] != overwriteOpMarker {
			continue
		}
		found = true
		if a, ok := op["args"].(map[string]any); ok {
			if c, _ := a["content"].(string); c != newContent {
				a["content"] = newContent
				changed = true
			}
		}
		break
	}
	// 2) 兼容老数据(标记出现前创建的)：取第一个 mode=link 的 Script Operator，
	//    改 content 的同时补上标记，之后就能精确匹配
	if !found {
		for _, op := range process {
			if op["type"] != "Script Operator" {
				continue
			}
			if a, ok := op["args"].(map[string]any); ok && a["mode"] == "link" {
				a["content"] = newContent
				op["customName"] = overwriteOpMarker
				found = true
				changed = true
				break
			}
		}
	}
	// 3) 都没有(用户删了)，把带标记的算子放到最前面
	if !found {
		process = append([]map[string]any{{
			"type":       "Script Operator",
			"args":       map[string]any{"content": newContent, "mode": "link"},
			"customName": overwriteOpMarker,
		}}, process...)
		changed = true
	}

	// 内容没有变化就不发 PATCH，也不打日志——避免"我没改却每次都说已更新"
	if !changed {
		return nil
	}

	payload, err := json.Marshal(map[string]any{
		"process":    process,
		"sourceName": sourceName,
		"sourceType": sourceType,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPatch,
		fmt.Sprintf("%s/api/file/%s", BaseURL, MihomoName),
		bytes.NewBuffer(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("更新mihomo配置文件失败,错误码:%d", resp.StatusCode)
	}
	slog.Debug("mihomo覆写订阅url已更新")
	return nil
}

// 如果用户监听了局域网IP，后续会请求失败
func formatPort(port string) string {
	if strings.Contains(port, ":") {
		parts := strings.Split(port, ":")
		return ":" + parts[len(parts)-1]
	}
	return ":" + port
}

func WarpUrl(url string) string {
	url = formatTimePlaceholders(url, time.Now())

	// 如果url中以https://raw.githubusercontent.com开头，那么就使用github代理
	if strings.HasPrefix(url, "https://raw.githubusercontent.com") {
		return config.GlobalConfig.GithubProxy + url
	}
	return url
}

// 动态时间占位符
// 支持在链接中使用时间占位符，会自动替换成当前日期/时间:
// - `{Y}` - 四位年份 (2023)
// - `{m}` - 两位月份 (01-12)
// - `{d}` - 两位日期 (01-31)
// - `{Ymd}` - 组合日期 (20230131)
// - `{Y_m_d}` - 下划线分隔 (2023_01_31)
// - `{Y-m-d}` - 横线分隔 (2023-01-31)
//
// 所有占位符均支持可选的天偏移后缀 `±N`（单位：天）：
// - `{Ymd+1}` - 明天的组合日期
// - `{Y-m-d-7}` - 7 天前的横线日期
// - `{Y+1}` - 明天那天的年份（通常不变，跨年才变化）
// 偏移统一按"天"计算，不存在月/年进位的歧义。
var timePlaceholderRe = regexp.MustCompile(`\{(Ymd|Y_m_d|Y-m-d|Y|m|d)([+-]\d+)?\}`)

var timePlaceholderLayouts = map[string]string{
	"Y":     "2006",
	"m":     "01",
	"d":     "02",
	"Ymd":   "20060102",
	"Y_m_d": "2006_01_02",
	"Y-m-d": "2006-01-02",
}

func formatTimePlaceholders(url string, t time.Time) string {
	return timePlaceholderRe.ReplaceAllStringFunc(url, func(s string) string {
		m := timePlaceholderRe.FindStringSubmatch(s)
		offset := 0
		if m[2] != "" {
			offset, _ = strconv.Atoi(m[2])
		}
		return t.AddDate(0, 0, offset).Format(timePlaceholderLayouts[m[1]])
	})
}
