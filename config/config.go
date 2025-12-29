package config

import _ "embed"

type Config struct {
	PrintProgress        bool     `yaml:"print-progress"`
	Concurrent           int      `yaml:"concurrent"`
	CheckInterval        int      `yaml:"check-interval"`
	CronExpression       string   `yaml:"cron-expression"`
	AliveTestUrl         string   `yaml:"alive-test-url"`
	SpeedTestUrl         string   `yaml:"speed-test-url"`
	DownloadTimeout      int      `yaml:"download-timeout"`
	DownloadMB           int      `yaml:"download-mb"`
	TotalSpeedLimit      int      `yaml:"total-speed-limit"`
	MinSpeed             int      `yaml:"min-speed"`
	Timeout              int      `yaml:"timeout"`
	MediaCheckTimeout    int      `yaml:"media-check-timeout"`
	FilterRegex          string   `yaml:"filter-regex"`
	SaveMethod           string   `yaml:"save-method"`
	WebDAVURL            string   `yaml:"webdav-url"`
	WebDAVUsername       string   `yaml:"webdav-username"`
	WebDAVPassword       string   `yaml:"webdav-password"`
	GithubToken          string   `yaml:"github-token"`
	GithubGistID         string   `yaml:"github-gist-id"`
	GithubAPIMirror      string   `yaml:"github-api-mirror"`
	WorkerURL            string   `yaml:"worker-url"`
	WorkerToken          string   `yaml:"worker-token"`
	S3Endpoint           string   `yaml:"s3-endpoint"`
	S3AccessID           string   `yaml:"s3-access-id"`
	S3SecretKey          string   `yaml:"s3-secret-key"`
	S3Bucket             string   `yaml:"s3-bucket"`
	S3UseSSL             bool     `yaml:"s3-use-ssl"`
	S3BucketLookup       string   `yaml:"s3-bucket-lookup"`
	SubUrlsReTry         int      `yaml:"sub-urls-retry"`
	SubUrlsRetryInterval int      `yaml:"sub-urls-retry-interval"`
	SubUrlsTimeout       int      `yaml:"sub-urls-timeout"`
	SubUrlsGetUA         string   `yaml:"sub-urls-get-ua"`
	SubUrlsRemote        []string `yaml:"sub-urls-remote"`
	SubUrls              []string `yaml:"sub-urls"`
	SuccessRate          float32  `yaml:"success-rate"`
	MihomoApiUrl         string   `yaml:"mihomo-api-url"`
	MihomoApiSecret      string   `yaml:"mihomo-api-secret"`
	ListenPort           string   `yaml:"listen-port"`
	RenameNode           bool     `yaml:"rename-node"`
	KeepSuccessProxies   bool     `yaml:"keep-success-proxies"`
	OutputDir            string   `yaml:"output-dir"`
	AppriseApiServer     string   `yaml:"apprise-api-server"`
	RecipientUrl         []string `yaml:"recipient-url"`
	NotifyTitle          string   `yaml:"notify-title"`
	SubStorePort         string   `yaml:"sub-store-port"`
	SubStorePath         string   `yaml:"sub-store-path"`
	SubStoreSyncCron     string   `yaml:"sub-store-sync-cron"`
	SubStorePushService  string   `yaml:"sub-store-push-service"`
	SubStoreProduceCron  string   `yaml:"sub-store-produce-cron"`
	MihomoOverwriteUrl   string   `yaml:"mihomo-overwrite-url"`
	MediaCheck           bool     `yaml:"media-check"`
	Platforms            []string `yaml:"platforms"`
	SuccessLimit         int32    `yaml:"success-limit"`
	NodePrefix           string   `yaml:"node-prefix"`
	NodeType             []string `yaml:"node-type"`
	EnableWebUI          bool     `yaml:"enable-web-ui"`
	APIKey               string   `yaml:"api-key"`
	GithubProxy          string   `yaml:"github-proxy"`
	Proxy                string   `yaml:"proxy"`
	CallbackScript       string   `yaml:"callback-script"`
}

var GlobalConfig = &Config{
	// 新增配置，给未更改配置文件的用户一个默认值
	ListenPort:         ":8199",
	NotifyTitle:        "🔔 节点状态更新",
	MihomoOverwriteUrl: "http://127.0.0.1:8199/sub/ACL4SSR_Online_Full.yaml",
	MediaCheckTimeout:  10,
	Platforms:          []string{"openai", "youtube", "netflix", "disney", "gemini", "iprisk"},
	DownloadMB:         20,
	AliveTestUrl:       "http://gstatic.com/generate_204",
	SubUrlsGetUA:       "clash.meta (https://github.com/beck-8/subs-check)",
}

//go:embed config.example.yaml
var DefaultConfigTemplate []byte

var GlobalProxies []map[string]any
