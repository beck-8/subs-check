//go:build windows

package check

import "golang.org/x/sys/windows"

// enableVTMode turns on virtual-terminal processing so ANSI escape
// sequences (cursor movement, colour) work on Windows cmd / Terminal.
// Harmless no-op when it's already on; returns false on old Windows
// releases (pre-Win10 1511), letting the caller fall back to log mode.
func enableVTMode(fd uintptr) bool {
	h := windows.Handle(fd)
	var mode uint32
	if err := windows.GetConsoleMode(h, &mode); err != nil {
		return false
	}
	if mode&windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING != 0 {
		return true
	}
	if err := windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING); err != nil {
		return false
	}
	return true
}
