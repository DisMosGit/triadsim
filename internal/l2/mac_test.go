package l2

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

func TestLearnCreatesEntry(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	entry, err := manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)
	assert.Equal(t, "02:00:00:00:00:aa", entry.MAC)
	assert.Equal(t, uint16(100), entry.VLAN)
	// Bridge ports are 1-based interface indexes: radio0=1, eth0=2, eth1=3.
	assert.Equal(t, uint32(2), entry.Port)
	assert.Equal(t, model.MACEntryTypeDynamic, entry.Type)
	assert.False(t, entry.Permanent)

	// The entry is a normal part of the datastore.
	stored, err := manager.MACEntry(ctx, "02:00:00:00:00:AA")
	require.NoError(t, err)
	assert.Equal(t, entry, stored)

	entries, err := manager.MACEntries(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	// Ordered by VLAN, then by MAC: the seeded static entry is the lowest.
	assert.Equal(t, "02:00:00:00:00:02", entries[0].MAC)
	assert.Equal(t, "02:00:00:00:00:aa", entries[1].MAC)
}

func TestLearnIsVLANAware(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	// eth1 carries no VLAN at all.
	_, err := manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth1")
	assert.ErrorIs(t, err, ErrMemberNotFound)

	// VLAN 900 does not exist.
	_, err = manager.Learn(ctx, "02:00:00:00:00:AA", 900, "eth0")
	assert.ErrorIs(t, err, ErrMemberNotFound)

	// eth9 is not an interface.
	_, err = manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth9")
	assert.ErrorIs(t, err, ErrPortUnknown)

	_, err = manager.Learn(ctx, "not-a-mac", 100, "eth0")
	assert.ErrorIs(t, err, ErrInvalidMAC)

	// A rejected frame learns nothing.
	entries, err := manager.MACEntries(ctx)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestLearnRefreshesAndMoves(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.SetMember(ctx, 100, model.VLANPort{
		Port: "eth1", Mode: model.VLANPortModeAccess, PVID: 100,
	}))

	first, err := manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)

	// The age leaf advances, then a new frame resets it.
	_, err = r.SetState(ctx, macPrefix(first.MAC)+"/age", uint32(42))
	require.NoError(t, err)
	refreshed, err := manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)
	assert.Zero(t, refreshed.Age)

	// The station moves to another port: the entry follows it.
	moved, err := manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth1")
	require.NoError(t, err)
	assert.Equal(t, uint32(3), moved.Port)

	entries, err := manager.MACEntries(ctx)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "learning must not duplicate the entry")
}

func TestLearnKeepsPermanentEntry(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	// The seed holds a permanent entry for the eth1 MAC.
	entry, err := manager.Learn(ctx, "02:00:00:00:00:02", 100, "eth0")
	require.NoError(t, err)
	assert.Equal(t, model.MACEntryTypeStatic, entry.Type)
	assert.True(t, entry.Permanent)
	assert.Equal(t, uint32(3), entry.Port, "a permanent entry keeps its port")
}

func TestLearnRespectsMaxEntries(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	_, err := r.Set(ctx, store.Running, "mac-table/max-entries", uint32(1))
	require.NoError(t, err)

	_, err = manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	assert.ErrorIs(t, err, ErrTableFull)

	// The permanent entry already counted towards the limit.
	_, err = manager.SetStatic(ctx, model.MACEntry{MAC: "02:00:00:00:00:BB", VLAN: 100, Port: 2})
	assert.ErrorIs(t, err, ErrTableFull)

	// Replacing an existing entry is always allowed.
	_, err = manager.SetStatic(ctx, model.MACEntry{MAC: "02:00:00:00:00:02", VLAN: 100, Port: 2})
	require.NoError(t, err)
}

func TestSetStaticEntry(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	entry, err := manager.SetStatic(ctx, model.MACEntry{MAC: "02:00:00:00:00:BB", VLAN: 100, Port: 2})
	require.NoError(t, err)
	assert.Equal(t, model.MACEntryTypeStatic, entry.Type)
	assert.True(t, entry.Permanent)

	// A dynamic frame does not displace it.
	learned, err := manager.Learn(ctx, "02:00:00:00:00:BB", 100, "eth0")
	require.NoError(t, err)
	assert.Equal(t, model.MACEntryTypeStatic, learned.Type)

	// Validation comes from the model.
	_, err = manager.SetStatic(ctx, model.MACEntry{MAC: "02:00:00:00:00:CC", VLAN: 100, Port: 0})
	assert.ErrorContains(t, err, "port must be greater than 0")
}

