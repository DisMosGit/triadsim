package restconf

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/l2"
)

// The storm endpoint drives the L2 domain: the injected frames move the
// interface counters of the ingress port.
func TestStormEndpointInjectsFrames(t *testing.T) {
	r, st := seedRouter(t)
	manager, err := l2.New(l2.Deps{Router: r})
	require.NoError(t, err)
	server := New(r, st, nil, Options{Storm: manager})

	response := call(t, server, http.MethodPost, "/api/simulate/l2-storm", MediaTypeJSON,
		`{"port":"eth0","packets":4}`)
	require.Equal(t, http.StatusAccepted, response.Code)
	assert.Contains(t, response.Body.String(), `"packets":4`)

	counters, err := manager.Counters(t.Context(), "eth0")
	require.NoError(t, err)
	assert.Equal(t, uint64(4), counters.InUcastPkts)
}

func TestStormEndpointRejectsBadRequests(t *testing.T) {
	r, st := seedRouter(t)
	manager, err := l2.New(l2.Deps{Router: r})
	require.NoError(t, err)
	server := New(r, st, nil, Options{Storm: manager})

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "malformed body", body: `{`, status: http.StatusBadRequest},
		{name: "missing port", body: `{"packets":1}`, status: http.StatusBadRequest},
		{name: "unknown port", body: `{"port":"eth9","packets":1}`, status: http.StatusUnprocessableEntity},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := call(t, server, http.MethodPost, "/api/simulate/l2-storm", MediaTypeJSON, test.body)
			assert.Equal(t, test.status, response.Code)
		})
	}

	// A body above the 1 MiB limit is rejected before it reaches the domain.
	oversized := call(t, server, http.MethodPost, "/api/simulate/l2-storm", MediaTypeJSON,
		`{"port":"`+strings.Repeat("x", 1<<20)+`"}`)
	assert.Equal(t, http.StatusRequestEntityTooLarge, oversized.Code)

	// A wrong method is answered by the handler, not by the domain.
	response := call(t, server, http.MethodPut, "/api/simulate/l2-storm", MediaTypeJSON, `{"port":"eth0"}`)
	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
}
