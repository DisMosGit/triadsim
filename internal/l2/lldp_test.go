package l2

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

func neighbor(port, chassis, portID string, ttl uint32) model.LLDPNeighbor {
	return model.LLDPNeighbor{
		Port:              port,
		ChassisID:         chassis,
		PortID:            portID,
		SystemName:        "peer-" + port,
		SystemDescription: "peer switch",
		TTL:               ttl,
		Capabilities:      "bridge",
	}
}

func TestLLDPConfigOfSeed(t *testing.T) {
	manager, _, _ := newTestManager(t)

	config, err := manager.LLDP(t.Context())
	require.NoError(t, err)
	assert.True(t, config.Enabled)
	assert.Equal(t, uint32(30), config.TxInterval)
	assert.Equal(t, uint32(4), config.TxHoldMultiplier)
	assert.Empty(t, config.Neighbors)
}

func TestSetAndListNeighbors(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.SetNeighbor(ctx, neighbor("eth1", "02:00:00:00:00:aa", "eth0", 120)))
	require.NoError(t, manager.SetNeighbor(ctx, neighbor("eth0", "02:00:00:00:00:bb", "eth1", 120)))

	neighbors, err := manager.Neighbors(ctx)
	require.NoError(t, err)
	require.Len(t, neighbors, 2)
	assert.Equal(t, []string{"eth0", "eth1"}, []string{neighbors[0].Port, neighbors[1].Port})

	got, err := manager.Neighbor(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, "02:00:00:00:00:aa", got.ChassisID)

	// The neighbour lives in the datastore like any other managed object.
	result, err := r.Get(ctx, store.Running, lldpNeighborPrefix("eth1")+"/port-id")
	require.NoError(t, err)
	assert.Equal(t, "eth0", result.Value)

	// Setting the same port again replaces the entry.
	require.NoError(t, manager.SetNeighbor(ctx, neighbor("eth1", "02:00:00:00:00:cc", "eth5", 60)))
	neighbors, err = manager.Neighbors(ctx)
	require.NoError(t, err)
	require.Len(t, neighbors, 2)
	got, err = manager.Neighbor(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, "02:00:00:00:00:cc", got.ChassisID)
	assert.Equal(t, uint32(60), got.TTL)
}

func TestSetNeighborValidation(t *testing.T) {
	tests := []struct {
		name    string
		entry   model.LLDPNeighbor
		wantErr string
	}{
		{
			name:    "empty port",
			entry:   model.LLDPNeighbor{ChassisID: "02:00:00:00:00:aa", PortID: "eth0", TTL: 120},
			wantErr: "port must not be empty",
		},
		{
			name:    "empty chassis id",
			entry:   model.LLDPNeighbor{Port: "eth1", PortID: "eth0", TTL: 120},
			wantErr: "chassis-id must not be empty",
		},
		{
			name:    "empty port id",
			entry:   model.LLDPNeighbor{Port: "eth1", ChassisID: "02:00:00:00:00:aa", TTL: 120},
			wantErr: "port-id must not be empty",
		},
		{
			name:    "zero ttl",
			entry:   model.LLDPNeighbor{Port: "eth1", ChassisID: "02:00:00:00:00:aa", PortID: "eth0"},
			wantErr: "ttl 0 out of range",
		},
		{
			name:    "ttl above maximum",
			entry:   model.LLDPNeighbor{Port: "eth1", ChassisID: "02:00:00:00:00:aa", PortID: "eth0", TTL: 65536},
			wantErr: "ttl 65536 out of range",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			manager, _, _ := newTestManager(t)

			err := manager.SetNeighbor(t.Context(), test.entry)
			require.ErrorContains(t, err, test.wantErr)

			neighbors, err := manager.Neighbors(t.Context())
			require.NoError(t, err)
			assert.Empty(t, neighbors)
		})
	}
}

func TestRemoveNeighbor(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.SetNeighbor(ctx, neighbor("eth1", "02:00:00:00:00:aa", "eth0", 120)))
	require.NoError(t, manager.RemoveNeighbor(ctx, "eth1"))

	_, err := manager.Neighbor(ctx, "eth1")
	assert.ErrorIs(t, err, ErrNeighborNotFound)
	assert.ErrorIs(t, manager.RemoveNeighbor(ctx, "eth1"), ErrNeighborNotFound)
	assert.ErrorIs(t, manager.RemoveNeighbor(ctx, "eth0"), ErrNeighborNotFound)
}

func TestLLDPRefreshOnTick(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	// A neighbour that a peer announced with a short TTL.
	require.NoError(t, manager.SetNeighbor(ctx, neighbor("eth1", "02:00:00:00:00:aa", "eth0", 1)))

	fake := manager.Clock().(*clock.FakeClock)
	fake.Advance(manager.tickInterval)
	require.NoError(t, manager.Tick(ctx))

	// The periodic TLV refresh sets the configured TTL: 30 s x 4.
	got, err := manager.Neighbor(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, uint32(120), got.TTL)

	result, err := r.Get(ctx, store.Running, lldpNeighborPrefix("eth1")+"/ttl")
	require.NoError(t, err)
	assert.Equal(t, uint32(120), result.Value)

	// A second period leaves an up-to-date TTL alone.
	config, err := manager.LLDP(ctx)
	require.NoError(t, err)
	require.NoError(t, manager.SetNeighbor(ctx, neighbor("eth1", "02:00:00:00:00:aa", "eth0", config.TxInterval*config.TxHoldMultiplier)))
	before := fake.Pending()
	fake.Advance(manager.tickInterval)
	require.NoError(t, manager.Tick(ctx))
	assert.Equal(t, before, fake.Pending())
}

func TestLLDPRefreshHonoursEnabled(t *testing.T) {
	manager, r, _ := newTestManager(t)
	ctx := t.Context()

	require.NoError(t, manager.SetNeighbor(ctx, neighbor("eth1", "02:00:00:00:00:aa", "eth0", 1)))
	_, err := r.Set(ctx, store.Running, lldpPrefix+"/enabled", false)
	require.NoError(t, err)

	fake := manager.Clock().(*clock.FakeClock)
	fake.Advance(manager.tickInterval)
	require.NoError(t, manager.Tick(ctx))

	got, err := manager.Neighbor(ctx, "eth1")
	require.NoError(t, err)
	assert.Equal(t, uint32(1), got.TTL, "a disabled transmitter must not refresh TLVs")
}

func TestLLDPRefreshWithNoNeighbors(t *testing.T) {
	manager, _, _ := newTestManager(t)
	ctx := t.Context()

	fake := manager.Clock().(*clock.FakeClock)
	fake.Advance(manager.tickInterval)
	require.NoError(t, manager.Tick(ctx))

	neighbors, err := manager.Neighbors(ctx)
	require.NoError(t, err)
	assert.Empty(t, neighbors)
}
