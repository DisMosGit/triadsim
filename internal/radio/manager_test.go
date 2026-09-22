package radio

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

// newFixture builds a fixture. mutate, when set, adjusts the device template
// before the router indexes it, which is how a test pins the link budget down.
func newFixture(t *testing.T, mutate func(*model.Device)) *fixture {
	t.Helper()

	device := model.DefaultDevice()
	if mutate != nil {
		mutate(device)
	}

	st := store.NewMemory(store.Options{})
	r, err := router.New(device, st)
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

// noATPC disables the seeded ATPC loop, so a test's link budget is a fixed
// number of dBm instead of a moving control loop.
func noATPC(device *model.Device) {
	device.Interfaces[0].RadioLink.ATPC.Enabled = false
}

// leaf reads one leaf from the running datastore.
func (f *fixture) leaf(t *testing.T, suffix string) any {
	t.Helper()

	value, err := f.store.Get(t.Context(), store.Running, linkPath("radio0", suffix))
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

func TestNewRequiresRouter(t *testing.T) {
	manager, err := New(Deps{})

	assert.Nil(t, manager)
	assert.ErrorIs(t, err, ErrNoRouter)
	assert.ErrorContains(t, err, "router must not be nil")
}

func TestTickComputesTheLinkBudget(t *testing.T) {
	f := newFixture(t, noATPC)
	require.NoError(t, f.manager.Tick(t.Context()))

	assert.InDelta(t, -46.4937, f.leaf(t, "rssi"), 0.01)
	assert.InDelta(t, -46.4937, f.leaf(t, "link-budget/calculated-rsl"), 0.01)
	assert.EqualValues(t, 308, f.leaf(t, "capacity"))
	assert.EqualValues(t, 12, f.leaf(t, "acm/current-profile"))
	assert.EqualValues(t, 308, f.leaf(t, "acm/current-capacity"))
	assert.InDelta(t, 8.5063, f.leaf(t, "fade-margin"), 0.01)
	assert.Equal(t, model.RadioLinkStateUp, f.leaf(t, "link-state"))
	assert.Empty(t, f.drain(t), "a healthy link raises no alarm")
}

func TestRadioFailureDrivesTheLinkDown(t *testing.T) {
	f := newFixture(t, noATPC)
	ctx := t.Context()

	state, err := f.manager.RadioFailure(ctx, "radio0", 0)
	require.NoError(t, err)
	assert.Equal(t, model.RadioLinkStateDown, state)
	assert.Equal(t, model.RadioLinkStateDown, f.leaf(t, "link-state"))
	assert.Equal(t, model.RSSIMinDBM, f.leaf(t, "rssi"))

	events := f.drain(t)
	require.Len(t, events, 1)
	assert.Equal(t, event.TypeAlarmRaised, events[0].Type)
	assert.Equal(t, event.DomainRadio, events[0].Domain)
	assert.Equal(t, event.AlarmRadioLinkDown, events[0].Alarm)
	assert.Equal(t, "critical", events[0].Severity)
	assert.Equal(t, "radio0", events[0].Resource)

	// The state the domain wrote is valid: a management-plane commit accepts it.
	require.NoError(t, f.store.Commit(ctx))

	state, err = f.manager.RadioRestore(ctx, "radio0")
	require.NoError(t, err)
	assert.Equal(t, model.RadioLinkStateUp, state)
	assert.Equal(t, model.RadioLinkStateUp, f.leaf(t, "link-state"))

	events = f.drain(t)
	require.Len(t, events, 1)
	assert.Equal(t, event.TypeAlarmCleared, events[0].Type)
	assert.Equal(t, event.AlarmRadioLinkDown, events[0].Alarm)
	assert.Equal(t, "cleared", events[0].Severity)
}

func TestRadioFailureInjectsTheDegradedBand(t *testing.T) {
	f := newFixture(t, noATPC)
	ctx := t.Context()

	state, err := f.manager.RadioFailure(ctx, "radio0", DefaultDegradeFadeDB)
	require.NoError(t, err)
	assert.Equal(t, model.RadioLinkStateDegraded, state)
	assert.Equal(t, model.RadioLinkStateDegraded, f.leaf(t, "link-state"))

	events := f.drain(t)
	require.Len(t, events, 1)
	assert.Equal(t, event.TypeAlarmRaised, events[0].Type)
	assert.Equal(t, event.AlarmRadioLinkDegraded, events[0].Alarm)
	assert.Equal(t, "major", events[0].Severity)

	// A fully down link clears the degraded alarm before raising the down one.
	state, err = f.manager.RadioFailure(ctx, "radio0", 0)
	require.NoError(t, err)
	assert.Equal(t, model.RadioLinkStateDown, state)

	events = f.drain(t)
	require.Len(t, events, 2)
	assert.Equal(t, event.AlarmRadioLinkDegraded, events[0].Alarm)
	assert.Equal(t, event.TypeAlarmCleared, events[0].Type)
	assert.Equal(t, event.AlarmRadioLinkDown, events[1].Alarm)
	assert.Equal(t, event.TypeAlarmRaised, events[1].Type)

	// A link that recovers to a healthy margin clears the down alarm.
	state, err = f.manager.RadioRestore(ctx, "radio0")
	require.NoError(t, err)
	assert.Equal(t, model.RadioLinkStateUp, state)
	events = f.drain(t)
	require.Len(t, events, 1)
	assert.Equal(t, event.AlarmRadioLinkDown, events[0].Alarm)
	assert.Equal(t, event.TypeAlarmCleared, events[0].Type)
}

func TestHysteresisKeepsADownLinkDown(t *testing.T) {
	f := newFixture(t, noATPC)
	ctx := t.Context()

	// -86.5 dBm is below the raise threshold, so the link goes down.
	_, err := f.manager.RadioFailure(ctx, "radio0", 40)
	require.NoError(t, err)
	require.Equal(t, model.RadioLinkStateDown, f.leaf(t, "link-state"))
	require.Len(t, f.drain(t), 1)

	// -84.5 dBm is above the raise threshold but below the clear one: the link
	// stays down and no alarm is repeated.
	state, err := f.manager.RadioFailure(ctx, "radio0", 38)
	require.NoError(t, err)
	assert.Equal(t, model.RadioLinkStateDown, state)
	assert.Empty(t, f.drain(t))
}

func TestRadioFailureRejectsAnUnknownLink(t *testing.T) {
	f := newFixture(t, noATPC)

	state, err := f.manager.RadioFailure(t.Context(), "radio9", 0)

	assert.Empty(t, state)
	assert.ErrorIs(t, err, ErrUnknownLink)
}

func TestRadioFailureAnEmptyLinkSelectsTheFirstRadio(t *testing.T) {
	f := newFixture(t, noATPC)

	state, err := f.manager.RadioFailure(t.Context(), "", 0)

	require.NoError(t, err)
	assert.Equal(t, model.RadioLinkStateDown, state)
	assert.Equal(t, model.RadioLinkStateDown, f.leaf(t, "link-state"))
}

func TestRadioRestoreRejectsAnUnknownLink(t *testing.T) {
	f := newFixture(t, noATPC)

	state, err := f.manager.RadioRestore(t.Context(), "radio9")

	assert.Empty(t, state)
	assert.ErrorIs(t, err, ErrUnknownLink)
}

func TestATPCStepsTowardsTheTarget(t *testing.T) {
	f := newFixture(t, nil)
	ctx := t.Context()

	// The seeded ATPC power is 18.5 dBm and the target is -45 dBm, so the first
	// step raises the power by the 1 dB limit.
	require.NoError(t, f.manager.Tick(ctx))
	assert.InDelta(t, 19.5, f.leaf(t, "atpc/current-power"), 0.001)

	require.NoError(t, f.manager.Tick(ctx))
	assert.InDelta(t, 20.5, f.leaf(t, "atpc/current-power"), 0.001)
}

// A restart adopts the persisted link state instead of raising an alarm for a
// link that never changed.
func TestTickAdoptsThePersistedLinkState(t *testing.T) {
	f := newFixture(t, noATPC)
	ctx := t.Context()

	require.NoError(t, f.manager.Tick(ctx))
	f.drain(t)

	restarted, err := New(Deps{Router: f.router, Bus: f.bus, Clock: clock.NewFakeClock()})
	require.NoError(t, err)
	require.NoError(t, restarted.Tick(ctx))

	assert.Equal(t, model.RadioLinkStateUp, f.leaf(t, "link-state"))
	assert.Empty(t, f.drain(t))
}

func TestRunTicksOnTheInjectedClock(t *testing.T) {
	f := newFixture(t, nil)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	go f.manager.Run(ctx)

	require.Eventually(t, func() bool {
		value, err := f.store.Get(ctx, store.Running, linkPath("radio0", "atpc/current-power"))
		return err == nil && value.(float64) > 18.6
	}, 5*time.Second, 5*time.Millisecond, "the initial tick must run")

	f.clock.Advance(DefaultTickInterval)

	require.Eventually(t, func() bool {
		value, err := f.store.Get(ctx, store.Running, linkPath("radio0", "atpc/current-power"))
		return err == nil && value.(float64) > 19.6
	}, 5*time.Second, 5*time.Millisecond, "the periodic tick must step the ATPC power")
}

// A link without a usable modulation profile keeps its fade margin and
// capacity instead of writing a value the model cannot derive.
func TestTickWithoutProfilesLeavesTheDerivedLeavesAlone(t *testing.T) {
	f := newFixture(t, func(device *model.Device) {
		noATPC(device)
		device.Interfaces[0].RadioLink.Profiles = nil
	})
	ctx := t.Context()

	require.NoError(t, f.manager.Tick(ctx))

	assert.InDelta(t, -46.4937, f.leaf(t, "rssi"), 0.01)
	assert.InDelta(t, 12.5, f.leaf(t, "fade-margin"), 0.01)
	assert.EqualValues(t, 112, f.leaf(t, "capacity"))
	assert.Equal(t, model.RadioLinkStateUp, f.leaf(t, "link-state"))
	assert.False(t, math.IsInf(f.leaf(t, "fade-margin").(float64), 1))
}
