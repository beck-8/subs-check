package assets

import (
	"bufio"
	"log"
	"log/slog"
	"net"
	"strings"
	"sync"
)

var (
	cfCdnIPRanges map[string][]*net.IPNet
	loadOnce      sync.Once
	loadError     error
)

func loadCfCdnIPRanges() {
	cfCdnIPRanges = make(map[string][]*net.IPNet)
	ipContents := map[string]string{
		"ipv4": embeddedIPv4,
		"ipv6": embeddedIPv6,
	}

	totalLoaded := 0
	for version, content := range ipContents {
		var ipNets []*net.IPNet
		scanner := bufio.NewScanner(strings.NewReader(content))
		lineCount := 0

		for scanner.Scan() {
			lineCount++
			line := strings.TrimSpace(scanner.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}

			_, ipNet, err := net.ParseCIDR(line)
			if err != nil {
				log.Printf("Warning: Failed to parse CIDR %s on line %d: %v", line, lineCount, err)
				continue
			}
			ipNets = append(ipNets, ipNet)
			slog.Debug("Loaded Cloudflare CDN IP range",
				slog.String("version", version),
				slog.String("cidr", ipNet.String()))
		}

		if err := scanner.Err(); err != nil {
			slog.Debug("Error reading IP ranges",
				slog.String("version", version),
				slog.Any("error", err))
			loadError = err
			return
		}

		cfCdnIPRanges[version] = ipNets
		totalLoaded += len(ipNets)
		slog.Debug("Loaded Cloudflare CDN IP ranges",
			slog.Int("count", len(ipNets)),
			slog.String("version", version))
	}

	slog.Debug("Successfully loaded Cloudflare CDN IP ranges",
		slog.Int("total_loaded", totalLoaded))
}

func GetCfCdnIPRanges() map[string][]*net.IPNet {
	loadOnce.Do(loadCfCdnIPRanges)

	if loadError != nil {
		slog.Debug("Error loading CDN IP ranges", slog.Any("error", loadError))
		return nil
	}

	if cfCdnIPRanges == nil || (len(cfCdnIPRanges["ipv4"]) == 0 && len(cfCdnIPRanges["ipv6"]) == 0) {
		slog.Debug("Warning: No CDN IP ranges loaded")
		return nil
	}

	return cfCdnIPRanges
}
