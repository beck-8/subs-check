//go:build linux

package utils

import (
	"log/slog"
	"syscall"

	"github.com/beck-8/subs-check/config"
	"github.com/metacubex/mihomo/component/dialer"
)

func SetDSCP() {
	dscp := config.GlobalConfig.DSCP
	if dscp <= 0 || dscp > 63 {
		return
	}
	tos := dscp << 2
	slog.Info("设置 DSCP 标记", "dscp", dscp, "tos", tos)

	dialer.DefaultSocketHook = func(network, address string, conn syscall.RawConn) error {
		return conn.Control(func(fd uintptr) {
			// IPv4
			syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IP, syscall.IP_TOS, tos)
			// IPv6
			syscall.SetsockoptInt(int(fd), syscall.IPPROTO_IPV6, syscall.IPV6_TCLASS, tos)
		})
	}
}
