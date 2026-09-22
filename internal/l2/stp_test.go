package l2

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// classicSTP switches the test device to IEEE 802.1D STP with an edge port on
// eth1, so the classic five-state vocabulary is exercised.
func classicSTP(t *testing.T, r *router.Router) {
	t.Helper()
	ctx := t.Context()

	_, err := r.Set(ctx, store.Running, stpPath("protocol"), model.STPProtocolSTP)
	require.NoError(t, err)
	_, err = r.Set(ctx, store.Running, stpPortPath("eth0", "state"), model.STPPortStateForwarding)
	require.NoError(t, err)
	_, err = r.Set(ctx, store.Running, stpPortPath("eth1", "state"), model.STPPortStateBlocking)
	require.NoError(t, err)
	_, err = r.Set(ctx, store.Running, stpPortPath("eth1", "edge-port"), true)
	require.NoError(t, err)
}

// stpPort returns one bridge port of the seeded STP state.
func stpPort(t *testing.T, manager *Manager, port string) model.STPPort {
	t.Helper()

	state, err := manager.STPState(t.Context())
	require.NoError(t, err)
	found, ok := findSTPPort(state.Ports, port)
	require.True(t, ok, "port %s must exist", port)
	return found
}

func TestSTPEventWithoutPort(t *testing.T) {
	manager, _, _ := newTestManager(t)

	err := manager.Handle(t.Context(), STPEvent{Kind: EventLinkDown})
	assert.ErrorContains(t, err, "without a port")
}

func TestSTPUnknownEventAndPort(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	err := manager.Handle(ctx, STPEvent{Kind: "hello", Port: "eth0"})
	assert.ErrorContains(t, err, "unknown stp event")

	err = manager.Handle(ctx, STPEvent{Kind: EventLinkDown, Port: "eth9"})
	assert.ErrorIs(t, err, ErrPortUnknown)
}

func TestSTPDisabledRejectsEvents(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	_, err := r.Set(ctx, store.Running, stpPath("enabled"), false)
	require.NoError(t, err)

	err = manager.Handle(ctx, STPEvent{Kind: EventLinkUp, Port: "eth0"})
	assert.ErrorIs(t, err, ErrSTPDisabled)
}

// The transitions of the simplified machine, driven one event at a time.
func TestSTPTransitions(t *testing.T) {
	tests := []struct {
		name      string
		classic   bool
		event     STPEvent
		wantRole  string
		wantState string
	}{
		{
			name:      "rstp link down discards",
			event:     STPEvent{Kind: EventLinkDown, Port: "eth0"},
			wantRole:  model.STPPortRoleAlternate,
			wantState: model.STPPortStateDiscarding,
		},
		{
			name:      "rstp link up blocks first",
			event:     STPEvent{Kind: EventLinkUp, Port: "eth0"},
			wantRole:  model.STPPortRoleDesignated,
			wantState: model.STPPortStateDiscarding,
		},
		{
			name:      "classic link up blocks",
			classic:   true,
			event:     STPEvent{Kind: EventLinkUp, Port: "eth0"},
			wantRole:  model.STPPortRoleDesignated,
			wantState: model.STPPortStateBlocking,
		},
		{
			name:      "classic link up on an edge port forwards",
			classic:   true,
			event:     STPEvent{Kind: EventLinkUp, Port: "eth1"},
			wantRole:  model.STPPortRoleDesignated,
			wantState: model.STPPortStateForwarding,
		},
		{
			name:      "classic link down disables",
			classic:   true,
			event:     STPEvent{Kind: EventLinkDown, Port: "eth0"},
			wantRole:  model.STPPortRoleAlternate,
			wantState: model.STPPortStateDisabled,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, r, _ := newTestManager(t)
			if test.classic {
				classicSTP(t, r)
			}

			require.NoError(t, manager.Handle(t.Context(), test.event))

			port := stpPort(t, manager, test.event.Port)
			assert.Equal(t, test.wantRole, port.Role)
			assert.Equal(t, test.wantState, port.State)
		})
	}
}

