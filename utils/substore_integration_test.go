//go:build substore_it

// 真实启动一个临时 sub-store(独立端口 + 独立数据目录),对 substore.go 的增删改查做端到端测试。
// 默认 go test 不会跑,需要显式带 tag:
//
//	go test -tags substore_it ./utils/ -run TestSubStore -v
//
// 依赖当前平台已嵌入的 node 二进制(assets.EmbeddedNode),与正式运行用的是同一份。
package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/beck-8/subs-check/config"
	"github.com/klauspost/compress/zstd"
)

// 不能 import assets 包(会形成 assets -> save/method -> utils 的 import cycle),
// 所以直接读 ../assets 下的 .zst 资源文件,和正式运行用的是同一份。
func assetsDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "assets")
}

func nodeAssetName() string {
	arch := runtime.GOARCH
	switch arch {
	case "386":
		arch = "i386"
	case "arm":
		arch = "armv7"
	}
	return fmt.Sprintf("node_%s_%s.zst", runtime.GOOS, arch)
}

const overwriteMarker = overwriteOpMarker

// startTempSubStore 在临时目录里解压并启动一个 sub-store,返回它的 BaseURL。
// 数据写到独立的 SUB_STORE_DATA_BASE_PATH,不会碰用户的真实数据。
func startTempSubStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	nodeName := "node"
	if runtime.GOOS == "windows" {
		nodeName += ".exe"
	}
	ad := assetsDir()
	nodeSrc := filepath.Join(ad, nodeAssetName())
	if _, err := os.Stat(nodeSrc); err != nil {
		t.Skipf("当前平台没有内嵌 node 资源 (%s),跳过集成测试", nodeSrc)
	}
	nodePath := filepath.Join(dir, nodeName)
	jsPath := filepath.Join(dir, "sub-store.bundle.js")
	decodeAsset(t, nodeSrc, nodePath, 0o755)
	decodeAsset(t, filepath.Join(ad, "sub-store.bundle.js.zst"), jsPath, 0o644)

	port := freePort(t)
	cmd := exec.Command(nodePath, jsPath)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"SUB_STORE_DATA_BASE_PATH="+dir,
		"SUB_STORE_BACKEND_API_PORT="+port,
		"SUB_STORE_BACKEND_API_HOST=127.0.0.1",
		"SUB_STORE_BODY_JSON_LIMIT=30mb",
	)
	logFile, _ := os.Create(filepath.Join(dir, "sub-store.log"))
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		t.Fatalf("启动 sub-store 失败: %v", err)
	}
	t.Logf("临时 sub-store 已启动: pid=%d 端口=%s 运行目录=%s", cmd.Process.Pid, port, dir)
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		if logFile != nil {
			logFile.Close()
		}
	})

	base := "http://127.0.0.1:" + port
	waitReady(t, base)
	return base
}

func decodeAsset(t *testing.T, srcPath, dst string, mode os.FileMode) {
	t.Helper()
	data, err := os.ReadFile(srcPath)
	if err != nil {
		t.Fatalf("read asset %s: %v", srcPath, err)
	}
	dec, err := zstd.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("zstd reader: %v", err)
	}
	defer dec.Close()
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		t.Fatalf("open %s: %v", dst, err)
	}
	if _, err := io.Copy(f, dec); err != nil {
		f.Close()
		t.Fatalf("decode %s: %v", dst, err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close %s: %v", dst, err)
	}
}

func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	defer l.Close()
	return strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
}

func waitReady(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/api/subs")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("sub-store 在超时内未就绪")
}

// patchRaw 直接发一个 PATCH,用于在测试里模拟"用户在 UI 里改了配置"。
func patchRaw(t *testing.T, url string, body any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPatch, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("patch %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("patch %s 状态码 %d", url, resp.StatusCode)
	}
}

