package save

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/beck-8/subs-check/check"
	"github.com/beck-8/subs-check/config"
)

// ---- marshalProxies ----

func TestMarshalProxies_Normal(t *testing.T) {
	results := []check.Result{
		{Proxy: map[string]any{"name": "node1", "type": "ss"}},
		{Proxy: map[string]any{"name": "node2", "type": "vmess"}},
	}
	data, err := marshalProxies(results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("expected non-empty yaml data")
	}
	// 检查输出包含 proxies 关键字
	if got := string(data); !contains(got, "proxies") {
		t.Errorf("yaml should contain 'proxies', got:\n%s", got)
	}
}

func TestMarshalProxies_Empty(t *testing.T) {
	_, err := marshalProxies(nil)
	if err == nil {
		t.Fatal("expected error for empty results")
	}

	_, err = marshalProxies([]check.Result{})
	if err == nil {
		t.Fatal("expected error for empty results slice")
	}
}

func TestMarshalProxies_SingleResult(t *testing.T) {
	results := []check.Result{
		{Proxy: map[string]any{"name": "only-node", "type": "trojan", "server": "1.2.3.4"}},
	}
	data, err := marshalProxies(results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := string(data)
	for _, keyword := range []string{"only-node", "trojan", "1.2.3.4"} {
		if !contains(got, keyword) {
			t.Errorf("yaml should contain %q, got:\n%s", keyword, got)
		}
	}
}

// ---- fetchSubStoreData ----

func TestFetchSubStoreData_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "mihomo-config-content")
	}))
	defer ts.Close()

	data := fetchSubStoreData(ts.URL, "mihomo.yaml")
	if string(data) != "mihomo-config-content" {
		t.Errorf("expected 'mihomo-config-content', got %q", string(data))
	}
}

func TestFetchSubStoreData_Non200(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, "not found")
	}))
	defer ts.Close()

	data := fetchSubStoreData(ts.URL, "mihomo.yaml")
	if data != nil {
		t.Errorf("expected nil for non-200 response, got %q", string(data))
	}
}

func TestFetchSubStoreData_ServerError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
	}))
	defer ts.Close()

	data := fetchSubStoreData(ts.URL, "base64.txt")
	if data != nil {
		t.Errorf("expected nil for 500 response, got %q", string(data))
	}
}

func TestFetchSubStoreData_InvalidURL(t *testing.T) {
	data := fetchSubStoreData("http://127.0.0.1:0/invalid", "test.yaml")
	if data != nil {
		t.Errorf("expected nil for connection error, got %q", string(data))
	}
}

// ---- saveIfNotEmpty ----

func TestSaveIfNotEmpty_WithData(t *testing.T) {
	called := false
	var gotData []byte
	var gotFilename string

	saver := func(data []byte, filename string) error {
		called = true
		gotData = data
		gotFilename = filename
		return nil
	}

	saveIfNotEmpty(saver, []byte("test-data"), "test.yaml")

	if !called {
		t.Fatal("saver should be called when data is not empty")
	}
	if string(gotData) != "test-data" {
		t.Errorf("expected data 'test-data', got %q", string(gotData))
	}
	if gotFilename != "test.yaml" {
		t.Errorf("expected filename 'test.yaml', got %q", gotFilename)
	}
}

func TestSaveIfNotEmpty_EmptyData(t *testing.T) {
	called := false
	saver := func(data []byte, filename string) error {
		called = true
		return nil
	}

	saveIfNotEmpty(saver, nil, "test.yaml")
	if called {
		t.Fatal("saver should not be called when data is nil")
	}

	saveIfNotEmpty(saver, []byte{}, "test.yaml")
	if called {
		t.Fatal("saver should not be called when data is empty")
	}
}

func TestSaveIfNotEmpty_SaverError(t *testing.T) {
	config.GlobalConfig.SaveMethod = "local"
	saver := func(data []byte, filename string) error {
		return fmt.Errorf("disk full")
	}

	// 不应 panic，错误只记录日志
	saveIfNotEmpty(saver, []byte("data"), "test.yaml")
}

