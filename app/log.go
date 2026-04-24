package app

import (
	"os"
	"path/filepath"

	"gopkg.in/natefinch/lumberjack.v2"
)

// FileLogger holds subs-check's own slog output. Kept dedicated (no gin
// access logs mixed in) so the web admin /api/logs endpoint can surface
// a clean view of application events.
var FileLogger = &lumberjack.Logger{
	Filename:   filepath.Join(os.TempDir(), "subs-check.log"),
	MaxSize:    10,
	MaxBackups: 3,
	MaxAge:     7,
}

// GinFileLogger catches gin's access log and panic stacks. Kept in the
// same directory as FileLogger for easy grouping but in a separate file
// so HTTP noise doesn't pollute the admin log viewer (and doesn't
// interleave with the CLI progress renderer on stdout).
var GinFileLogger = &lumberjack.Logger{
	Filename:   filepath.Join(os.TempDir(), "subs-check-gin.log"),
	MaxSize:    10,
	MaxBackups: 3,
	MaxAge:     7,
}

// TempLog returns the path to the application log (the one the web UI reads).
func TempLog() string {
	return FileLogger.Filename
}
