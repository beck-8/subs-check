package assets

import (
	"testing"
)

func TestGetCfCdnIPRanges(t *testing.T) {
	ipRanges := GetCfCdnIPRanges()
	if ipRanges == nil {
		t.Fatal("GetCfCdnIPRanges 返回 nil")
	}
	if len(ipRanges["ipv4"]) == 0 {
		t.Error("未加载到任何 IPv4 段")
	}
	if len(ipRanges["ipv6"]) == 0 {
		t.Error("未加载到任何 IPv6 段")
	}
	for _, ipnet := range ipRanges["ipv4"] {
		t.Logf("IPv4: %s", ipnet.String())
	}
	for _, ipnet := range ipRanges["ipv6"] {
		t.Logf("IPv6: %s", ipnet.String())
	}
}