func TestSTPForwardDelayWalk(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()
	classicSTP(t, r)

	_, err := r.Set(ctx, store.Running, stpPortPath("eth0", "edge-port"), false)
	require.NoError(t, err)
	require.NoError(t, manager.Handle(ctx, STPEvent{Kind: EventLinkUp, Port: "eth0"}))
	assert.Equal(t, model.STPPortStateBlocking, stpPort(t, manager, "eth0").State)

	fake := manager.Clock().(*clock.FakeClock)

	// Nothing happens before the forward delay elapses.
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, model.STPPortStateBlocking, stpPort(t, manager, "eth0").State)

	fake.Advance(manager.forwardDelay)
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, model.STPPortStateLearning, stpPort(t, manager, "eth0").State)

	fake.Advance(manager.forwardDelay)
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, model.STPPortStateForwarding, stpPort(t, manager, "eth0").State)

	// A forwarding port has no pending change left.
	fake.Advance(10 * manager.forwardDelay)
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, model.STPPortStateForwarding, stpPort(t, manager, "eth0").State)
}

func TestSTPRootElection(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	// The seeded bridge is its own root: 02:00:00:00:00:01, priority 32768.
	state, err := manager.STPState(ctx)
	require.NoError(t, err)
	assert.Equal(t, state.BridgeAddress, state.RootID)
	assert.Zero(t, state.RootCost)

	// A superior bridge identifier turns eth0 into the root port.
	err = manager.Handle(ctx, STPEvent{
		Kind: EventBPDU, Port: "eth0",
		RootID: "02:00:00:00:00:aa", RootPriority: 4096, PathCost: 1000,
	})
	require.NoError(t, err)

	state, err = manager.STPState(ctx)
	require.NoError(t, err)
	assert.Equal(t, "02:00:00:00:00:aa", state.RootID)
	assert.Equal(t, uint32(21000), state.RootCost, "the announcing cost plus the port cost")

	eth0 := stpPort(t, manager, "eth0")
	assert.Equal(t, model.STPPortRoleRoot, eth0.Role)
	assert.Equal(t, model.STPPortStateForwarding, eth0.State, "RSTP root ports forward at once")

	eth1 := stpPort(t, manager, "eth1")
	assert.Equal(t, model.STPPortRoleAlternate, eth1.Role)
	assert.Equal(t, model.STPPortStateDiscarding, eth1.State)

	// An inferior BPDU keeps this bridge as the root.
	err = manager.Handle(ctx, STPEvent{
		Kind: EventBPDU, Port: "eth1",
		RootID: "02:00:00:00:00:bb", RootPriority: 61440, PathCost: 20000,
	})
	require.NoError(t, err)

	state, err = manager.STPState(ctx)
	require.NoError(t, err)
	assert.Equal(t, state.BridgeAddress, state.RootID)
	assert.Zero(t, state.RootCost)

	eth1 = stpPort(t, manager, "eth1")
	assert.Equal(t, model.STPPortRoleDesignated, eth1.Role)
	assert.Equal(t, model.STPPortStateForwarding, eth1.State)

	// The root election is published as state, so it survives a restart of
	// the machine with the same datastore.
	stored, err := r.Get(ctx, store.Running, stpPath("root-id"))
	require.NoError(t, err)
	assert.Equal(t, state.RootID, stored.Value)
}

func TestSTPRootElectionUsesPriorityFirst(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	// A lower priority wins even with a numerically higher address.
	err := manager.Handle(ctx, STPEvent{
		Kind: EventBPDU, Port: "eth0",
		RootID: "ff:ff:ff:ff:ff:ff", RootPriority: 1, PathCost: 0,
	})
	require.NoError(t, err)

	state, err := manager.STPState(ctx)
	require.NoError(t, err)
	assert.Equal(t, "ff:ff:ff:ff:ff:ff", state.RootID)
	assert.Equal(t, model.STPPortRoleRoot, stpPort(t, manager, "eth0").Role)

	// The same priority with a worse address does not beat the current root,
	// whose address is lower.
	err = manager.Handle(ctx, STPEvent{
		Kind: EventBPDU, Port: "eth1",
		RootID: "ff:ff:ff:ff:ff:ff", RootPriority: 1, PathCost: 0,
	})
	require.NoError(t, err)
	assert.Equal(t, "ff:ff:ff:ff:ff:ff", state.RootID)
}

