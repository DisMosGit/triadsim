package restconf

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
)

// eventBus returns an EventBus closed when the test finishes.
func eventBus(t *testing.T) *event.Bus {
	t.Helper()
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	t.Cleanup(bus.Close)
	return bus
}

// call runs one request with an optional body through the server handler.
func call(t *testing.T, server *Server, method, target, contentType string, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader([]byte(body)))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

// getJSON reads one resource and decodes the response body.
func getJSON(t *testing.T, server *Server, target string) (int, map[string]any) {
	t.Helper()
	response := call(t, server, http.MethodGet, target, "", "")
	var body map[string]any
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body), "body: %s", response.Body.String())
	return response.Code, body
}

func TestGetSystemInfo(t *testing.T) {
	server := newTestServer(t, Options{})

	status, body := getJSON(t, server, "/restconf/data/sim-device:system-info")
	require.Equal(t, http.StatusOK, status)

	info, ok := body["sim-device:system-info"].(map[string]any)
	require.True(t, ok, "body: %v", body)
	assert.Equal(t, "sim-001", info["device-id"])
	assert.Equal(t, "triadsim-01", info["name"])
}

func TestGetLeafAndState(t *testing.T) {
	server := newTestServer(t, Options{})

	// A leaf is returned as a module-qualified scalar.
	status, body := getJSON(t, server, "/restconf/data/sim-device:system-info/device-id")
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, map[string]any{"sim-device:device-id": "sim-001"}, body)

	// State leaves are returned by default ...
	status, body = getJSON(t, server, "/restconf/data/sim-device:interfaces/interface=radio0/radio-link")
	require.Equal(t, http.StatusOK, status)
	link := body["sim-radio-link:radio-link"].(map[string]any)
	assert.Equal(t, -72.5, link["rssi"])

	// ... and omitted with ?content=config.
	response := call(t, server, http.MethodGet,
		"/restconf/data/sim-device:interfaces/interface=radio0/radio-link?content=config", "", "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.NotContains(t, response.Body.String(), "rssi")
	assert.Contains(t, response.Body.String(), "tx-power")

	// With ?content=config a state leaf is not a resource at all.
	response = call(t, server, http.MethodGet,
		"/restconf/data/sim-device:interfaces/interface=radio0/radio-link/rssi?content=config", "", "")
	assert.Equal(t, http.StatusNotFound, response.Code)
}

func TestGetStpState(t *testing.T) {
	server := newTestServer(t, Options{})

	status, body := getJSON(t, server, "/restconf/data/sim-l2-switching:stp/state")
	require.Equal(t, http.StatusOK, status)

	state := body["sim-l2-switching:state"].(map[string]any)
	assert.Equal(t, true, state["enabled"])
	assert.Equal(t, "rstp", state["protocol"])
	assert.Equal(t, float64(32768), state["bridge-priority"])

	ports := state["ports"].(map[string]any)["port"].([]any)
	require.Len(t, ports, 2)
	first := ports[0].(map[string]any)
	assert.Equal(t, "eth0", first["port"])
	assert.Equal(t, "root", first["role"])
	assert.Equal(t, "forwarding", first["state"])
}

func TestGetMacTable(t *testing.T) {
	server := newTestServer(t, Options{})

	status, body := getJSON(t, server, "/restconf/data/sim-l2-switching:mac-table")
	require.Equal(t, http.StatusOK, status)

	table := body["sim-l2-switching:mac-table"].(map[string]any)
	assert.Equal(t, float64(300), table["aging-time"])
	entries := table["entry"].([]any)
	require.Len(t, entries, 1)
	assert.Equal(t, "02:00:00:00:00:02", entries[0].(map[string]any)["mac-address"])
}

func TestGetVlans(t *testing.T) {
	server := newTestServer(t, Options{})

	status, body := getJSON(t, server, "/restconf/data/sim-l2-switching:vlans")
	require.Equal(t, http.StatusOK, status)

	vlans := body["sim-l2-switching:vlans"].(map[string]any)["vlan"].([]any)
	require.Len(t, vlans, 1)
	vlan := vlans[0].(map[string]any)
	assert.Equal(t, float64(100), vlan["id"])
	assert.Equal(t, "DATA", vlan["name"])

	// A single entry is returned as a one-element list.
	status, body = getJSON(t, server, "/restconf/data/sim-l2-switching:vlans/vlan=100")
	require.Equal(t, http.StatusOK, status)
	entries := body["sim-l2-switching:vlan"].([]any)
	require.Len(t, entries, 1)
}

