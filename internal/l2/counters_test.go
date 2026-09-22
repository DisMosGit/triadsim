package l2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/model"
)

// twoPortVLAN makes eth0 and eth1 members of VLAN 100 so frames can be
// forwarded between them.
func twoPortVLAN(t *testing.T, manager *Manager) {
	t.Helper()
	require.NoError(t, manager.SetMember(t.Context(), 100, model.VLANPort{
		Port: "eth1", Mode: model.VLANPortModeAccess, PVID: 100,
	}))
}

func TestCountersStartAtZero(t *testing.T) {
	manager, _, _ := newTestManager(t)

	counters, err := manager.Counters(t.Context(), "eth0")
	require.NoError(t, err)
	assert.Equal(t, model.InterfaceCounters{}, counters)

	_, err = manager.Counters(t.Context(), "eth9")
	assert.ErrorIs(t, err, ErrPortUnknown)
}

func TestTransmitFloodsUnknownDestination(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()
	twoPortVLAN(t, manager)

	result, err := manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:AA", Dst: "02:00:00:00:00:BB",
		VLAN: 100, Ingress: "eth0", Bytes: 128,
	})
	require.NoError(t, err)
	assert.True(t, result.Learned)
	assert.Equal(t, []string{"eth1"}, result.Egress)
	assert.Empty(t, result.Dropped)

	ingress, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, uint64(128), ingress.InOctets)
	assert.Equal(t, uint64(1), ingress.InUcastPkts)
	assert.Zero(t, ingress.OutOctets)

	egress, err := manager.Counters(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, uint64(128), egress.OutOctets)
	assert.Equal(t, uint64(1), egress.OutUcastPkts)

	// The source address is now in the forwarding database.
	entry, err := manager.MACEntry(ctx, "02:00:00:00:00:AA")
	require.NoError(t, err)
	assert.Equal(t, uint32(2), entry.Port)
	assert.Equal(t, uint16(100), entry.VLAN)
}

func TestTransmitForwardKnownDestination(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()
	twoPortVLAN(t, manager)

	// Learn both stations: AA on eth0, BB on eth1.
	_, err := manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:AA", Dst: "ff:ff:ff:ff:ff:ff",
		VLAN: 100, Ingress: "eth0", Bytes: 64, Broadcast: true,
	})
	require.NoError(t, err)
	_, err = manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:BB", Dst: "ff:ff:ff:ff:ff:ff",
		VLAN: 100, Ingress: "eth1", Bytes: 64, Broadcast: true,
	})
	require.NoError(t, err)

	before, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)

	result, err := manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:BB", Dst: "02:00:00:00:00:AA",
		VLAN: 100, Ingress: "eth1", Bytes: 100,
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"eth0"}, result.Egress, "a known destination is not flooded")

	after, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, before.OutOctets+100, after.OutOctets)
	assert.Equal(t, before.InOctets, after.InOctets, "the frame does not arrive on eth0 twice")
}

func TestTransmitFiltersSamePort(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()
	twoPortVLAN(t, manager)

	// The seeded permanent entry maps the eth1 MAC to bridge port 3, but the
	// station is learned on eth0 here, so the frame must be filtered.
	_, err := manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:AA", Dst: "ff:ff:ff:ff:ff:ff",
		VLAN: 100, Ingress: "eth0", Bytes: 64, Broadcast: true,
	})
	require.NoError(t, err)

	result, err := manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:AA", Dst: "02:00:00:00:00:AA",
		VLAN: 100, Ingress: "eth0", Bytes: 64,
	})
	require.NoError(t, err)
	assert.Empty(t, result.Egress, "a frame for the ingress port is filtered")
}

func TestTransmitCountsDiscardsAndErrors(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	// eth1 is not a member of VLAN 100 yet: the frame is discarded.
	result, err := manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:AA", Dst: "02:00:00:00:00:BB",
		VLAN: 100, Ingress: "eth1", Bytes: 64,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Dropped, "is not on port eth1")
	assert.Empty(t, result.Egress)

	counters, err := manager.Counters(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, uint32(1), counters.InDiscards)
	assert.Zero(t, counters.InOctets)

	// A frame above the interface MTU is an input error.
	result, err = manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:AA", Dst: "02:00:00:00:00:BB",
		VLAN: 100, Ingress: "eth0", Bytes: 9000,
	})
	require.NoError(t, err)
	assert.Contains(t, result.Dropped, "exceeds the 1500 byte MTU")

	counters, err = manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, uint32(1), counters.InErrors)
	assert.Zero(t, counters.InOctets)
}

func TestTransmitRejectsBadInput(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	_, err := manager.Transmit(ctx, Frame{Src: "nope", Dst: "02:00:00:00:00:BB", VLAN: 100, Ingress: "eth0"})
	assert.ErrorIs(t, err, ErrInvalidMAC)

	_, err = manager.Transmit(ctx, Frame{Src: "02:00:00:00:00:AA", Dst: "nope", VLAN: 100, Ingress: "eth0"})
	assert.ErrorIs(t, err, ErrInvalidMAC)

	_, err = manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:AA", Dst: "02:00:00:00:00:BB", VLAN: 100, Ingress: "eth9",
	})
	assert.ErrorIs(t, err, ErrPortUnknown)
}

func TestTransmitUsesMinimumFrameSize(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	_, err := manager.Transmit(ctx, Frame{
		Src: "02:00:00:00:00:AA", Dst: "02:00:00:00:00:BB", VLAN: 100, Ingress: "eth0",
	})
	require.NoError(t, err)

	counters, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, MinFrameBytes, counters.InOctets)
}

func TestIncrementCounters(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.Increment(ctx, "eth0", model.InterfaceCounters{
		InOctets: 1000, OutOctets: 2000, InErrors: 1, OutDiscards: 3,
	}))
	require.NoError(t, manager.Increment(ctx, "eth0", model.InterfaceCounters{InOctets: 500, InErrors: 1}))

	counters, err := manager.Counters(ctx, "eth0")
	require.NoError(t, err)
	assert.Equal(t, model.InterfaceCounters{
		InOctets: 1500, OutOctets: 2000, InErrors: 2, OutDiscards: 3,
	}, counters)

	assert.ErrorIs(t, manager.Increment(ctx, "eth9", model.InterfaceCounters{InOctets: 1}), ErrPortUnknown)
}
