package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/DisMosGit/triadsim/internal/store"
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

// testDeps binds the management planes to ephemeral loopback ports and keeps
// the startup file inside the test's temporary directory.
func testDeps(t *testing.T) runtimeDeps {
	t.Helper()

	return runtimeDeps{
		snmpAddr:     "127.0.0.1:0",
		netconfAddr:  "127.0.0.1:0",
		restconfAddr: "127.0.0.1:0",
		metricsAddr:  "127.0.0.1:0",
		gnmiAddr:     "127.0.0.1:0",
		startupFile:  filepath.Join(t.TempDir(), "startup.json"),
	}
}

// startRun runs the start command in the background and returns a stop
// function plus the done channel.
func startRun(t *testing.T, ctx context.Context, configPath string, out *lockedBuffer, deps runtimeDeps) (<-chan error, context.CancelFunc) {
	t.Helper()

	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- run(runCtx, configPath, out, deps) }()
	return done, cancel
}

// waitForRecord polls out until a JSON record with the given msg appears.
func waitForRecord(t *testing.T, out *lockedBuffer, msg string) map[string]any {
	t.Helper()

	var found map[string]any
	require.Eventually(t, func() bool {
		for _, record := range decodeRecords(t, out.String()) {
			if record["msg"] == msg {
				found = record
				return true
			}
		}
		return false
	}, 10*time.Second, 5*time.Millisecond, "log record %q must appear", msg)

	return found
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

func TestRunLogsStartAndStop(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: debug\n")

	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, testDeps(t))

	start := waitForRecord(t, out, "simulator starting")
	assert.Equal(t, "INFO", start["level"])
	assert.Equal(t, float64(1161), start["snmp_port"])
	assert.Equal(t, float64(1162), start["snmp_trap_port"])
	assert.Equal(t, float64(1830), start["netconf_port"])
	assert.Equal(t, float64(8080), start["restconf_port"])
	assert.Equal(t, float64(9090), start["metrics_port"])
	assert.Equal(t, false, start["gnmi_enabled"])
	assert.Empty(t, start["gnmi_addr"], "a disabled gNMI service must not listen")
	assert.Equal(t, "debug", start["log_level"])
	assert.Equal(t, "sim-001", start["device_id"])
	assert.NotEmpty(t, start["netconf_addr"])
	assert.NotEmpty(t, start["restconf_addr"])

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancellation")
	}

	stop := waitForRecord(t, out, "simulator stopped")
	assert.Equal(t, context.Canceled.Error(), stop["reason"])
}

func TestRunBlocksUntilCancelled(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: info\n")

	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, testDeps(t))
	waitForRecord(t, out, "simulator starting")

	select {
	case err := <-done:
		t.Fatalf("run returned before cancellation: %v", err)
	default:
	}

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return after cancellation")
	}
	assert.Contains(t, out.String(), "simulator stopped")
}

func TestRunSeedsStartupOnFirstBoot(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: info\n")

	deps := testDeps(t)
	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, deps)
	waitForRecord(t, out, "simulator starting")
	cancel()
	require.NoError(t, <-done)

	values, err := store.Load(context.Background(), deps.startupFile)
	require.NoError(t, err)
	assert.NotEmpty(t, values)
	assert.Equal(t, 20.0, values["interfaces/interface[name=radio0]/radio-link/tx-power"])
}

func TestRunServesMetrics(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: info\n")

	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, testDeps(t))
	defer func() {
		cancel()
		<-done
	}()

	start := waitForRecord(t, out, "simulator starting")
	address, ok := start["metrics_addr"].(string)
	require.True(t, ok)

	response, err := http.Get("http://" + address + "/metrics")
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	assert.Equal(t, http.StatusOK, response.StatusCode)

	body := new(bytes.Buffer)
	_, err = body.ReadFrom(response.Body)
	require.NoError(t, err)
	assert.Contains(t, body.String(), "simulator_uptime_seconds")
	assert.Contains(t, body.String(), "simulator_snmp_requests_total")
}

