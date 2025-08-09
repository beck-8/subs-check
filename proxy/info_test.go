package proxies

import (
	"net/http"
	"os"
	"testing"

	"github.com/beck-8/subs-check/assets"
	"github.com/sinspired/checkip/pkg/ipinfo"
)

func TestGetMixed(t *testing.T) {
	// 使用 subs-check 自己的 assets 包
	db, err := assets.OpenMaxMindDB("")

	if err != nil {
		t.Errorf("打开 MaxMind 数据库失败: %v", err)
		// 数据库打开失败时，设置为 nil，后续代码会处理这种情况
		db = nil
	}

	// 确保数据库在函数结束时关闭
	if db != nil {
		defer func() {
			if err := db.Close(); err != nil {
				t.Errorf("关闭 MaxMind 数据库失败: %v", err)
			}
		}()
	}
	httpClient := &http.Client{}
	// 设置一个临时环境变量，以排除部分api数据库未更新返回的 CN
	os.Setenv("SUBS-CHECK-CALL", "true")
	defer os.Unsetenv("SUBS-CHECK-CALL")
	loc, ip, countryCode_tag, err := ipinfo.GetMixed(httpClient, db)
	if err != nil {
		t.Errorf("获取代理国家信息失败: %v", err)
	} else {
		t.Logf("位置: %s, IP: %s, 标签: %s", loc, ip, countryCode_tag)
		if loc == "" || ip == "" || countryCode_tag == "" {
			t.Error("获取的国家信息或IP地址不完整")
		}
	}
}
