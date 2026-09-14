package check

import "testing"

// wireguard/masque/openvpn/zerotier 配置 remote-dns-resolve + dns 时，adapter.ParseProxy 会调用
// dns.ParseNameServer；它只在 mihomo/config 的 init() 中赋值，check.go 不引入该包会 nil pointer panic。
func TestCreateClientWithRemoteDNS(t *testing.T) {
	client := CreateClient(map[string]any{
		"name":               "wg-remote-dns",
		"type":               "wireguard",
		"server":             "192.0.2.1",
		"port":               2480,
		"ip":                 "172.16.0.2",
		"private-key":        "eCtXsJZ27+4PbhDkHnB923tkUn2Gj59wZw5wFA75MnU=",
		"public-key":         "Cr8hWlKvtDt7nrvf+f0brNQQzabAqrjfBvas9pmowjo=",
		"remote-dns-resolve": true,
		"dns":                []string{"1.1.1.1", "2606:4700:4700::1111"},
	})
	if client == nil {
		t.Fatal("CreateClient returned nil")
	}
	client.Close()
}
