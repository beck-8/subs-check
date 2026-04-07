//go:build !linux

package utils

func SetDSCP() {
	// DSCP 标记仅在 Linux 上生效
}