func TestGetMissingResource(t *testing.T) {
	server := newTestServer(t, Options{})

	response := call(t, server, http.MethodGet, "/restconf/data/sim-l2-switching:vlans/vlan=999", "", "")
	assert.Equal(t, http.StatusNotFound, response.Code)

	var document errorDocument
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &document))
	require.Len(t, document.Errors.Error, 1)
	assert.Equal(t, "data-missing", document.Errors.Error[0].Tag)
}

func TestPutCreatesAndReplacesVlan(t *testing.T) {
	server := newTestServer(t, Options{})
	target := "/restconf/data/sim-l2-switching:vlans/vlan=200"
	body := `{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE"}]}`

	response := call(t, server, http.MethodPut, target, MediaTypeJSON, body)
	require.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "/restconf/data/sim-l2-switching:vlans/vlan=200", response.Header().Get("Location"))

	status, decoded := getJSON(t, server, target)
	require.Equal(t, http.StatusOK, status)
	entry := decoded["sim-l2-switching:vlan"].([]any)[0].(map[string]any)
	assert.Equal(t, "VOICE", entry["name"])

	// Replacing the same resource is a 200.
	response = call(t, server, http.MethodPut, target, MediaTypeJSON,
		`{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE2"}]}`)
	require.Equal(t, http.StatusOK, response.Code)

	status, decoded = getJSON(t, server, target)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "VOICE2", decoded["sim-l2-switching:vlan"].([]any)[0].(map[string]any)["name"])
}

func TestPutRejectsInvalidConfiguration(t *testing.T) {
	server := newTestServer(t, Options{})
	target := "/restconf/data/sim-l2-switching:vlans/vlan=400"

	// A VLAN without a name is rejected by the model.
	response := call(t, server, http.MethodPut, target, MediaTypeJSON,
		`{"sim-l2-switching:vlan":[{"id":400}]}`)
	assert.Equal(t, http.StatusUnprocessableEntity, response.Code)

	// The rejected configuration left the datastore untouched.
	response = call(t, server, http.MethodGet, target, "", "")
	assert.Equal(t, http.StatusNotFound, response.Code)
}

func TestPutRejectsReadOnlyLeaf(t *testing.T) {
	server := newTestServer(t, Options{})

	response := call(t, server, http.MethodPut,
		"/restconf/data/sim-device:interfaces/interface=radio0/radio-link/rssi",
		MediaTypeJSON, `{"sim-device:rssi":-50}`)
	assert.Equal(t, http.StatusForbidden, response.Code)
}

func TestPatchMergesAndDeleteRemoves(t *testing.T) {
	server := newTestServer(t, Options{})
	target := "/restconf/data/sim-l2-switching:vlans/vlan=200"
	require.Equal(t, http.StatusCreated, call(t, server, http.MethodPut, target, MediaTypeJSON,
		`{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE"}]}`).Code)

	// A patch merges, so it can change one leaf.
	response := call(t, server, http.MethodPatch, target, MediaTypeJSON,
		`{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE-NEW"}]}`)
	require.Equal(t, http.StatusNoContent, response.Code)

	_, body := getJSON(t, server, target)
	assert.Equal(t, "VOICE-NEW", body["sim-l2-switching:vlan"].([]any)[0].(map[string]any)["name"])

	// DELETE removes the entry.
	response = call(t, server, http.MethodDelete, target, "", "")
	require.Equal(t, http.StatusNoContent, response.Code)

	response = call(t, server, http.MethodGet, target, "", "")
	assert.Equal(t, http.StatusNotFound, response.Code)
}

