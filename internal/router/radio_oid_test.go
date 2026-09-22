package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/store"
)

// The vendor radio-link subtree gains the link-state object, which reports the
// operational state of the link as an integer enumeration.
func TestBindingsExposeRadioLinkState(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	bindings, err := r.Bindings(ctx, store.Running)
	require.NoError(t, err)

	byOID := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		byOID[binding.OID] = binding
	}

	state, ok := byOID[EnterpriseOID+".1.1.8.0"]
	require.True(t, ok, "simRadioLinkState")
	assert.Equal(t, TypeInteger, state.Type)
	assert.Equal(t, "interfaces/interface[name=radio0]/radio-link/link-state", state.Path)
	assert.False(t, state.Writable)
	assert.Equal(t, 1, state.Value, "the seeded link is up")

	// The path and the OID resolve both ways.
	oid, ok := r.OIDForPath("interfaces/interface[name=radio0]/radio-link/link-state")
	require.True(t, ok)
	assert.Equal(t, EnterpriseOID+".1.1.8.0", oid)
}

func TestLinkStateValueEnumeration(t *testing.T) {
	tests := []struct {
		state string
		want  any
	}{
		{state: "up", want: 1},
		{state: "degraded", want: 2},
		{state: "down", want: 3},
		{state: "unknown", want: 1},
	}

	for _, test := range tests {
		t.Run(test.state, func(t *testing.T) {
			assert.Equal(t, test.want, linkStateValue(test.state))
		})
	}
}
