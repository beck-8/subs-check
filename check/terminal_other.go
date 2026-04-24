//go:build !windows

package check

// enableVTMode is a no-op outside Windows; ANSI escapes are supported
// by every unix terminal emulator worth targeting.
func enableVTMode(fd uintptr) bool {
	return true
}