// ---- newRemoteSaver ----

func TestNewRemoteSaver_UnknownMethod(t *testing.T) {
	config.GlobalConfig.SaveMethod = "ftp"
	_, err := newRemoteSaver()
	if err == nil {
		t.Fatal("expected error for unknown save method")
	}
	if !contains(err.Error(), "未知的保存方法") {
		t.Errorf("error should mention unknown method, got: %v", err)
	}
}

func TestNewRemoteSaver_R2MissingConfig(t *testing.T) {
	config.GlobalConfig.SaveMethod = "r2"
	config.GlobalConfig.WorkerURL = ""
	config.GlobalConfig.WorkerToken = ""

	_, err := newRemoteSaver()
	if err == nil {
		t.Fatal("expected error for incomplete R2 config")
	}
	if !contains(err.Error(), "R2") {
		t.Errorf("error should mention R2, got: %v", err)
	}
}

func TestNewRemoteSaver_GistMissingConfig(t *testing.T) {
	config.GlobalConfig.SaveMethod = "gist"
	config.GlobalConfig.GithubToken = ""
	config.GlobalConfig.GithubGistID = ""

	_, err := newRemoteSaver()
	if err == nil {
		t.Fatal("expected error for incomplete Gist config")
	}
	if !contains(err.Error(), "Gist") {
		t.Errorf("error should mention Gist, got: %v", err)
	}
}

func TestNewRemoteSaver_WebDAVMissingConfig(t *testing.T) {
	config.GlobalConfig.SaveMethod = "webdav"
	config.GlobalConfig.WebDAVURL = ""

	_, err := newRemoteSaver()
	if err == nil {
		t.Fatal("expected error for incomplete WebDAV config")
	}
	if !contains(err.Error(), "WebDAV") {
		t.Errorf("error should mention WebDAV, got: %v", err)
	}
}

func TestNewRemoteSaver_S3MissingConfig(t *testing.T) {
	config.GlobalConfig.SaveMethod = "s3"
	config.GlobalConfig.S3Endpoint = ""

	_, err := newRemoteSaver()
	if err == nil {
		t.Fatal("expected error for incomplete S3 config")
	}
	if !contains(err.Error(), "S3") {
		t.Errorf("error should mention S3, got: %v", err)
	}
}

func TestNewRemoteSaver_R2ValidConfig(t *testing.T) {
	config.GlobalConfig.SaveMethod = "r2"
	config.GlobalConfig.WorkerURL = "https://worker.example.com"
	config.GlobalConfig.WorkerToken = "test-token"

	saver, err := newRemoteSaver()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saver == nil {
		t.Fatal("expected non-nil saver")
	}
}

func TestNewRemoteSaver_GistValidConfig(t *testing.T) {
	config.GlobalConfig.SaveMethod = "gist"
	config.GlobalConfig.GithubToken = "ghp_test"
	config.GlobalConfig.GithubGistID = "abc123"

	saver, err := newRemoteSaver()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saver == nil {
		t.Fatal("expected non-nil saver")
	}
}

func TestNewRemoteSaver_WebDAVValidConfig(t *testing.T) {
	config.GlobalConfig.SaveMethod = "webdav"
	config.GlobalConfig.WebDAVURL = "https://dav.example.com"
	config.GlobalConfig.WebDAVUsername = "user"
	config.GlobalConfig.WebDAVPassword = "pass"

	saver, err := newRemoteSaver()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saver == nil {
		t.Fatal("expected non-nil saver")
	}
}

func TestNewRemoteSaver_S3ValidConfig(t *testing.T) {
	config.GlobalConfig.SaveMethod = "s3"
	config.GlobalConfig.S3Endpoint = "s3.example.com"
	config.GlobalConfig.S3AccessID = "access"
	config.GlobalConfig.S3SecretKey = "secret"
	config.GlobalConfig.S3Bucket = "bucket"

	saver, err := newRemoteSaver()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if saver == nil {
		t.Fatal("expected non-nil saver")
	}
}

// ---- helper ----

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
