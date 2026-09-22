// Package log configures the simulator's structured logger.
//
// Logging goes through log/slog only: records are encoded as JSON on stderr,
// which keeps the simulator's output machine readable when it runs alongside
// other processes. Setup installs the configured logger as slog's default; the
// Debug/Info/Warn/Error helpers are thin, context-aware wrappers around it for
// call sites that do not want to hold a *slog.Logger.
package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
)

// Accepted log levels, matching Config.Log.Level in internal/config.
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// New returns a JSON logger that writes to w and drops records below level.
func New(level string, w io.Writer) (*slog.Logger, error) {
	lvl, err := ParseLevel(level)
	if err != nil {
		return nil, err
	}
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl})
	return slog.New(handler), nil
}

// Setup builds the stderr logger for level and installs it as slog's default.
// It returns an error without touching the default logger when level is
// unknown.
func Setup(level string) (*slog.Logger, error) {
	logger, err := New(level, os.Stderr)
	if err != nil {
		return nil, err
	}
	SetDefault(logger)
	return logger, nil
}

// SetDefault installs logger as the process-wide slog default.
func SetDefault(logger *slog.Logger) {
	slog.SetDefault(logger)
}

// ParseLevel maps a configured level onto slog.Level. Matching is
// case-insensitive and surrounding whitespace is ignored.
func ParseLevel(level string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case LevelDebug:
		return slog.LevelDebug, nil
	case LevelInfo:
		return slog.LevelInfo, nil
	case LevelWarn:
		return slog.LevelWarn, nil
	case LevelError:
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("log: unknown level %q (want %s, %s, %s or %s)",
			level, LevelDebug, LevelInfo, LevelWarn, LevelError)
	}
}

// Debug logs at debug level on the default logger.
func Debug(ctx context.Context, msg string, args ...any) {
	slog.Default().DebugContext(ctx, msg, args...)
}

// Info logs at info level on the default logger.
func Info(ctx context.Context, msg string, args ...any) {
	slog.Default().InfoContext(ctx, msg, args...)
}

// Warn logs at warn level on the default logger.
func Warn(ctx context.Context, msg string, args ...any) {
	slog.Default().WarnContext(ctx, msg, args...)
}

// Error logs at error level on the default logger.
func Error(ctx context.Context, msg string, args ...any) {
	slog.Default().ErrorContext(ctx, msg, args...)
}
