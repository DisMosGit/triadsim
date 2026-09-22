package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// lockedBuffer is a concurrency-safe io.Writer for log capture.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// writeConfig writes content to a temporary config file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
	return path
}

// isolateDefaultLogger restores slog's default logger when the test finishes.
func isolateDefaultLogger(t *testing.T) {
	t.Helper()

	previous := slog.Default()
	t.Cleanup(func() { slog.SetDefault(previous) })
}

func TestRootCmdExposesStart(t *testing.T) {
	cmd := NewRootCmd()

	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	assert.Contains(t, names, "start")
}

func TestStartCmdRejectsArguments(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"start", "unexpected"})

	assert.Error(t, cmd.ExecuteContext(context.Background()))
}

func TestStartCmdRejectsUnknownFlag(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"start", "--nope"})

	assert.Error(t, cmd.ExecuteContext(context.Background()))
}

func TestStartCmdFailsOnMissingConfig(t *testing.T) {
	cmd := NewRootCmd()
	cmd.SetArgs([]string{"start", "--config", filepath.Join(t.TempDir(), "absent.yaml")})

	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "load config")
}

func TestStartCmdFailsOnInvalidConfig(t *testing.T) {
	path := writeConfig(t, "log:\n  level: verbose\n")

	cmd := NewRootCmd()
	cmd.SetArgs([]string{"start", "--config", path})

	err := cmd.ExecuteContext(context.Background())
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown level")
}

func TestRunStartLogsStartAndStopOnCancelledContext(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: debug\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	require.NoError(t, runStart(ctx, path, &out))

	records := decodeRecords(t, &out)
	require.Len(t, records, 2)

	start := records[0]
	assert.Equal(t, "simulator starting", start["msg"])
	assert.Equal(t, "INFO", start["level"])
	assert.Equal(t, float64(1161), start["snmp_port"])
	assert.Equal(t, float64(1162), start["snmp_trap_port"])
	assert.Equal(t, float64(830), start["netconf_port"])
	assert.Equal(t, float64(8080), start["restconf_port"])
	assert.Equal(t, float64(9090), start["metrics_port"])
	assert.Equal(t, false, start["gnmi_enabled"])
	assert.Equal(t, "debug", start["log_level"])
	assert.Equal(t, "startup.json", start["startup_file"])

	stop := records[1]
	assert.Equal(t, "simulator stopped", stop["msg"])
	assert.Equal(t, context.Canceled.Error(), stop["reason"])
}

func TestRunStartBlocksUntilCancelled(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: info\n")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := &lockedBuffer{}
	done := make(chan error, 1)
	go func() { done <- runStart(ctx, path, out) }()

	require.Eventually(t, func() bool {
		return strings.Contains(out.String(), "simulator starting")
	}, 10*time.Second, time.Millisecond, "start must log before blocking")

	select {
	case err := <-done:
		t.Fatalf("runStart returned before cancellation: %v", err)
	default:
	}

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("runStart did not return after cancellation")
	}
	assert.Contains(t, out.String(), "simulator stopped")
}

// decodeRecords parses newline-delimited JSON records from out.
func decodeRecords(t *testing.T, out *bytes.Buffer) []map[string]any {
	t.Helper()

	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var rec map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &rec), "line must be JSON: %s", line)
		records = append(records, rec)
	}
	return records
}
