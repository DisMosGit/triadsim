package restconf

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// request runs one request carrying extra headers through the server handler.
func request(t *testing.T, server *Server, method, target, contentType, body string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, req)
	return recorder
}

// Every retrieval carries the caching metadata of RFC 8040 §3.4.1 and §5.5,
// and both validators move only when configuration data changes — never for a
// state write (§3.4.1.2).
func TestCachingMetadata(t *testing.T) {
	r, st := seedRouter(t)
	server := New(r, st, nil, Options{})
	const resource = "/restconf/data/sim-device:system-info/name"

	response := do(t, server, http.MethodGet, resource)
	require.Equal(t, http.StatusOK, response.Code)
	etag := response.Header().Get("ETag")
	assert.NotEmpty(t, etag, "the datastore resource must carry an ETag")
	assert.NotEmpty(t, response.Header().Get("Last-Modified"))
	assert.Equal(t, "no-cache", response.Header().Get("Cache-Control"))

	// A state write must not move the validators.
	_, err := r.SetState(t.Context(), "system-info/uptime", uint32(42))
	require.NoError(t, err)
	response = do(t, server, http.MethodGet, resource)
	assert.Equal(t, etag, response.Header().Get("ETag"), "state data must not change the entity-tag")

	// A configuration write must.
	response = call(t, server, http.MethodPatch, resource, MediaTypeJSON, `{"sim-device:name":"RENAMED"}`)
	require.Equal(t, http.StatusNoContent, response.Code)
	response = do(t, server, http.MethodGet, resource)
	assert.NotEqual(t, etag, response.Header().Get("ETag"))

	// The two representations carry different entity-tags (§3.4.1.2).
	xml := request(t, server, http.MethodGet, resource, "", "", map[string]string{"Accept": MediaTypeXML})
	require.Equal(t, http.StatusOK, xml.Code)
	assert.NotEqual(t, response.Header().Get("ETag"), xml.Header().Get("ETag"))
}

// Retrievals honor If-None-Match and If-Modified-Since with 304 (RFC 7232,
// recommended by RFC 8040 §5.5).
func TestConditionalGet(t *testing.T) {
	server := newTestServer(t, Options{})
	const resource = "/restconf/data/sim-device:system-info/name"

	response := do(t, server, http.MethodGet, resource)
	require.Equal(t, http.StatusOK, response.Code)
	etag := response.Header().Get("ETag")

	matching := request(t, server, http.MethodGet, resource, "", "",
		map[string]string{"If-None-Match": etag})
	assert.Equal(t, http.StatusNotModified, matching.Code)
	assert.Empty(t, matching.Body.Bytes())

	stale := request(t, server, http.MethodGet, resource, "", "",
		map[string]string{"If-None-Match": `"not-the-tag"`})
	assert.Equal(t, http.StatusOK, stale.Code)

	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	unmodified := request(t, server, http.MethodGet, resource, "", "",
		map[string]string{"If-Modified-Since": future})
	assert.Equal(t, http.StatusNotModified, unmodified.Code)
}

// Edits honor If-Match and If-Unmodified-Since: a stale copy is refused with
// 412 and changes nothing (RFC 8040 §3.4.1 edit collision prevention).
func TestConditionalEdit(t *testing.T) {
	server := newTestServer(t, Options{})
	const resource = "/restconf/data/sim-device:system-info/name"

	response := do(t, server, http.MethodGet, resource)
	require.Equal(t, http.StatusOK, response.Code)
	stale := response.Header().Get("ETag")

	// Move the datastore under the client's feet.
	require.Equal(t, http.StatusNoContent,
		call(t, server, http.MethodPatch, resource, MediaTypeJSON, `{"sim-device:name":"RENAMED"}`).Code)

	refused := request(t, server, http.MethodPatch, resource, MediaTypeJSON,
		`{"sim-device:name":"LATE"}`, map[string]string{"If-Match": stale})
	assert.Equal(t, http.StatusPreconditionFailed, refused.Code)
	assert.NotEmpty(t, refused.Header().Get("ETag"), "the 412 carries the current validators")

	status, body := getJSON(t, server, resource)
	require.Equal(t, http.StatusOK, status)
	assert.Equal(t, "RENAMED", body["sim-device:name"], "the refused edit changed nothing")

	// The current entity-tag succeeds.
	response = do(t, server, http.MethodGet, resource)
	fresh := response.Header().Get("ETag")
	accepted := request(t, server, http.MethodPatch, resource, MediaTypeJSON,
		`{"sim-device:name":"CURRENT"}`, map[string]string{"If-Match": fresh})
	assert.Equal(t, http.StatusNoContent, accepted.Code)

	// If-Unmodified-Since in the past is stale as well; the second granularity
	// of HTTP dates needs a visibly old timestamp (RFC 7232 §2.2).
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	refused = request(t, server, http.MethodPatch, resource, MediaTypeJSON,
		`{"sim-device:name":"TOO-LATE"}`, map[string]string{"If-Unmodified-Since": past})
	assert.Equal(t, http.StatusPreconditionFailed, refused.Code)

	future := time.Now().Add(time.Hour).UTC().Format(http.TimeFormat)
	accepted = request(t, server, http.MethodPatch, resource, MediaTypeJSON,
		`{"sim-device:name":"IN-TIME"}`, map[string]string{"If-Unmodified-Since": future})
	assert.Equal(t, http.StatusNoContent, accepted.Code)
}
