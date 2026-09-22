package restconf

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	triadsync "github.com/DisMosGit/triadsim/internal/sync"
)

// The sync endpoint drives the PTP clock of the sync domain.
func TestSyncLossEndpointDrivesClock(t *testing.T) {
	r, st := seedRouter(t)
	manager, err := triadsync.New(triadsync.Deps{Router: r, Clock: clock.NewFakeClock()})
	require.NoError(t, err)
	server := New(r, st, nil, Options{Sync: manager})

	response := call(t, server, http.MethodPost, "/api/simulate/sync-loss", MediaTypeJSON, `{"port":"eth0"}`)
	require.Equal(t, http.StatusAccepted, response.Code)
	assert.Contains(t, response.Body.String(), `"state":"holdover-in-spec"`)

	state, err := manager.Handle(t.Context(), triadsync.Event{Type: triadsync.EventReset})
	require.NoError(t, err)
	assert.Equal(t, triadsync.StateFreerun, state)
}

func TestSyncLossEndpointRejectsBadRequests(t *testing.T) {
	r, st := seedRouter(t)
	manager, err := triadsync.New(triadsync.Deps{Router: r, Clock: clock.NewFakeClock()})
	require.NoError(t, err)
	server := New(r, st, nil, Options{Sync: manager})

	tests := []struct {
		name   string
		body   string
		status int
	}{
		{name: "malformed body", body: `{`, status: http.StatusBadRequest},
		{name: "unknown port", body: `{"port":"eth9"}`, status: http.StatusUnprocessableEntity},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := call(t, server, http.MethodPost, "/api/simulate/sync-loss", MediaTypeJSON, test.body)
			assert.Equal(t, test.status, response.Code)
		})
	}

	// A body above the 1 MiB limit is rejected before it reaches the domain.
	oversized := call(t, server, http.MethodPost, "/api/simulate/sync-loss", MediaTypeJSON,
		`{"port":"`+strings.Repeat("x", 1<<20)+`"}`)
	assert.Equal(t, http.StatusRequestEntityTooLarge, oversized.Code)

	// A wrong method is answered by the handler, not by the domain.
	response := call(t, server, http.MethodPut, "/api/simulate/sync-loss", MediaTypeJSON, `{"port":"eth0"}`)
	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

// Without an injected domain the endpoint reports operation-not-supported.
func TestSyncLossEndpointWithoutDomain(t *testing.T) {
	r, st := seedRouter(t)
	server := New(r, st, nil, Options{})

	response := call(t, server, http.MethodPost, "/api/simulate/sync-loss", MediaTypeJSON, `{"port":"eth0"}`)
	assert.Equal(t, http.StatusNotImplemented, response.Code)
}

// The sync subtree is addressable over RESTCONF: state leaves are readable and
// configuration is writable.
func TestSyncDataEndpoints(t *testing.T) {
	r, st := seedRouter(t)
	server := New(r, st, nil, Options{})

	response := call(t, server, http.MethodGet, "/restconf/data/sim-sync:ptp/clock/state", "", "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), `"sim-sync:state":"locked"`)

	response = call(t, server, http.MethodPatch, "/restconf/data/sim-sync:ptp/clock", MediaTypeJSON,
		`{"sim-sync:clock":{"domain":25}}`)
	require.Equal(t, http.StatusNoContent, response.Code)

	response = call(t, server, http.MethodGet, "/restconf/data/sim-sync:ptp/clock/domain", "", "")
	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), "25")

	// An invalid quality level is rejected by model validation.
	response = call(t, server, http.MethodPatch,
		"/restconf/data/sim-sync:synce/interfaces/interface=eth0",
		MediaTypeJSON, `{"sim-sync:interface":{"ql":"QL-NOPE"}}`)
	assert.Equal(t, http.StatusUnprocessableEntity, response.Code)
}