func TestDeleteMACAndFlush(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	_, err := manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)
	_, err = manager.Learn(ctx, "02:00:00:00:00:BB", 100, "eth0")
	require.NoError(t, err)

	require.NoError(t, manager.DeleteMAC(ctx, "02:00:00:00:00:aa"))
	_, err = manager.MACEntry(ctx, "02:00:00:00:00:AA")
	assert.ErrorIs(t, err, ErrMACNotFound)

	assert.ErrorIs(t, manager.DeleteMAC(ctx, "02:00:00:00:00:AA"), ErrMACNotFound)
	assert.ErrorIs(t, manager.DeleteMAC(ctx, "nonsense"), ErrInvalidMAC)

	removed, err := manager.FlushMAC(ctx, 0)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)

	entries, err := manager.MACEntries(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, model.MACEntryTypeStatic, entries[0].Type)

	// A VLAN filter that matches nothing removes nothing.
	removed, err = manager.FlushMAC(ctx, 200)
	require.NoError(t, err)
	assert.Zero(t, removed)
}

func TestAgingRemovesIdleEntries(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	// The seeded aging time is 300 s; shorten it so the sweep is quick.
	_, err := r.Set(ctx, store.Running, "mac-table/aging-time", uint32(10))
	require.NoError(t, err)
	_, err = manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)

	fake := manager.Clock().(*clock.FakeClock)

	// The first sweep only starts the aging clock.
	require.NoError(t, manager.Tick(ctx))
	entry, err := manager.MACEntry(ctx, "02:00:00:00:00:AA")
	require.NoError(t, err)
	assert.Zero(t, entry.Age)

	fake.Advance(5 * time.Second)
	require.NoError(t, manager.Tick(ctx))
	entry, err = manager.MACEntry(ctx, "02:00:00:00:00:AA")
	require.NoError(t, err)
	assert.Equal(t, uint32(5), entry.Age)

	// Being learned again resets the age.
	fake.Advance(5 * time.Second)
	require.NoError(t, manager.Tick(ctx))
	_, err = manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)

	fake.Advance(5 * time.Second)
	require.NoError(t, manager.Tick(ctx))
	entry, err = manager.MACEntry(ctx, "02:00:00:00:00:AA")
	require.NoError(t, err)
	assert.Equal(t, uint32(5), entry.Age, "learning restarts the age")

	// Reaching the aging time removes the entry, while the permanent one stays.
	fake.Advance(5 * time.Second)
	require.NoError(t, manager.Tick(ctx))
	_, err = manager.MACEntry(ctx, "02:00:00:00:00:AA")
	assert.ErrorIs(t, err, ErrMACNotFound)

	entries, err := manager.MACEntries(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "02:00:00:00:00:02", entries[0].MAC)

	// current-count follows the table.
	result, err := r.Get(ctx, store.Running, "mac-table/current-count")
	require.NoError(t, err)
	assert.Equal(t, uint32(1), result.Value)
}

func TestAgingWithoutElapsedTimeIsANoop(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	_, err := manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)

	// No clock advance at all: the sweep must not touch the ages.
	require.NoError(t, manager.Tick(ctx))
	require.NoError(t, manager.Tick(ctx))
	entry, err := manager.MACEntry(ctx, "02:00:00:00:00:AA")
	require.NoError(t, err)
	assert.Zero(t, entry.Age)
}

func TestCurrentCountTracksTable(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	_, err := manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)
	result, err := r.Get(ctx, store.Running, "mac-table/current-count")
	require.NoError(t, err)
	assert.Equal(t, uint32(2), result.Value)

	require.NoError(t, manager.DeleteMAC(ctx, "02:00:00:00:00:AA"))
	result, err = r.Get(ctx, store.Running, "mac-table/current-count")
	require.NoError(t, err)
	assert.Equal(t, uint32(1), result.Value)
}

func TestBridgePortRoundTrip(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	index, err := manager.BridgePort(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, uint32(3), index)

	name, err := manager.PortName(ctx, index)
	require.NoError(t, err)
	assert.Equal(t, "eth1", name)

	_, err = manager.BridgePort(ctx, "eth9")
	assert.ErrorIs(t, err, ErrPortUnknown)
	_, err = manager.PortName(ctx, 99)
	assert.ErrorIs(t, err, ErrPortUnknown)
}

func TestRunAgesOnTheInjectedClock(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	_, err := r.Set(ctx, store.Running, "mac-table/aging-time", uint32(10))
	require.NoError(t, err)
	_, err = manager.Learn(ctx, "02:00:00:00:00:AA", 100, "eth0")
	require.NoError(t, err)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		manager.Run(runCtx)
	}()

	fake := manager.Clock().(*clock.FakeClock)
	pending := func() bool { return fake.Pending() > 0 }
	// Run schedules its first period only after the goroutine starts. The
	// first period starts the aging clock, the second one ages the entry.
	require.Eventually(t, pending, time.Second, time.Millisecond)
	fake.Advance(manager.tickInterval)
	require.Eventually(t, pending, time.Second, time.Millisecond)
	fake.Advance(5 * time.Second)
	require.Eventually(t, func() bool {
		entry, err := manager.MACEntry(ctx, "02:00:00:00:00:AA")
		return err == nil && entry.Age == 5
	}, time.Second, time.Millisecond)

	cancel()
	<-done
	assert.Zero(t, fake.Pending(), "Run must stop its timer when the context ends")
}
