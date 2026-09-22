package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordedRequest is one request a test server received.
type recordedRequest struct {
	method string
	path   string
	query  string
	accept string
	body   string
}

// recorder is an HTTP server that records the requests of a command.
type recorder struct {
	mu       sync.Mutex
	requests []recordedRequest
	status   int
	response string
}

func (r *recorder) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	body, err := io.ReadAll(request.Body)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	r.mu.Lock()
	r.requests = append(r.requests, recordedRequest{
		method: request.Method,
		path:   request.URL.Path,
		query:  request.URL.RawQuery,
		accept: request.Header.Get("Accept"),
		body:   string(body),
	})
	r.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(r.status)
	_, _ = io.WriteString(w, r.response)
}

// received returns the requests the server has seen.
func (r *recorder) received() []recordedRequest {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recordedRequest(nil), r.requests...)
}

// startRecorder starts an HTTP server that answers with status and response.
func startRecorder(t *testing.T, status int, response string) (*httptest.Server, *recorder) {
	t.Helper()

	rec := &recorder{status: status, response: response}
	server := httptest.NewServer(rec)
	t.Cleanup(server.Close)
	return server, rec
}

func TestRootCmdExposesEveryCommand(t *testing.T) {
	cmd := NewRootCmd()

	var names []string
	for _, sub := range cmd.Commands() {
		names = append(names, sub.Name())
	}
	for _, want := range []string{"start", "alarm", "dump", "config", "version"} {
		assert.Contains(t, names, want)
	}

	alarm, _, err := cmd.Find([]string{"alarm"})
	require.NoError(t, err)
	require.NotNil(t, alarm)
	var subcommands []string
	for _, sub := range alarm.Commands() {
		subcommands = append(subcommands, sub.Name())
	}
	assert.Contains(t, subcommands, "inject")
}

func TestAlarmInjectDrivesTheSimulationAPI(t *testing.T) {
	tests := []struct {
		name     string
		alarm    string
		fadeDB   float64
		fadeSet  bool
		wantPath string
		wantFade *float64
	}{
		{name: "down uses the default fade", alarm: "radioLinkDown", wantPath: "/api/simulate/radio-failure", wantFade: ptr(0.0)},
		{name: "degraded defaults to a partial fade", alarm: "radioLinkDegraded", wantPath: "/api/simulate/radio-failure", wantFade: ptr(32.0)},
		{name: "restore", alarm: "radioLinkUp", wantPath: "/api/simulate/radio-restore"},
		{name: "an explicit fade wins", alarm: "radioLinkDown", fadeDB: 15, fadeSet: true, wantPath: "/api/simulate/radio-failure", wantFade: ptr(15.0)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server, rec := startRecorder(t, http.StatusAccepted, `{"status":"ok","state":"down"}`)

			out := &bytes.Buffer{}
			require.NoError(t, runAlarmInject(t.Context(), out, server.URL, test.alarm, "radio0", test.fadeDB, test.fadeSet))

			requests := rec.received()
			require.Len(t, requests, 1)
			assert.Equal(t, http.MethodPost, requests[0].method)
			assert.Equal(t, test.wantPath, requests[0].path)
			assert.Contains(t, out.String(), `"state":"down"`)

			var payload map[string]any
			require.NoError(t, json.Unmarshal([]byte(requests[0].body), &payload))
			assert.Equal(t, "radio0", payload["link"])
			if test.wantFade == nil {
				assert.NotContains(t, payload, "fade-db")
			} else {
				assert.Equal(t, *test.wantFade, payload["fade-db"])
			}
		})
	}
}

func TestAlarmInjectCommandFlags(t *testing.T) {
	server, rec := startRecorder(t, http.StatusAccepted, `{"status":"ok"}`)

	cmd := newAlarmInjectCmd(&bytes.Buffer{})
	cmd.SetArgs([]string{"--addr", server.URL, "--type", "radioLinkDegraded", "--link", "radio0", "--fade-db", "25"})
	require.NoError(t, cmd.ExecuteContext(t.Context()))

	requests := rec.received()
	require.Len(t, requests, 1)
	assert.Contains(t, requests[0].body, `"fade-db":25`)
}

