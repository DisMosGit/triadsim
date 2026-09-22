package restconf

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer returns a Server with the seeded device and the given options.
func newTestServer(t *testing.T, opts Options) *Server {
	t.Helper()
	r, st := seedRouter(t)
	return New(r, st, nil, opts)
}

// do runs one request through the server handler.
func do(t *testing.T, server *Server, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, httptest.NewRequest(method, target, nil))
	return recorder
}

func TestHandlerRoutesUnknownPaths(t *testing.T) {
	server := newTestServer(t, Options{})

	response := do(t, server, http.MethodGet, "/restconf/data/sim-device:nope")
	assert.Equal(t, http.StatusBadRequest, response.Code)
	assert.Contains(t, response.Header().Get("Content-Type"), MediaTypeJSON)

	var document errorDocument
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &document))
	require.Len(t, document.Errors.Error, 1)
	assert.Equal(t, "unknown-element", document.Errors.Error[0].Tag)
}

func TestHandlerRejectsUnsupportedMethods(t *testing.T) {
	server := newTestServer(t, Options{})

	response := do(t, server, http.MethodTrace, "/restconf/data/sim-device:system-info")
	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
	assert.Equal(t, "GET, HEAD, PUT, PATCH, POST, DELETE", response.Header().Get("Allow"))
}

func TestHandlerAnswersNotImplemented(t *testing.T) {
	server := newTestServer(t, Options{})

	tests := []struct {
		name   string
		method string
		target string
	}{
		{name: "operations", method: http.MethodPost, target: "/restconf/operations/sim-l2:clear-mac-table"},
		{name: "streams", method: http.MethodGet, target: "/restconf/streams/sim-events"},
		{name: "storm without simulator", method: http.MethodPost, target: "/api/simulate/l2-storm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := do(t, server, tt.method, tt.target)
			assert.Equal(t, http.StatusNotImplemented, response.Code)

			var document errorDocument
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &document))
			require.Len(t, document.Errors.Error, 1)
			assert.Equal(t, "operation-not-supported", document.Errors.Error[0].Tag)
		})
	}
}

func TestServerListenAndServe(t *testing.T) {
	ctx := t.Context()
	server := newTestServer(t, Options{Addr: "127.0.0.1:0"})
	require.NoError(t, server.Listen())
	require.NotNil(t, server.Addr())

	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx) }()

	response, err := http.Get("http://" + server.Addr().String() + "/restconf/data/sim-device:nope")
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, response.StatusCode)

	require.NoError(t, server.Close())
	require.NoError(t, <-done)
}

func TestServerServeRequiresListen(t *testing.T) {
	server := newTestServer(t, Options{Addr: "127.0.0.1:0"})
	require.Error(t, server.Serve(t.Context()))
}
