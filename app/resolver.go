package app

import (
	"fmt"
	"log/slog"
	"net"
	"strings"

	"github.com/beck-8/subs-check/config"
	"github.com/metacubex/mihomo/component/resolver"
	_ "github.com/metacubex/mihomo/config" // init() sets dns.ParseNameServer, used by parseNameservers
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

	// The global IPv6 toggle applies to both the system and custom resolvers.
	resolver.DisableIPv6 = !config.GlobalConfig.IPv6

	if !c.Enable {
		return nil
	}

	if len(c.DefaultNameserver) == 0 {
		c.DefaultNameserver = defaultBootstrapNameservers
	}
	valid, err := validateBootstrapIPs(c.DefaultNameserver)
	if err != nil {
		return err
	}
	c.DefaultNameserver = valid
	if len(c.Nameserver) == 0 {
		c.Nameserver = c.DefaultNameserver
	}
	if len(c.ProxyServerNameserver) == 0 {
		c.ProxyServerNameserver = c.Nameserver
	}

	main, err := parseNameservers(c.Nameserver, "nameserver")
	if err != nil {
		return err
	}
	proxySrv, err := parseNameservers(c.ProxyServerNameserver, "proxy-server-nameserver")
	if err != nil {
		return err
	}
	def, err := parseNameservers(c.DefaultNameserver, "default-nameserver")
	if err != nil {
		return err
	}

	rs := dns.NewResolver(dns.Config{
		Main:        main,
		Default:     def,
		ProxyServer: proxySrv,
		IPv6:        config.GlobalConfig.IPv6,
	})

	resolver.DefaultResolver = rs.Resolver
	resolver.ProxyServerHostResolver = rs.ProxyResolver

	slog.Info("DNS resolver 使用自定义 DNS",
		"nameserver", len(main),
		"proxy-server", len(proxySrv),
		"default", len(def),
		"ipv6", config.GlobalConfig.IPv6)
	return nil
}

// parseNameservers converts nameserver strings into dns.NameServer with mihomo's parser,
// so the syntax matches mihomo's dns section (bare IP becomes UDP:53).
// Invalid entries are warn-skipped; an error is returned only when all entries are invalid.
// fieldName is used in log warnings to point users at the offending config field.
func parseNameservers(servers []string, fieldName string) ([]dns.NameServer, error) {
	out := make([]dns.NameServer, 0, len(servers))
	for _, s := range servers {
		// Parse one by one so a single bad entry doesn't reject the whole list.
		ns, err := dns.ParseNameServer([]string{s})
		if err != nil {
			slog.Warn(fieldName+" 跳过无效项", "value", s, "reason", err)
			continue
		}
		out = append(out, ns...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s 全部无效，至少需要一个有效项", fieldName)
	}
	return out, nil
}

// validateBootstrapIPs filters default-nameserver entries to those that are literal IPs,
// warning about (and dropping) invalid ones. Returns an error only when nothing remains.
// Accepts: "1.1.1.1", "1.1.1.1:5353", "::1", "[::1]:5353", etc.
// Hostnames are rejected — bootstrap can't depend on DNS to resolve itself.
func validateBootstrapIPs(servers []string) ([]string, error) {
	valid := make([]string, 0, len(servers))
	for _, ns := range servers {
		host := ns
		// SplitHostPort handles both IPv4:port and bracketed IPv6:port.
		if h, _, err := net.SplitHostPort(ns); err == nil {
			host = h
		}
		// Bare bracketed IPv6 like "[::1]" without port.
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
		if net.ParseIP(host) == nil {
			slog.Warn("default-nameserver 跳过无效 IP", "value", ns)
			continue
		}
		valid = append(valid, ns)
	}
	if len(valid) == 0 {
		return nil, fmt.Errorf("default-nameserver 全部无效，至少需要一个有效 IP")
	}
	return valid, nil
}
