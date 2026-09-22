package sync

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/event"
)

func TestNextState(t *testing.T) {
	tests := []struct {
		name string
		from State
		typ  EventType
		want State
		ok   bool
	}{
		{name: "detect from freerun", from: StateFreerun, typ: EventSourceDetected, want: StateAcquiring, ok: true},
		{name: "calibrate from acquiring", from: StateAcquiring, typ: EventCalibrationComplete, want: StateLocked, ok: true},
		{name: "lost while acquiring returns to freerun", from: StateAcquiring, typ: EventSourceLost, want: StateFreerun, ok: true},
		{name: "lost while locked enters holdover", from: StateLocked, typ: EventSourceLost, want: StateHoldoverInSpec, ok: true},
		{name: "holdover expires", from: StateHoldoverInSpec, typ: EventHoldoverExpired, want: StateHoldoverOutOfSpec, ok: true},
		{name: "restore from holdover in spec", from: StateHoldoverInSpec, typ: EventSourceRestored, want: StateLocked, ok: true},
		{name: "restore from holdover out of spec", from: StateHoldoverOutOfSpec, typ: EventSourceRestored, want: StateLocked, ok: true},
		{name: "reset from locked", from: StateLocked, typ: EventReset, want: StateFreerun, ok: true},
		{name: "reset from holdover", from: StateHoldoverOutOfSpec, typ: EventReset, want: StateFreerun, ok: true},
		{name: "reset while freerun is a no-op", from: StateFreerun, typ: EventReset, want: StateFreerun, ok: false},
		{name: "detected while locked is a no-op", from: StateLocked, typ: EventSourceDetected, want: StateLocked, ok: false},
		{name: "expiry outside holdover is a no-op", from: StateLocked, typ: EventHoldoverExpired, want: StateLocked, ok: false},
		{name: "restore while locked is a no-op", from: StateLocked, typ: EventSourceRestored, want: StateLocked, ok: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := nextState(tt.from, tt.typ)

			assert.Equal(t, tt.want, got)
			assert.Equal(t, tt.ok, ok)
		})
	}
}

// Handle drives the seeded locked clock through every reachable state and
// records the events it publishes.
func TestHandleDrivesState(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	state, err := f.manager.Handle(ctx, Event{Type: EventSourceLost, Reason: "test"})
	require.NoError(t, err)
	assert.Equal(t, StateHoldoverInSpec, state)
	assert.Equal(t, string(StateHoldoverInSpec), f.state(t))

	state, err = f.manager.Handle(ctx, Event{Type: EventHoldoverExpired})
	require.NoError(t, err)
	assert.Equal(t, StateHoldoverOutOfSpec, state)

	state, err = f.manager.Handle(ctx, Event{Type: EventSourceRestored})
	require.NoError(t, err)
	assert.Equal(t, StateLocked, state)

	state, err = f.manager.Handle(ctx, Event{Type: EventReset})
	require.NoError(t, err)
	assert.Equal(t, StateFreerun, state)

	_, err = f.manager.Handle(ctx, Event{Type: EventSourceDetected})
	require.NoError(t, err)
	state, err = f.manager.Handle(ctx, Event{Type: EventCalibrationComplete})
	require.NoError(t, err)
	assert.Equal(t, StateLocked, state)

	assert.Equal(t, []event.EventType{
		event.TypeStateTransition, // locked -> holdover-in-spec
		event.TypeStateTransition, // holdover-in-spec -> holdover-out-of-spec
		event.TypeAlarmRaised,
		event.TypeStateTransition, // holdover-out-of-spec -> locked
		event.TypeAlarmCleared,
		event.TypeStateTransition, // locked -> freerun
		event.TypeStateTransition, // freerun -> acquiring
		event.TypeStateTransition, // acquiring -> locked
	}, eventTypes(f.drain(t)))
}

func TestHandleRejectsUnknownEvent(t *testing.T) {
	f := newFixture(t)

	_, err := f.manager.Handle(t.Context(), Event{Type: "Bogus"})

	assert.ErrorIs(t, err, ErrUnknownEvent)
	assert.Equal(t, string(StateLocked), f.state(t))
	assert.Empty(t, f.drain(t))
}

func TestHandleUndefinedPairIsNoop(t *testing.T) {
	f := newFixture(t)

	state, err := f.manager.Handle(t.Context(), Event{Type: EventSourceRestored})
	require.NoError(t, err)

	assert.Equal(t, StateLocked, state)
	assert.Empty(t, f.drain(t))
}

func TestSyncLoss(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	// An unknown source is rejected and leaves the clock alone.
	_, err := f.manager.SyncLoss(ctx, "eth9")
	assert.ErrorIs(t, err, ErrUnknownSource)
	assert.Equal(t, string(StateLocked), f.state(t))

	// A known SyncE interface drives the clock to holdover.
	state, err := f.manager.SyncLoss(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, StateHoldoverInSpec, state)
	assert.Equal(t, string(StateHoldoverInSpec), f.state(t))

	// A second loss is a no-op: the clock already holds over.
	state, err = f.manager.SyncLoss(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, StateHoldoverInSpec, state)

	// An unnamed source is accepted too.
	_, err = f.manager.Handle(ctx, Event{Type: EventSourceRestored})
	require.NoError(t, err)
	state, err = f.manager.SyncLoss(ctx, "")
	require.NoError(t, err)
	assert.Equal(t, StateHoldoverInSpec, state)
}

func TestSyncLossWhileFreerunIsNoop(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	_, err := f.manager.Handle(ctx, Event{Type: EventReset})
	require.NoError(t, err)

	state, err := f.manager.SyncLoss(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, StateFreerun, state)
}
