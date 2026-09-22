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

// stormManager returns a manager with a 100 pps storm threshold and a bus the
// test can read.
func stormManager(t *testing.T) (*Manager, *event.Bus, <-chan event.Event) {
	t.Helper()

	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	require.NoError(t, r.Seed(t.Context(), store.Running))
	require.NoError(t, st.Rollback(t.Context()))

	bus := event.New(16, clock.NewFakeClock())
	events := bus.Subscribe()

	manager, err := New(Deps{
		Router:            r,
		Bus:               bus,
		Clock:             clock.NewFakeClock(),
		StormThresholdPPS: 100,
	})
	require.NoError(t, err)
	t.Cleanup(bus.Close)
	return manager, bus, events
}

func TestStormCountsAndFloods(t *testing.T) {
	manager, _, _ := stormManager(t)
	ctx := t.Context()
	twoPortVLAN(t, manager)

	require.NoError(t, manager.Storm(ctx, "eth0", 10))
	assert.False(t, manager.Storming("eth0"))

	ingress, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, uint64(10*MinFrameBytes), ingress.InOctets)
	assert.Equal(t, uint64(10), ingress.InUcastPkts)
	assert.Zero(t, ingress.InDiscards)

	// The frames are flooded to the other member port of the VLAN.
	egress, err := manager.Counters(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, uint64(10), egress.OutUcastPkts)
	assert.Equal(t, uint64(10*MinFrameBytes), egress.OutOctets)
}

func TestStormRaisesAndClearsAlarm(t *testing.T) {
	manager, _, events := stormManager(t)
	ctx := t.Context()
	twoPortVLAN(t, manager)

	// 150 frames in one injection cross the 100 pps limit: the alarm is raised
	// and the frames above the limit are discarded.
	require.NoError(t, manager.Storm(ctx, "eth0", 150))
	assert.True(t, manager.Storming("eth0"))

	select {
	case got := <-events:
		assert.Equal(t, event.TypeAlarmRaised, got.Type)
		assert.Equal(t, "l2/storm/eth0", got.Resource)
		assert.Equal(t, "major", got.Severity)
		assert.Contains(t, got.Message, "150 pps above the 100 pps limit")
	default:
		t.Fatal("a storm above the threshold must raise an alarm")
	}

	counters, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, uint64(100), counters.InUcastPkts, "only the frames within the limit are accepted")
	assert.Equal(t, uint32(50), counters.InDiscards)

	// The rate stays above the limit: every frame is dropped, but no second
	// alarm is published.
	require.NoError(t, manager.Storm(ctx, "eth0", 10))
	select {
	case got := <-events:
		t.Fatalf("unexpected event %+v", got)
	default:
	}

	// Time moves on, the window empties and the alarm clears.
	fake := manager.Clock().(*clock.FakeClock)
	fake.Advance(2 * time.Second)
	require.NoError(t, manager.Storm(ctx, "eth0", 10))
	assert.False(t, manager.Storming("eth0"))

	select {
	case got := <-events:
		assert.Equal(t, event.TypeAlarmCleared, got.Type)
		assert.Equal(t, "l2/storm/eth0", got.Resource)
	default:
		t.Fatal("a storm that ended must clear its alarm")
	}
}

func TestStormRateWindowSlides(t *testing.T) {
	manager, _, _ := stormManager(t)
	ctx := t.Context()

	fake := manager.Clock().(*clock.FakeClock)
	for i := 0; i < 4; i++ {
		require.NoError(t, manager.Storm(ctx, "eth0", 50))
		fake.Advance(500 * time.Millisecond)
	}
	// 200 packets inside one second: above the 100 pps limit.
	assert.True(t, manager.Storming("eth0"))

	// A second later the window only holds the last injection.
	fake.Advance(time.Second)
	require.NoError(t, manager.Storm(ctx, "eth0", 10))
	assert.False(t, manager.Storming("eth0"))
}

func TestStormRejectsBadInput(t *testing.T) {
	manager, _, _ := stormManager(t)
	ctx := t.Context()

	assert.ErrorIs(t, manager.Storm(ctx, "eth9", 10), ErrPortUnknown)
	assert.NoError(t, manager.Storm(ctx, "eth0", 0), "an empty injection is a no-op")
	assert.ErrorIs(t, manager.Storm(ctx, "eth0", MaxStormPackets+1), ErrStormTooLarge)

	counters, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, model.InterfaceCounters{}, counters, "a rejected injection moves no counter")
}

func TestStormWithoutVLANOnlyCountsIngress(t *testing.T) {
	manager, _, _ := stormManager(t)
	ctx := t.Context()

	// eth1 is not a member of any VLAN: there is nothing to flood to.
	require.NoError(t, manager.Storm(ctx, "eth1", 10))

	ingress, err := manager.Counters(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, uint64(10), ingress.InUcastPkts)

	egress, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Zero(t, egress.OutUcastPkts)
}

// The manager satisfies the RESTCONF storm endpoint contract.
func TestStormSatisfiesRESTCONFInterface(t *testing.T) {
	manager, _, _ := stormManager(t)
	var injector interface {
		Storm(ctx context.Context, port string, packets uint32) error
	} = manager
	require.NoError(t, injector.Storm(t.Context(), "eth0", 1))
}
