package log

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// decodeJSONLines splits b into decoded JSON records.
func decodeJSONLines(t *testing.T, b *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(b.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "line must be JSON: %s", line)
		records = append(records, rec)
	}
	return records
}

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name    string
		level   string
		want    slog.Level
		wantErr bool
	}{
		{name: "debug", level: "debug", want: slog.LevelDebug},
		{name: "info", level: "info", want: slog.LevelInfo},
		{name: "warn", level: "warn", want: slog.LevelWarn},
		{name: "error", level: "error", want: slog.LevelError},
		{name: "upper case", level: "INFO", want: slog.LevelInfo},
		{name: "mixed case with spaces", level: "  WaRn  ", want: slog.LevelWarn},
		{name: "empty", level: "", wantErr: true},
		{name: "unknown", level: "verbose", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseLevel(tt.level)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewWritesJSONToWriter(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(LevelInfo, &buf)
	require.NoError(t, err)

	logger.InfoContext(context.Background(), "test", "link", "radio0")

	records := decodeJSONLines(t, &buf)
	require.Len(t, records, 1)
	assert.Equal(t, "INFO", records[0]["level"])
	assert.Equal(t, "test", records[0]["msg"])
	assert.Equal(t, "radio0", records[0]["link"])
	assert.Contains(t, records[0], "time")
}

func TestNewRejectsUnknownLevel(t *testing.T) {
	var buf bytes.Buffer
	_, err := New("verbose", &buf)
	require.Error(t, err)
	assert.Empty(t, buf.String())
}

func TestLevelFiltersRecords(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(LevelWarn, &buf)
	require.NoError(t, err)
	ctx := context.Background()

	logger.DebugContext(ctx, "debug")
	logger.InfoContext(ctx, "info")
	logger.WarnContext(ctx, "warn")
	logger.ErrorContext(ctx, "error")

	records := decodeJSONLines(t, &buf)
	require.Len(t, records, 2)
	assert.Equal(t, "warn", records[0]["msg"])
	assert.Equal(t, "error", records[1]["msg"])
}

func TestHelpersLogOnDefaultLogger(t *testing.T) {
	var buf bytes.Buffer
	logger, err := New(LevelDebug, &buf)
	require.NoError(t, err)
	restoreDefaultLogger(t, logger)

	ctx := context.Background()
	Debug(ctx, "debug-entry")
	Info(ctx, "info-entry")
	Warn(ctx, "warn-entry")
	Error(ctx, "error-entry")

	records := decodeJSONLines(t, &buf)
	require.Len(t, records, 4)
	for i, want := range []string{"debug-entry", "info-entry", "warn-entry", "error-entry"} {
		assert.Equal(t, want, records[i]["msg"])
	}
}

func TestSetupWritesJSONToStderr(t *testing.T) {
	r, w, err := os.Pipe()
	require.NoError(t, err)
	oldStderr := os.Stderr
	before := slog.Default()
	os.Stderr = w
	t.Cleanup(func() {
		os.Stderr = oldStderr
		slog.SetDefault(before)
		_ = r.Close()
	})

	_, err = Setup(LevelInfo)
	require.NoError(t, err)

	Info(context.Background(), "test")

	require.NoError(t, w.Close())
	data, err := io.ReadAll(r)
	require.NoError(t, err)

	var rec map[string]any
	require.NoError(t, json.Unmarshal(bytes.TrimSpace(data), &rec))
	assert.Equal(t, "INFO", rec["level"])
	assert.Equal(t, "test", rec["msg"])
}

func TestSetupKeepsDefaultLoggerOnError(t *testing.T) {
	before := slog.Default()
	t.Cleanup(func() { slog.SetDefault(before) })

	_, err := Setup("verbose")
	require.Error(t, err)
	assert.Same(t, before, slog.Default())
}

// restoreDefaultLogger installs logger as the slog default and restores the
// previous default when the test finishes.
func restoreDefaultLogger(t *testing.T, logger *slog.Logger) {
	t.Helper()
	previous := slog.Default()
	slog.SetDefault(logger)
	t.Cleanup(func() { slog.SetDefault(previous) })
}
