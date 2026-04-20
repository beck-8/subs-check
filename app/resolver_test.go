package app

import (
	"strings"
	"testing"

	"github.com/beck-8/subs-check/config"
	"github.com/metacubex/mihomo/component/resolver"
)

func TestParseNameservers(t *testing.T) {
	cases := []struct {
		in      string
		wantNet string
		wantAdr string
	}{
		{"223.5.5.5", "", "223.5.5.5:53"},
		{"223.5.5.5:5353", "", "223.5.5.5:5353"},
		{"udp://1.1.1.1", "", "1.1.1.1:53"},
		{"tcp://1.1.1.1:5353", "tcp", "1.1.1.1:5353"},
		{"tls://dns.alidns.com", "tls", "dns.alidns.com:853"},
		{"https://dns.alidns.com/dns-query", "https", "https://dns.alidns.com:443/dns-query"},
		{"https://1.1.1.1/dns-query", "https", "https://1.1.1.1:443/dns-query"},
		{"quic://dns.alidns.com", "quic", "dns.alidns.com:853"},
		{"udp://[::1]", "", "[::1]:53"},
		{"udp://[::1]:5353", "", "[::1]:5353"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			got, err := parseNameservers([]string{c.in})
			if err != nil {
				t.Fatalf("parse %q: %v", c.in, err)
			}
			if len(got) != 1 {
				t.Fatalf("want 1 result, got %d", len(got))
			}
			if got[0].Net != c.wantNet {
				t.Errorf("Net: got %q, want %q", got[0].Net, c.wantNet)
			}
			if got[0].Addr != c.wantAdr {
				t.Errorf("Addr: got %q, want %q", got[0].Addr, c.wantAdr)
			}
		})
	}
}

func TestParseNameserversErrors(t *testing.T) {
	_, err := parseNameservers([]string{"ftp://example.com"})
	if err == nil {
		t.Errorf("expected error for unsupported scheme")
	}
}

func TestInitResolverFallbacks(t *testing.T) {
	saved := config.GlobalConfig.DNS
	t.Cleanup(func() { config.GlobalConfig.DNS = saved })

	t.Run("disabled is no-op", func(t *testing.T) {
		config.GlobalConfig.DNS = config.DNSConfig{Enable: false}
		if err := initResolver(); err != nil {
			t.Errorf("disabled init should not error, got %v", err)
		}
	})

	t.Run("enabled with all empty falls back to bootstrap defaults", func(t *testing.T) {
		config.GlobalConfig.DNS = config.DNSConfig{Enable: true}
		if err := initResolver(); err != nil {
			t.Fatalf("init failed: %v", err)
		}
		// Mutation is in-place; verify both fields filled from defaults via the chain.
		c := config.GlobalConfig.DNS
		if len(c.DefaultNameserver) == 0 {
			t.Errorf("default-nameserver not filled")
		}
		if len(c.Nameserver) == 0 {
			t.Errorf("nameserver not filled (expected fallback to default)")
		}
		if len(c.ProxyServerNameserver) == 0 {
			t.Errorf("proxy-server-nameserver not filled (expected fallback to nameserver)")
		}
	})

	t.Run("only nameserver set fills proxy-server but keeps default-nameserver default", func(t *testing.T) {
		config.GlobalConfig.DNS = config.DNSConfig{
			Enable:     true,
			Nameserver: []string{"https://dns.alidns.com/dns-query"},
		}
		if err := initResolver(); err != nil {
			t.Fatalf("init failed: %v", err)
		}
		c := config.GlobalConfig.DNS
		if c.Nameserver[0] != "https://dns.alidns.com/dns-query" {
			t.Errorf("nameserver overwritten: %v", c.Nameserver)
		}
		if c.ProxyServerNameserver[0] != "https://dns.alidns.com/dns-query" {
			t.Errorf("proxy-server-nameserver should fall back to nameserver, got %v", c.ProxyServerNameserver)
		}
		if len(c.DefaultNameserver) == 0 || c.DefaultNameserver[0] != "223.5.5.5" {
			t.Errorf("default-nameserver should default to 223.5.5.5, got %v", c.DefaultNameserver)
		}
	})

	t.Run("invalid scheme returns error", func(t *testing.T) {
		config.GlobalConfig.DNS = config.DNSConfig{
			Enable:     true,
			Nameserver: []string{"ftp://example.com"},
		}
		err := initResolver()
		if err == nil || !strings.Contains(err.Error(), "unsupported DNS scheme") {
			t.Errorf("expected scheme error, got %v", err)
		}
	})

	t.Run("enabled init replaces global resolvers", func(t *testing.T) {
		// Snapshot globals so we can verify they actually change.
		savedDefault := resolver.DefaultResolver
		savedProxy := resolver.ProxyServerHostResolver
		t.Cleanup(func() {
			resolver.DefaultResolver = savedDefault
			resolver.ProxyServerHostResolver = savedProxy
		})

		config.GlobalConfig.DNS = config.DNSConfig{Enable: true}
		if err := initResolver(); err != nil {
			t.Fatalf("init failed: %v", err)
		}
		if resolver.DefaultResolver == nil {
			t.Errorf("DefaultResolver not set")
		}
		if resolver.ProxyServerHostResolver == nil {
			t.Errorf("ProxyServerHostResolver not set")
		}
	})
}