// getProcess 拉取 sub 或 file 的 process 和 content。
func getProcess(t *testing.T, url string) ([]map[string]any, string) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("get %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var r struct {
		Data struct {
			Process []map[string]any `json:"process"`
			Content string           `json:"content"`
		} `json:"data"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("解析 %s 失败: %v\n%s", url, err, string(body))
	}
	if r.Status != "success" {
		t.Fatalf("get %s status=%s", url, r.Status)
	}
	return r.Data.Process, r.Data.Content
}

func hasOperatorType(process []map[string]any, typ string) bool {
	for _, op := range process {
		if op["type"] == typ {
			return true
		}
	}
	return false
}

func TestSubStoreCRUD(t *testing.T) {
	base := startTempSubStore(t)

	// substore.go 里的函数读全局,测试里设置好
	BaseURL = base
	config.GlobalConfig.GithubProxy = ""
	config.GlobalConfig.MihomoOverwriteUrl = "http://127.0.0.1:9/strategy-v1.yaml"
	mihomoOverwriteUrl = ""

	// ---- sub: 创建 ----
	if err := checkSub(); err == nil {
		t.Fatal("sub 尚未创建,checkSub 应该失败")
	}
	if err := createSub([]byte("proxies:\n  - {name: a}\n")); err != nil {
		t.Fatalf("createSub: %v", err)
	}
	if err := checkSub(); err != nil {
		t.Fatalf("创建后 checkSub 应成功: %v", err)
	}

	// 模拟用户给 sub 加了一个 Sort Operator
	subURL := base + "/api/sub/" + SubName
	patchRaw(t, subURL, map[string]any{
		"process": []map[string]any{
			{"type": "Quick Setting Operator"},
			{"type": "Sort Operator", "args": "asc", "customName": "我的排序"},
		},
	})

	// ---- sub: 更新(只发 content),用户的 Sort Operator 必须保留 ----
	if err := updateSub([]byte("proxies:\n  - {name: b}\n  - {name: c}\n")); err != nil {
		t.Fatalf("updateSub: %v", err)
	}
	proc, content := getProcess(t, subURL)
	if !hasOperatorType(proc, "Sort Operator") {
		t.Errorf("updateSub 不应覆盖用户的 Sort Operator,实际 process=%v", proc)
	}
	if !bytes.Contains([]byte(content), []byte("name: b")) {
		t.Errorf("updateSub 后 content 未刷新: %q", content)
	}

	// ---- file: 创建,我们的算子应带标记 ----
	if err := checkfile(); err == nil {
		t.Fatal("file 尚未创建,checkfile 应该失败")
	}
	if err := createfile(); err != nil {
		t.Fatalf("createfile: %v", err)
	}
	if err := checkfile(); err != nil {
		t.Fatalf("创建后 checkfile 应成功: %v", err)
	}
	fileURL := base + "/api/file/" + MihomoName
	proc, _ = getProcess(t, base+"/api/wholeFile/"+MihomoName)
	if !markerPresent(proc) {
		t.Fatalf("createfile 后应有 customName 标记,实际 process=%v", proc)
	}

	// 模拟用户在 file 里又加了一个自己的算子
	ourArgs := map[string]any{"content": WarpUrl(config.GlobalConfig.MihomoOverwriteUrl), "mode": "link"}
	patchRaw(t, fileURL, map[string]any{
		"process": []map[string]any{
			{"type": "Sort Operator", "args": "desc", "customName": "用户排序"},
			{"type": "Script Operator", "args": ourArgs, "customName": overwriteMarker},
		},
	})

	// ---- file: 更新覆写 URL,只动我们带标记的算子,用户算子保留 ----
	config.GlobalConfig.MihomoOverwriteUrl = "http://127.0.0.1:9/strategy-v2.yaml"
	if err := updatefile(); err != nil {
		t.Fatalf("updatefile: %v", err)
	}
	proc, _ = getProcess(t, base+"/api/wholeFile/"+MihomoName)
	if !hasOperatorType(proc, "Sort Operator") {
		t.Errorf("updatefile 不应删除用户的 Sort Operator,实际 process=%v", proc)
	}
	want := WarpUrl("http://127.0.0.1:9/strategy-v2.yaml")
	if got := markerContent(proc); got != want {
		t.Errorf("我们算子的 content 应更新为 %q,实际 %q", want, got)
	}
	if c := userScriptContent(proc); c != "" {
		t.Errorf("用户算子被误改了? 不应有变化,实际 userScript content=%q", c)
	}

	// ---- file: 幂等性——URL 没变时再次 updatefile 不应改动任何东西 ----
	before, _ := getProcess(t, base+"/api/wholeFile/"+MihomoName)
	if err := updatefile(); err != nil {
		t.Fatalf("第二次 updatefile: %v", err)
	}
	after, _ := getProcess(t, base+"/api/wholeFile/"+MihomoName)
	if len(after) != len(before) {
		t.Errorf("幂等更新不应改变算子数量: before=%d after=%d", len(before), len(after))
	}
	if markerContent(after) != want {
		t.Errorf("幂等更新不应改变 content: %q", markerContent(after))
	}
	if n := countMarker(after); n != 1 {
		t.Errorf("幂等更新不应重复插入带标记的算子,实际个数=%d", n)
	}
}

func countMarker(process []map[string]any) int {
	n := 0
	for _, op := range process {
		if op["customName"] == overwriteMarker {
			n++
		}
	}
	return n
}

// TestSubStoreLegacyFileMigration 验证老数据(无标记)首次更新会被识别并补上标记。
func TestSubStoreLegacyFileMigration(t *testing.T) {
	base := startTempSubStore(t)
	BaseURL = base
	config.GlobalConfig.GithubProxy = ""
	config.GlobalConfig.MihomoOverwriteUrl = "http://127.0.0.1:9/old.yaml"

	// 创建 sub(file 的 source 引用它)
	if err := createSub([]byte("proxies: []\n")); err != nil {
		t.Fatalf("createSub: %v", err)
	}

	// 直接 POST 一个"老格式" file:link 模式的 Script Operator,但没有 customName 标记
	legacy := map[string]any{
		"name": MihomoName,
		"process": []map[string]any{
			{"type": "Script Operator", "args": map[string]any{"content": "http://127.0.0.1:9/old.yaml", "mode": "link"}},
		},
		"source":     "local",
		"sourceName": "sub",
		"sourceType": "subscription",
		"type":       "mihomoProfile",
	}
	b, _ := json.Marshal(legacy)
	resp, err := http.Post(base+"/api/files", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("post legacy file: %v", err)
	}
	resp.Body.Close()

	// 更新:应按 type+mode 命中老算子,改 content 的同时补上标记
	config.GlobalConfig.MihomoOverwriteUrl = "http://127.0.0.1:9/new.yaml"
	if err := updatefile(); err != nil {
		t.Fatalf("updatefile: %v", err)
	}
	proc, _ := getProcess(t, base+"/api/wholeFile/"+MihomoName)
	if !markerPresent(proc) {
		t.Errorf("老数据更新后应补上标记,实际 process=%v", proc)
	}
	if got, want := markerContent(proc), WarpUrl("http://127.0.0.1:9/new.yaml"); got != want {
		t.Errorf("content 应更新为 %q,实际 %q", want, got)
	}
}

func markerPresent(process []map[string]any) bool {
	for _, op := range process {
		if op["customName"] == overwriteMarker {
			return true
		}
	}
	return false
}

func markerContent(process []map[string]any) string {
	for _, op := range process {
		if op["customName"] != overwriteMarker {
			continue
		}
		if a, ok := op["args"].(map[string]any); ok {
			if c, ok := a["content"].(string); ok {
				return c
			}
		}
	}
	return ""
}

// userScriptContent 返回非标记的 Script Operator 的 content(测试里期望它不变)。
func userScriptContent(process []map[string]any) string {
	for _, op := range process {
		if op["type"] != "Script Operator" || op["customName"] == overwriteMarker {
			continue
		}
		if a, ok := op["args"].(map[string]any); ok {
			if c, ok := a["content"].(string); ok {
				return c
			}
		}
	}
	return ""
}
