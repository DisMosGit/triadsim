package restconf

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// callXML runs one request with explicit Accept and Content-Type headers.
func callXML(t *testing.T, server *Server, method, target, accept, contentType, body string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, bytes.NewReader([]byte(body)))
	if accept != "" {
		request.Header.Set("Accept", accept)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	return recorder
}

func TestGetXML(t *testing.T) {
	server := newTestServer(t, Options{})

	response := callXML(t, server, http.MethodGet, "/restconf/data/sim-device:system-info", MediaTypeXML, "", "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Equal(t, MediaTypeXML, response.Header().Get("Content-Type"))
	assert.Contains(t, response.Body.String(), `<system-info xmlns="urn:sim:device">`)
	assert.Contains(t, response.Body.String(), "<device-id>sim-001</device-id>")

	// A nested module declares its own namespace.
	response = callXML(t, server, http.MethodGet,
		"/restconf/data/sim-device:interfaces/interface=radio0/radio-link", MediaTypeXML, "", "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `<radio-link xmlns="urn:sim:radio-link">`)

	// The whole datastore is wrapped in <data>.
	response = callXML(t, server, http.MethodGet, "/restconf/data", MediaTypeXML, "", "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `<data xmlns="urn:ietf:params:xml:ns:yang:ietf-restconf">`)
}

func TestGetRejectsUnknownAccept(t *testing.T) {
	server := newTestServer(t, Options{})

	response := callXML(t, server, http.MethodGet, "/restconf/data/sim-device:system-info", "text/plain", "", "")
	assert.Equal(t, http.StatusNotAcceptable, response.Code)
}

func TestPutWithXMLBody(t *testing.T) {
	server := newTestServer(t, Options{})
	target := "/restconf/data/sim-l2-switching:vlans/vlan=300"
	body := `<vlan xmlns="urn:sim:l2-switching"><id>300</id><name>TEST</name></vlan>`

	response := callXML(t, server, http.MethodPut, target, MediaTypeXML, MediaTypeXML, body)
	require.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "/restconf/data/sim-l2-switching:vlans/vlan=300", response.Header().Get("Location"))

	status, decoded := getJSON(t, server, target)
	require.Equal(t, http.StatusOK, status)
	entry := decoded["sim-l2-switching:vlan"].([]any)[0].(map[string]any)
	assert.Equal(t, "TEST", entry["name"])
}

func TestPutWithMalformedXMLBody(t *testing.T) {
	server := newTestServer(t, Options{})

	response := callXML(t, server, http.MethodPut, "/restconf/data/sim-device:system-info/name",
		MediaTypeXML, MediaTypeXML, "<name>")
	assert.Equal(t, http.StatusBadRequest, response.Code)
}

func TestPostXMLListEntry(t *testing.T) {
	server := newTestServer(t, Options{})
	target := "/restconf/data/sim-l2-switching:vlans/vlan"
	body := `<vlan xmlns="urn:sim:l2-switching"><id>300</id><name>TEST</name></vlan>`

	response := callXML(t, server, http.MethodPost, target, MediaTypeXML, MediaTypeXML, body)
	require.Equal(t, http.StatusCreated, response.Code)
	assert.Equal(t, "/restconf/data/sim-l2-switching:vlans/vlan=300", response.Header().Get("Location"))

	// The same entry twice is a conflict.
	response = callXML(t, server, http.MethodPost, target, MediaTypeXML, MediaTypeXML, body)
	assert.Equal(t, http.StatusConflict, response.Code)
}