func TestPostCreatesListEntry(t *testing.T) {
	server := newTestServer(t, Options{})
	collection := "/restconf/data/sim-l2-switching:vlans/vlan"
	body := `{"sim-l2-switching:vlan":[{"id":300,"name":"TEST"}]}`

	response := call(t, server, http.MethodPost, collection, MediaTypeJSON, body)
	require.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "/restconf/data/sim-l2-switching:vlans/vlan=300", response.Header().Get("Location"))

	// Creating the same entry again is a conflict.
	response = call(t, server, http.MethodPost, collection, MediaTypeJSON, body)
	assert.Equal(t, http.StatusConflict, response.Code)
}

func TestWriteTargetsCandidateDatastore(t *testing.T) {
	server := newTestServer(t, Options{})
	target := "/restconf/data/sim-l2-switching:vlans/vlan=200?datastore=candidate"

	response := call(t, server, http.MethodPut, target, MediaTypeJSON,
		`{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE"}]}`)
	require.Equal(t, http.StatusCreated, response.Code)

	// Running is untouched.
	running := call(t, server, http.MethodGet, "/restconf/data/sim-l2-switching:vlans/vlan=200", "", "")
	assert.Equal(t, http.StatusNotFound, running.Code)

	// Candidate holds it.
	candidate := call(t, server, http.MethodGet, target, "", "")
	assert.Equal(t, http.StatusOK, candidate.Code)
}

func TestWriteRejectsReadOnlyStartup(t *testing.T) {
	server := newTestServer(t, Options{})

	response := call(t, server, http.MethodPut,
		"/restconf/data/sim-device:system-info?datastore=startup", MediaTypeJSON,
		`{"sim-device:system-info":{"name":"x"}}`)
	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

func TestWriteRejectsUnsupportedFormat(t *testing.T) {
	server := newTestServer(t, Options{})

	response := call(t, server, http.MethodPut, "/restconf/data/sim-device:system-info",
		"application/json", `{"sim-device:system-info":{"name":"x"}}`)
	assert.Equal(t, http.StatusUnsupportedMediaType, response.Code)
	assert.Equal(t, MediaTypeJSON, response.Header().Get("Content-Type"))
}

func TestWriteRejectsMalformedBody(t *testing.T) {
	server := newTestServer(t, Options{})

	response := call(t, server, http.MethodPut, "/restconf/data/sim-device:system-info/name",
		MediaTypeJSON, `{not json`)
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestConfigChangedIsPublished(t *testing.T) {
	r, st := seedRouter(t)
	bus := eventBus(t)
	server := New(r, st, bus, Options{})

	channel := bus.Subscribe()
	defer bus.Unsubscribe(channel)

	response := call(t, server, http.MethodPut, "/restconf/data/sim-l2-switching:vlans/vlan=200",
		MediaTypeJSON, `{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE"}]}`)
	require.Equal(t, http.StatusCreated, response.Code)

	select {
	case event := <-channel:
		assert.Equal(t, "ConfigChanged", string(event.Type))
		assert.Contains(t, event.Message, "restconf PUT")
	default:
		t.Fatal("a successful write must publish ConfigChanged")
	}
}

func TestDeleteListCollectionRemovesEntries(t *testing.T) {
	server := newTestServer(t, Options{})

	response := call(t, server, http.MethodDelete, "/restconf/data/sim-l2-switching:vlans/vlan", "", "")
	require.Equal(t, http.StatusNoContent, response.Code)

	status, body := getJSON(t, server, "/restconf/data/sim-l2-switching:vlans")
	require.Equal(t, http.StatusOK, status)
	vlans := body["sim-l2-switching:vlans"].(map[string]any)
	// RFC 7951: a list with no entries is an empty array.
	assert.Equal(t, []any{}, vlans["vlan"])
}

// A body above the 1 MiB limit is a 413, not a malformed message.
func TestBodyAboveLimitIsTooLarge(t *testing.T) {
	server := newTestServer(t, Options{})
	target := "/restconf/data/sim-l2-switching:vlans/vlan=900/name"
	body := strings.Repeat("x", 1<<20+1)

	response := call(t, server, http.MethodPut, target, MediaTypeJSON, body)
	assert.Equal(t, http.StatusRequestEntityTooLarge, response.Code)
	assert.Contains(t, response.Body.String(), "too-big")
}