func TestSTPPublishesTransitions(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()
	classicSTP(t, r)

	bus := event.New(8, manager.Clock())
	manager.bus = bus
	events := bus.Subscribe()
	defer bus.Unsubscribe(events)

	require.NoError(t, manager.Handle(ctx, STPEvent{Kind: EventLinkDown, Port: "eth0"}))

	select {
	case got := <-events:
		assert.Equal(t, event.TypeStateTransition, got.Type)
		assert.Equal(t, "l2/stp/eth0", got.Resource)
		assert.Contains(t, got.Message, "forwarding -> disabled")
		assert.False(t, got.Timestamp.IsZero())
	default:
		t.Fatal("a port transition must be published on the bus")
	}

	// No event when there is nothing to change: eth0 is already disabled.
	require.NoError(t, manager.Handle(ctx, STPEvent{Kind: EventLinkDown, Port: "eth0"}))
	select {
	case got := <-events:
		t.Fatalf("unexpected event %+v", got)
	default:
	}
}

func TestSTPStateAndPortsOrdering(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	state, err := manager.STPState(ctx)
	require.NoError(t, err)
	assert.True(t, state.Enabled)
	assert.Equal(t, model.STPProtocolRSTP, state.Protocol)

	ports, err := manager.STPPorts(ctx)
	require.NoError(t, err)
	require.Len(t, ports, 2)
	assert.Equal(t, []string{"eth0", "eth1"}, []string{ports[0].Port, ports[1].Port})
}

func TestStateNameCollapsesRSTPPhases(t *testing.T) {
	tests := []struct {
		protocol string
		phase    STPPhase
		want     string
	}{
		{model.STPProtocolSTP, STPPhaseDisabled, model.STPPortStateDisabled},
		{model.STPProtocolSTP, STPPhaseBlocking, model.STPPortStateBlocking},
		{model.STPProtocolSTP, STPPhaseListening, model.STPPortStateListening},
		{model.STPProtocolSTP, STPPhaseLearning, model.STPPortStateLearning},
		{model.STPProtocolSTP, STPPhaseForwarding, model.STPPortStateForwarding},
		{model.STPProtocolRSTP, STPPhaseDisabled, model.STPPortStateDiscarding},
		{model.STPProtocolRSTP, STPPhaseBlocking, model.STPPortStateDiscarding},
		{model.STPProtocolRSTP, STPPhaseListening, model.STPPortStateDiscarding},
		{model.STPProtocolRSTP, STPPhaseLearning, model.STPPortStateLearning},
		{model.STPProtocolRSTP, STPPhaseForwarding, model.STPPortStateForwarding},
	}

	for _, test := range tests {
		t.Run(test.protocol+" "+string(test.phase), func(t *testing.T) {
			assert.Equal(t, test.want, stateName(test.protocol, test.phase))
		})
	}
}

func TestSTPForwardDelayDefaults(t *testing.T) {
	manager, err := New(Deps{Router: mustRouter(t)})
	require.NoError(t, err)
	assert.Equal(t, DefaultForwardDelay, manager.forwardDelay)
	assert.Equal(t, DefaultTickInterval, manager.tickInterval)
}

// TestSTPUnblocksUntilForwardDelay checks that the scheduled phase changes obey
// the injected clock rather than the wall clock.
func TestSTPUnblocksUntilForwardDelay(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()
	classicSTP(t, r)

	_, err := r.Set(ctx, store.Running, stpPortPath("eth0", "edge-port"), false)
	require.NoError(t, err)
	require.NoError(t, manager.Handle(ctx, STPEvent{Kind: EventLinkUp, Port: "eth0"}))

	fake := manager.Clock().(*clock.FakeClock)
	fake.Advance(manager.forwardDelay - time.Second)
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, model.STPPortStateBlocking, stpPort(t, manager, "eth0").State,
		"a blocking port must not learn before the forward delay")

	fake.Advance(time.Second)
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, model.STPPortStateLearning, stpPort(t, manager, "eth0").State)

	fake.Advance(manager.forwardDelay - time.Second)
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, model.STPPortStateLearning, stpPort(t, manager, "eth0").State,
		"a learning port must not forward before the second forward delay")

	fake.Advance(time.Second)
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, model.STPPortStateForwarding, stpPort(t, manager, "eth0").State)
}

// mustRouter returns a seeded router for the constructor tests.
func mustRouter(t *testing.T) *router.Router {
	t.Helper()
	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	require.NoError(t, r.Seed(context.Background(), store.Running))
	return r
}
