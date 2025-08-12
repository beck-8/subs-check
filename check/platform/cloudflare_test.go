package platform

import (
	"context"
	"net/http"
	"testing"
	"time"

)

func TestGetCFProxy(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	httpClient := &http.Client{}
	success := false
	for _, url := range CF_CDN_APIS {
		loc, ip := FetchCFTrace(httpClient, ctx, url)
		if loc == "" || ip == "" {
			t.Logf("%s GetCFProxy 失败: loc=%s, ip=%s", url, loc, ip)
		} else {
			t.Logf("%s GetCFProxy 成功: loc=%s, ip=%s", url, loc, ip)
			success = true
		}
	}
	if !success {
		t.Error("GetCFProxy 失败: 所有 url 均未获取到 loc 和 ip")
	}
}

func TestFetchCFProxy(t *testing.T) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	loc, ip := GetCFTrace(httpClient)
	if loc == "" || ip == "" {
		t.Error("FetchCFProxy 失败: 未获取 loc 和 ip")
	} else {
		t.Logf("FetchCFProxy 成功: location=%s, IP=%s", loc, ip)
	}
}

func TestCheckCloudflare(t *testing.T) {
	client := &http.Client{Timeout: 10 * time.Second}
	ok, loc, ip := CheckCloudflare(client)
	if ok {
		t.Log("CheckCloudflare 成功访问")
		if loc != "" && ip != "" {
			t.Logf("Location: %s, IP: %s", loc, ip)
		} else {
			t.Log("cloudflare.com 成功访问, 未获取 loc 和 ip")
		}
	}else{
		if loc == "" && ip == "" {
			t.Log("cloudflare.com 成功访问, 未获取 loc 和 ip")
		} else {
			t.Errorf("CheckCloudflare 不可用节点: Location=%s, IP=%s", loc, ip)
		}
	}
}
