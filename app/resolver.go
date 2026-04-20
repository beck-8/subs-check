package app

import (
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/beck-8/subs-check/config"
	"github.com/metacubex/mihomo/component/resolver"
	"github.com/metacubex/mihomo/dns"
)

// defaultBootstrapNameservers 是 default-nameserver 留空时的兜底，必须是纯 IP。
var defaultBootstrapNameservers = []string{
	"223.5.5.5",
	"119.29.29.29",
}

// initResolver wires mihomo's global resolver based on user config.
// Call after loadConfig() and before any proxy.DialContext.
//
// Fallback chain when Enable=true:
//
//	default-nameserver → defaultBootstrapNameservers
//	nameserver         → default-nameserver
//	proxy-server-nameserver → nameserver
func initResolver() error {
	c := &config.GlobalConfig.DNS

	// IPv6 toggle applies regardless of Enable — a user can flip on v6 without replacing the resolver.
	resolver.DisableIPv6 = !c.IPv6

	if !c.Enable {
		slog.Info("DNS resolver 使用 mihomo 默认", "ipv6", c.IPv6)
		return nil
	}

	if len(c.DefaultNameserver) == 0 {
		c.DefaultNameserver = defaultBootstrapNameservers
	}
	if len(c.Nameserver) == 0 {
		c.Nameserver = c.DefaultNameserver
	}
	if len(c.ProxyServerNameserver) == 0 {
		c.ProxyServerNameserver = c.Nameserver
	}

	main, err := parseNameservers(c.Nameserver)
	if err != nil {
		return fmt.Errorf("解析 nameserver 失败: %w", err)
	}
	proxySrv, err := parseNameservers(c.ProxyServerNameserver)
	if err != nil {
		return fmt.Errorf("解析 proxy-server-nameserver 失败: %w", err)
	}
	def, err := parseNameservers(c.DefaultNameserver)
	if err != nil {
		return fmt.Errorf("解析 default-nameserver 失败: %w", err)
	}

	rs := dns.NewResolver(dns.Config{
		Main:        main,
		Default:     def,
		ProxyServer: proxySrv,
		IPv6:        c.IPv6,
	})

	resolver.DefaultResolver = rs.Resolver
	resolver.ProxyServerHostResolver = rs.ProxyResolver

	slog.Info("DNS resolver 已初始化",
		"main", len(main),
		"proxy-server", len(proxySrv),
		"default", len(def),
		"ipv6", c.IPv6)
	return nil
}

// parseNameservers converts string URLs into dns.NameServer. A bare IP is treated as UDP:53.
// Supports: udp://, tcp://, tls://, https://, http://, quic://.
func parseNameservers(servers []string) ([]dns.NameServer, error) {
	out := make([]dns.NameServer, 0, len(servers))
	for _, s := range servers {
		// Bare IP or host[:port] gets the udp:// prefix.
		if !strings.Contains(s, "://") {
			s = "udp://" + s
		}
		u, err := url.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("%q: %w", s, err)
		}
		ns := dns.NameServer{}
		switch u.Scheme {
		case "udp":
			ns.Addr = hostPort(u.Host, "53")
		case "tcp":
			ns.Net = "tcp"
			ns.Addr = hostPort(u.Host, "53")
		case "tls":
			ns.Net = "tls"
			ns.Addr = hostPort(u.Host, "853")
		case "https", "http":
			ns.Net = "https"
			defPort := "443"
			if u.Scheme == "http" {
				defPort = "80"
			}
			cleaned := url.URL{Scheme: u.Scheme, Host: hostPort(u.Host, defPort), Path: u.Path}
			ns.Addr = cleaned.String()
		case "quic":
			ns.Net = "quic"
			ns.Addr = hostPort(u.Host, "853")
		default:
			return nil, fmt.Errorf("%q: unsupported DNS scheme %q", s, u.Scheme)
		}
		out = append(out, ns)
	}
	return out, nil
}

func hostPort(host, defPort string) string {
	if host == "" {
		return ":" + defPort
	}
	// IPv6 literal already has its own [::1] wrapping; just check for a trailing :port.
	if idx := strings.LastIndex(host, ":"); idx > strings.LastIndex(host, "]") {
		return host
	}
	return host + ":" + defPort
}