func TestRunServesRestconf(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: info\n")

	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, testDeps(t))
	defer func() {
		cancel()
		<-done
	}()

	start := waitForRecord(t, out, "simulator starting")
	address, ok := start["restconf_addr"].(string)
	require.True(t, ok)
	require.NotEmpty(t, address)

	response, err := http.Get("http://" + address + "/restconf/data/sim-device:system-info")
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	assert.Equal(t, http.StatusOK, response.StatusCode)

	body := new(bytes.Buffer)
	_, err = body.ReadFrom(response.Body)
	require.NoError(t, err)
	assert.Contains(t, body.String(), `"sim-device:system-info"`)
	assert.Contains(t, body.String(), `"device-id":"sim-001"`)
}

func TestRunServesSyncLoss(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: info\n")

	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, testDeps(t))
	defer func() {
		cancel()
		<-done
	}()

	start := waitForRecord(t, out, "simulator starting")
	address, ok := start["restconf_addr"].(string)
	require.True(t, ok)
	require.NotEmpty(t, address)

	state, err := http.Get("http://" + address + "/restconf/data/sim-sync:ptp/clock/state")
	require.NoError(t, err)
	defer func() { _ = state.Body.Close() }()
	require.Equal(t, http.StatusOK, state.StatusCode)
	stateBody := new(bytes.Buffer)
	_, err = stateBody.ReadFrom(state.Body)
	require.NoError(t, err)
	assert.Contains(t, stateBody.String(), `"sim-sync:state":"locked"`)

	response, err := http.Post("http://"+address+"/api/simulate/sync-loss",
		"application/json", strings.NewReader(`{"port":"eth0"}`))
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusAccepted, response.StatusCode)
	lossBody := new(bytes.Buffer)
	_, err = lossBody.ReadFrom(response.Body)
	require.NoError(t, err)
	assert.Contains(t, lossBody.String(), `"state":"holdover-in-spec"`)
}

func TestRunServesNetconfSubsystem(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: info\n")

	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, testDeps(t))
	defer func() {
		cancel()
		<-done
	}()

	start := waitForRecord(t, out, "simulator starting")
	address, ok := start["netconf_addr"].(string)
	require.True(t, ok)
	require.NotEmpty(t, address)

	client, err := ssh.Dial("tcp", address, &ssh.ClientConfig{
		User:            "admin",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         5 * time.Second,
	})
	require.NoError(t, err)
	defer func() { _ = client.Close() }()

	channel, requests, err := client.OpenChannel("session", nil)
	require.NoError(t, err)
	go ssh.DiscardRequests(requests)
	defer func() { _ = channel.Close() }()

	accepted, err := channel.SendRequest("subsystem", true, ssh.Marshal(&struct{ Name string }{Name: "netconf"}))
	require.NoError(t, err)
	require.True(t, accepted, "start must accept the netconf subsystem")

	// The server hello arrives first, with end-of-message framing.
	buffered := bufio.NewReader(channel)
	var message strings.Builder
	for !strings.HasSuffix(message.String(), "]]>]]>") {
		b, err := buffered.ReadByte()
		require.NoError(t, err)
		message.WriteByte(b)
	}

	assert.Contains(t, message.String(), "<hello")
	assert.Contains(t, message.String(), "urn:ietf:params:netconf:base:1.1")
	assert.Contains(t, message.String(), "<session-id>")
}

func TestRunServesGnmiWhenEnabled(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "gnmi:\n  enabled: true\n  port: 19339\n")

	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, testDeps(t))
	defer func() {
		cancel()
		<-done
	}()

	start := waitForRecord(t, out, "simulator starting")
	assert.Equal(t, true, start["gnmi_enabled"])
	address, ok := start["gnmi_addr"].(string)
	require.True(t, ok, "the startup record must carry gnmi_addr")
	require.NotEmpty(t, address)

	conn, err := grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	response, err := gnmi.NewGNMIClient(conn).Capabilities(context.Background(), &gnmi.CapabilityRequest{})
	require.NoError(t, err)
	assert.Len(t, response.GetSupportedModels(), 4)
}

// decodeRecords parses newline-delimited JSON records from data.
func decodeRecords(t *testing.T, data string) []map[string]any {
	t.Helper()

	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(data), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &record), "line must be JSON: %s", line)
		records = append(records, record)
	}
	return records
}
