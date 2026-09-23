package restconf

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/store"
)

// RFC 8527 §3.1 addresses a datastore with a namespace-qualified identityref
// carried in the path; the ?datastore= alias keeps answering on
// /restconf/data for existing clients.
func TestDatastoreIdentityAddressing(t *testing.T) {
	server := newTestServer(t, Options{})

	alias := do(t, server, http.MethodGet,
		"/restconf/data/sim-device:system-info?datastore=candidate")
	require.Equal(t, http.StatusOK, alias.Code)

	standard := do(t, server, http.MethodGet,
		"/restconf/ds/ietf-datastores:candidate/sim-device:system-info")
	require.Equal(t, http.StatusOK, standard.Code)
	assert.JSONEq(t, alias.Body.String(), standard.Body.String())

	// The datastore root itself is readable too.
	response := do(t, server, http.MethodGet, "/restconf/ds/ietf-datastores:running")
	assert.Equal(t, http.StatusOK, response.Code)
}

// An edit under /restconf/ds/ writes the datastore the path names and nothing
// else, so standard clients can finally reach candidate and startup.
func TestDatastoreIdentityEditTargetsTheNamedDatastore(t *testing.T) {
	ctx := t.Context()
	r, st := seedRouter(t)
	server := New(r, st, nil, Options{})

	response := call(t, server, http.MethodPatch,
		"/restconf/ds/ietf-datastores:candidate/sim-device:system-info/name",
		MediaTypeJSON, `{"sim-device:name":"RENAMED"}`)
	require.Equal(t, http.StatusNoContent, response.Code)

	value, err := st.Get(ctx, store.Candidate, "system-info/name")
	require.NoError(t, err)
	assert.Equal(t, "RENAMED", value)

	value, err = st.Get(ctx, store.Running, "system-info/name")
	require.NoError(t, err)
	assert.Equal(t, "triadsim-01", value, "a candidate edit must leave running untouched")
}

func TestDatastoreIdentityRejectsUnknownAndAmbiguous(t *testing.T) {
	server := newTestServer(t, Options{})

	tests := []struct {
		name   string
		method string
		target string
		status int
	}{
		{
			name:   "unknown identityref",
			method: http.MethodGet,
			target: "/restconf/ds/ietf-datastores:operational/sim-device:system-info",
			status: http.StatusBadRequest,
		},
		{
			name:   "plain name is not an identityref",
			method: http.MethodGet,
			target: "/restconf/ds/candidate/sim-device:system-info",
			status: http.StatusBadRequest,
		},
		{
			name:   "both selectors at once",
			method: http.MethodGet,
			target: "/restconf/ds/ietf-datastores:candidate/sim-device:system-info?datastore=candidate",
			status: http.StatusBadRequest,
		},
		{
			name:   "startup is read-only",
			method: http.MethodPatch,
			target: "/restconf/ds/sim-device:startup/sim-device:system-info/name",
			status: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			response := call(t, server, tt.method, tt.target,
				MediaTypeJSON, `{"sim-device:name":"X"}`)
			assert.Equal(t, tt.status, response.Code)
		})
	}
}

// A created resource's Location is a valid URI whatever the list key holds:
// RFC 8040 §3.5.3 requires every reserved character of a key value to be
// percent-encoded (a raw one would corrupt the URI — a query, a fragment, an
// ambiguous separator), and the request parser decodes the Location back.
func TestLocationEncodesListKeys(t *testing.T) {
	server := newTestServer(t, Options{})
	collection := "/restconf/data/sim-l2-switching:lldp/neighbors/neighbor"
	prefix := "/restconf/data/sim-l2-switching:lldp/neighbors/neighbor="

	keys := []string{
		"a?b=c",
		"x#y",
		"p%q",
		"m n",
		"c,d",
		"e=f",
		"i:j;k+@l",
		// A "/" inside a key is not representable in the canonical router
		// path grammar (a/b[c=d] splits segments on "/"), which predates and
		// bounds this finding; the URI layer itself encodes it correctly and
		// TestKeyEncodingRoundTrips covers it.
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			body, err := json.Marshal(map[string]any{
				"sim-l2-switching:neighbor": []map[string]any{{
					"port": key, "chassis-id": "c1", "port-id": "p1", "ttl": 120,
				}},
			})
			require.NoError(t, err)

			response := call(t, server, http.MethodPost, collection, MediaTypeJSON, string(body))
			require.Equal(t, http.StatusCreated, response.Code, "body: %s", body)

			location := response.Header().Get("Location")
			require.True(t, strings.HasPrefix(location, prefix), "unexpected Location %q", location)
			escaped := strings.TrimPrefix(location, prefix)
			for _, reserved := range []string{"?", "#", " ", ",", "=", "+", "@", ":", ";", "/"} {
				assert.NotContains(t, escaped, reserved,
					"the key %q must not reach the URI unencoded as %q", key, escaped)
			}

			// The client follows the Location verbatim and reaches the entry.
			fetched := do(t, server, http.MethodGet, location)
			require.Equal(t, http.StatusOK, fetched.Code, "GET %s", location)
			_, decoded := getJSON(t, server, location)
			entries := decoded["sim-l2-switching:neighbor"].([]any)
			require.Len(t, entries, 1)
			assert.Equal(t, key, entries[0].(map[string]any)["port"])
		})
	}
}

// The encoding is round-trip safe by construction: escaping a key and parsing
// the segment back yields the original value for every reserved character.
func TestKeyEncodingRoundTrips(t *testing.T) {
	keys := []string{
		"plain", "a?b", "x#y", "p%q", "m n", "c,d", "e=f", "g/h", "i:j;k+@l",
		"02:00:00:00:00:aa", "100%", "a+b", "u@v", "-._~",
	}
	for _, key := range keys {
		t.Run(key, func(t *testing.T) {
			module, name, value, httpErr := splitSegment("neighbor=" + escapeKey(key))

			require.Nil(t, httpErr)
			assert.Empty(t, module)
			assert.Equal(t, "neighbor", name)
			assert.Equal(t, key, value)
		})
	}
}

// locationFor builds an identifier the request parser resolves back to the
// same canonical path.
func TestLocationForParsesBack(t *testing.T) {
	const key = "a?b=c#d e,f%g:h;i+@k"
	location := locationFor(BasePath+"/data", "lldp/neighbors/neighbor[port="+key+"]")

	server := newTestServer(t, Options{})
	target, httpErr := server.parseTarget(httptest.NewRequest(http.MethodGet, location, nil))

	require.Nil(t, httpErr)
	assert.Equal(t, "lldp/neighbors/neighbor[port="+key+"]", target.Path)
}
