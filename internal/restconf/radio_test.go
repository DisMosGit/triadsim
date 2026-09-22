package restconf

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/radio"
	"github.com/DisMosGit/triadsim/internal/store"
)

// linkStatePath is the link-state leaf of the seeded radio link.
const linkStatePath = "interfaces/interface[name=radio0]/radio-link/link-state"

// The radio endpoints inject a failure and a restore into the radio domain.
func TestRadioFailureEndpointDrivesTheLink(t *testing.T) {
	r, st := seedRouter(t)
	manager, err := radio.New(radio.Deps{Router: r, Clock: clock.NewFakeClock()})
	require.NoError(t, err)
	server := New(r, st, nil, Options{Radio: manager})

	response := call(t, server, http.MethodPost, "/api/simulate/radio-failure", MediaTypeJSON, `{"link":"radio0"}`)
	require.Equal(t, http.StatusAccepted, response.Code)
	assert.Contains(t, response.Body.String(), `"state":"down"`)

	state, err := st.Get(t.Context(), store.Running, linkStatePath)
	require.NoError(t, err)
	assert.Equal(t, "down", state)

	response = call(t, server, http.MethodPost, "/api/simulate/radio-restore", MediaTypeJSON, `{"link":"radio0"}`)
	require.Equal(t, http.StatusAccepted, response.Code)
	assert.Contains(t, response.Body.String(), `"state":"up"`)

	state, err = st.Get(t.Context(), store.Running, linkStatePath)
	require.NoError(t, err)
	assert.Equal(t, "up", state)
}

func TestRadioFailureEndpointRejectsBadRequests(t *testing.T) {
	r, st := seedRouter(t)
	manager, err := radio.New(radio.Deps{Router: r, Clock: clock.NewFakeClock()})
	require.NoError(t, err)
	server := New(r, st, nil, Options{Radio: manager})

	tests := []struct {
		name   string
		target string
		body   string
		status int
	}{
		{name: "malformed body", target: "/api/simulate/radio-failure", body: `{`, status: http.StatusBadRequest},
		{name: "unknown link", target: "/api/simulate/radio-failure", body: `{"link":"radio9"}`, status: http.StatusUnprocessableEntity},
		{name: "negative fade", target: "/api/simulate/radio-failure", body: `{"link":"radio0","fade-db":-5}`, status: http.StatusBadRequest},
		{name: "restore unknown link", target: "/api/simulate/radio-restore", body: `{"link":"radio9"}`, status: http.StatusUnprocessableEntity},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := call(t, server, http.MethodPost, test.target, MediaTypeJSON, test.body)
			assert.Equal(t, test.status, response.Code)
		})
	}

	// A body above the 1 MiB limit is rejected before it reaches the domain.
	oversized := call(t, server, http.MethodPost, "/api/simulate/radio-failure", MediaTypeJSON,
		`{"link":"`+strings.Repeat("x", 1<<20)+`"}`)
	assert.Equal(t, http.StatusRequestEntityTooLarge, oversized.Code)

	// A wrong method is answered by the handler, not by the domain.
	response := call(t, server, http.MethodGet, "/api/simulate/radio-failure", MediaTypeJSON, "")
	assert.Equal(t, http.StatusMethodNotAllowed, response.Code)
}

// Without an injected domain the endpoints report operation-not-supported.
func TestRadioEndpointsWithoutDomain(t *testing.T) {
	r, st := seedRouter(t)
	server := New(r, st, nil, Options{})

	for _, target := range []string{"/api/simulate/radio-failure", "/api/simulate/radio-restore"} {
		response := call(t, server, http.MethodPost, target, MediaTypeJSON, `{"link":"radio0"}`)
		assert.Equal(t, http.StatusNotImplemented, response.Code, target)
	}
}
