package sync

import (
	"context"
	"math"
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

// fixture bundles a manager over a router seeded with DefaultDevice, the store
// behind it, the event bus and its subscription and the fake clock.
type fixture struct {
	manager *Manager
	router  *router.Router
	store   *store.Memory
	bus     *event.Bus
	clock   *clock.FakeClock
	events  <-chan event.Event
}

// newFixture builds a fixture whose manager has not ticked yet.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	require.NoError(t, r.Seed(t.Context(), store.Running))
	require.NoError(t, st.Rollback(t.Context()))

	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	t.Cleanup(bus.Close)

	clk := clock.NewFakeClock()
	manager, err := New(Deps{Router: r, Bus: bus, Clock: clk})
	require.NoError(t, err)
	return &fixture{manager: manager, router: r, store: st, bus: bus, clock: clk, events: bus.Subscribe()}
}

// state reads ptp/clock/state from the running datastore.
func (f *fixture) state(t *testing.T) string {
	t.Helper()

	value, err := f.store.Get(t.Context(), store.Running, "ptp/clock/state")
	require.NoError(t, err)
	text, ok := value.(string)
	require.True(t, ok, "ptp/clock/state is a string")
	return text
}

// leaf reads one leaf from the running datastore.
func (f *fixture) leaf(t *testing.T, path string) any {
	t.Helper()

	value, err := f.store.Get(t.Context(), store.Running, path)
	require.NoError(t, err)
	return value
}

// drain returns every event currently queued on the subscription.
func (f *fixture) drain(t *testing.T) []event.Event {
	t.Helper()

	var events []event.Event
	for {
		select {
		case e := <-f.events:
			events = append(events, e)
		default:
			return events
		}
	}
}

// eventTypes maps events to their types, for compact assertions.
func eventTypes(events []event.Event) []event.EventType {
	types := make([]event.EventType, 0, len(events))
	for _, e := range events {
		types = append(types, e.Type)
	}
	return types
}

// hasEvent reports whether the slice contains the event type.
func hasEvent(events []event.Event, typ event.EventType) bool {
	for _, e := range events {
		if e.Type == typ {
			return true
		}
	}
	return false
}

func TestNewRequiresRouter(t *testing.T) {
	manager, err := New(Deps{})

	assert.Nil(t, manager)
	assert.ErrorIs(t, err, ErrNoRouter)
	assert.ErrorContains(t, err, "router must not be nil")
}

func TestRefreshQualityBounds(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	// A locked clock stays within the jitter bound.
	require.NoError(t, f.manager.Tick(ctx))
	assert.LessOrEqual(t, math.Abs(f.leaf(t, "ptp/clock/offset").(float64)), DefaultJitterNS)
	assert.LessOrEqual(t, f.leaf(t, "ptp/clock/jitter").(float64), DefaultJitterNS)

	// A free-running clock wanders without the locked bound.
	_, err := f.manager.Handle(ctx, Event{Type: EventReset})
	require.NoError(t, err)
	require.NoError(t, f.manager.Tick(ctx))
	assert.LessOrEqual(t, math.Abs(f.leaf(t, "ptp/clock/offset").(float64)), DefaultFreerunOffsetNS)

	// Holdover drifts away from the offset the clock held when the source was
	// lost: one ppb is one nanosecond per second.
	_, err = f.manager.Handle(ctx, Event{Type: EventSourceDetected})
	require.NoError(t, err)
	_, err = f.manager.Handle(ctx, Event{Type: EventCalibrationComplete})
	require.NoError(t, err)
	_, err = f.manager.Handle(ctx, Event{Type: EventSourceLost})
	require.NoError(t, err)

	const seconds = 100
	f.clock.Advance(seconds * time.Second)
	require.NoError(t, f.manager.Tick(ctx))

	drift := f.leaf(t, "ptp/clock/offset").(float64) - f.manager.holdoverOffset
	assert.InDelta(t, DefaultDriftPPB*seconds, drift, 1e-6)
	assert.InDelta(t, DefaultDriftPPB*seconds, f.leaf(t, "ptp/clock/jitter").(float64), 1e-6)
}

// The holdover timer runs on the injected clock: advancing it past the
// configured timeout drives the transition from Run's loop.
func TestHoldoverTimerExpires(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go f.manager.Run(ctx)

	state, err := f.manager.SyncLoss(ctx, "eth0")
	require.NoError(t, err)
	require.Equal(t, StateHoldoverInSpec, state)

	assert.Equal(t, 1, f.clock.Pending(), "the holdover expiry is scheduled")

	f.clock.Advance(300 * time.Second)

	require.Eventually(t, func() bool {
		return f.state(t) == string(StateHoldoverOutOfSpec)
	}, 5*time.Second, 5*time.Millisecond)

	assert.True(t, hasEvent(f.drain(t), event.TypeAlarmRaised))
}

// A source restore stops the holdover timer, so the expiry cannot fire later.
func TestSourceRestoreStopsHoldover(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	_, err := f.manager.Handle(ctx, Event{Type: EventSourceLost})
	require.NoError(t, err)
	assert.Equal(t, 1, f.clock.Pending(), "holdover schedules one timer")

	_, err = f.manager.Handle(ctx, Event{Type: EventSourceRestored})
	require.NoError(t, err)
	assert.Equal(t, 0, f.clock.Pending(), "restoring the source cancels the timer")

	// A late expiry is a no-op.
	state, err := f.manager.Handle(ctx, Event{Type: EventHoldoverExpired})
	require.NoError(t, err)
	assert.Equal(t, StateLocked, state)
}

// A restart adopts the persisted clock state instead of falling back to
// free-run.
func TestAdoptsPersistedState(t *testing.T) {
	f := newFixture(t)
	ctx := t.Context()

	_, err := f.router.SetState(ctx, "ptp/clock/state", string(StateHoldoverOutOfSpec))
	require.NoError(t, err)

	state, err := f.manager.Handle(ctx, Event{Type: EventSourceRestored})
	require.NoError(t, err)
	assert.Equal(t, StateLocked, state)
}
