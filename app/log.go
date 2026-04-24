package app

import (
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// FileLogger writes to the shared temp log (e.g. $TMP/subs-check.log).
// Both slog's file handler and gin's DefaultWriter write here so progress
// rendering on stdout isn't cluttered by HTTP access logs when the admin
// page is open, while everything is still recorded for debugging.
var FileLogger = &lumberjack.Logger{
	Filename:   filepath.Join(os.TempDir(), "subs-check.log"),
	MaxSize:    10,
	MaxBackups: 3,
	MaxAge:     7,
}

// TempLog returns the path to the shared temp log file.
func TempLog() string {
	return FileLogger.Filename
}