func TestAlarmInjectRejectsAnUnknownType(t *testing.T) {
	err := runAlarmInject(t.Context(), &bytes.Buffer{}, DefaultSimulatorAddr, "radioLinkNope", "radio0", 0, false)

	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown alarm type")
	assert.ErrorContains(t, err, "radioLinkDown")
}

func TestAlarmInjectReportsASimulatorError(t *testing.T) {
	server, _ := startRecorder(t, http.StatusUnprocessableEntity, `{"ietf-restconf:errors":{}}`)

	err := runAlarmInject(t.Context(), &bytes.Buffer{}, server.URL, "radioLinkDown", "radio9", 0, false)

	require.Error(t, err)
	assert.ErrorContains(t, err, "inject radioLinkDown")
	assert.ErrorContains(t, err, "422")
}

func TestDumpFetchesTheDatastore(t *testing.T) {
	t.Run("json is indented", func(t *testing.T) {
		server, rec := startRecorder(t, http.StatusOK, `{"sim-device:system-info":{"name":"triadsim-01"}}`)

		out := &bytes.Buffer{}
		require.NoError(t, runDump(t.Context(), out, server.URL, "json", "all"))

		requests := rec.received()
		require.Len(t, requests, 1)
		assert.Equal(t, http.MethodGet, requests[0].method)
		assert.Equal(t, "/restconf/data", requests[0].path)
		assert.Equal(t, "content=all", requests[0].query)
		assert.Equal(t, "application/yang-data+json", requests[0].accept)
		assert.Contains(t, out.String(), "\n  \"sim-device:system-info\"")
	})

	t.Run("xml is passed through", func(t *testing.T) {
		server, rec := startRecorder(t, http.StatusOK, `<data/>`)

		out := &bytes.Buffer{}
		require.NoError(t, runDump(t.Context(), out, server.URL, "xml", "nonconfig"))

		requests := rec.received()
		require.Len(t, requests, 1)
		assert.Equal(t, "content=nonconfig", requests[0].query)
		assert.Equal(t, "application/yang-data+xml", requests[0].accept)
		assert.Equal(t, "<data/>\n", out.String())
	})
}

func TestDumpRejectsUnknownOptions(t *testing.T) {
	err := runDump(t.Context(), &bytes.Buffer{}, DefaultSimulatorAddr, "yaml", "all")
	assert.ErrorContains(t, err, "unknown format")

	err = runDump(t.Context(), &bytes.Buffer{}, DefaultSimulatorAddr, "json", "everything")
	assert.ErrorContains(t, err, "unknown content")
}

func TestDumpReportsAnUnreachableSimulator(t *testing.T) {
	err := runDump(t.Context(), &bytes.Buffer{}, "http://127.0.0.1:1", "json", "all")

	require.Error(t, err)
	assert.ErrorContains(t, err, "dump")
}

func TestConfigValidateAcceptsTheShippedConfig(t *testing.T) {
	out := &bytes.Buffer{}
	cmd := newConfigValidateCmd(out)
	cmd.SetArgs([]string{"--file", filepath.Join("..", "..", "configs", "default.yaml")})

	require.NoError(t, cmd.ExecuteContext(t.Context()))
	assert.Contains(t, out.String(), ": OK")
}

func TestConfigValidateRejectsAnInvalidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("log:\n  level: verbose\n"), 0o600))

	cmd := newConfigValidateCmd(&bytes.Buffer{})
	cmd.SetArgs([]string{"--file", path})

	err := cmd.ExecuteContext(t.Context())
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown level")
}

func TestVersionPrintsTheVersion(t *testing.T) {
	out := &bytes.Buffer{}

	require.NoError(t, newVersionCmd(out).ExecuteContext(context.Background()))

	assert.Equal(t, "triadsim "+Version+"\n", out.String())
	assert.False(t, strings.HasPrefix(Version, "v"), "Version carries no v prefix")
}

// ptr returns a pointer to value, so a table can distinguish an absent field
// from a present one.
func ptr(value float64) *float64 { return &value }
