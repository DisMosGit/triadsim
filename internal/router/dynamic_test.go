package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/store"
)

// Dynamic list instances are the Phase 4 addition: the store, not the boot
// template, decides which VLANs, MAC entries and LLDP neighbours exist.
func TestSetCreatesCreatableListEntry(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)

	path := "vlans/vlan[id=200]/name"
	result, err := r.Set(ctx, store.Running, path, "VOICE")
	require.NoError(t, err)
	assert.Equal(t, "VOICE", result.Value)

	stored, err := st.Get(ctx, store.Running, path)
	require.NoError(t, err)
	assert.Equal(t, "VOICE", stored)
}

func TestSetRejectsClosedListEntry(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	// The interface list is closed: interfaces come from the device seed.
	_, err := r.Set(ctx, store.Running, "interfaces/interface[name=eth9]/mtu", 1500)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSetCreatesMACAndLLDPEntries(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	// The MAC entry does not exist in the template, but the list is creatable,
	// so a writable leaf opens it.
	_, err := r.Set(ctx, store.Running, "mac-table/entry[mac-address=00:11:22:33:44:55]/port", 2)
	require.NoError(t, err)

	// Age is read-only even in a freshly created entry.
	_, err = r.Set(ctx, store.Running, "mac-table/entry[mac-address=00:11:22:33:44:55]/age", 10)
	assert.ErrorIs(t, err, ErrReadOnly)

	_, err = r.Set(ctx, store.Running, "lldp/neighbors/neighbor[port=eth1]/ttl", 120)
	require.NoError(t, err)
}

func TestValidateAcceptsNewListEntry(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)

	snapshot, err := snapshotDatastore(ctx, st, store.Running)
	require.NoError(t, err)

	snapshot["vlans/vlan[id=200]/id"] = uint16(200)
	snapshot["vlans/vlan[id=200]/name"] = "VOICE"
	require.NoError(t, r.Validate(ctx, snapshot), "a new VLAN must be validated like any other")

	// The model rules apply to the new entry too: a VLAN needs a name.
	snapshot["vlans/vlan[id=200]/name"] = ""
	assert.Error(t, r.Validate(ctx, snapshot))
}

func TestValidateRejectsUnknownPaths(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)

	snapshot, err := snapshotDatastore(ctx, st, store.Running)
	require.NoError(t, err)

	tests := []struct {
		name  string
		path  string
		value any
	}{
		{name: "unknown container", path: "bogus/x", value: 1},
		{name: "unknown leaf", path: "system-info/nope", value: 1},
		{name: "container instead of leaf", path: "vlans", value: 1},
		{name: "missing list predicate", path: "vlans/vlan/name", value: "x"},
		{name: "wrong leaf type", path: "vlans/vlan[id=200]/id", value: "not-a-number"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := make(map[string]any, len(snapshot)+1)
			for path, value := range snapshot {
				failed[path] = value
			}
			failed[tt.path] = tt.value

			assert.Error(t, r.Validate(ctx, failed))
		})
	}
}

// A list entry created in candidate survives Commit only when the model
// accepts the whole proposed configuration.
func TestCommitAcceptsCreatedEntry(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)

	_, err := r.Set(ctx, store.Candidate, "vlans/vlan[id=200]/id", 200)
	require.NoError(t, err)
	_, err = r.Set(ctx, store.Candidate, "vlans/vlan[id=200]/name", "VOICE")
	require.NoError(t, err)
	require.NoError(t, st.Commit(ctx))

	stored, err := st.Get(ctx, store.Running, "vlans/vlan[id=200]/name")
	require.NoError(t, err)
	assert.Equal(t, "VOICE", stored)
}

func TestSetStateWritesBothDatastores(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)
	rssi := "interfaces/interface[name=radio0]/radio-link/rssi"

	_, err := r.SetState(ctx, rssi, -50.0)
	require.NoError(t, err)

	for _, ds := range []store.Datastore{store.Running, store.Candidate} {
		stored, err := st.Get(ctx, ds, rssi)
		require.NoError(t, err, "state must live in %s", ds)
		assert.Equal(t, -50.0, stored)
	}

	// The management-plane path still rejects the read-only leaf.
	_, err = r.Set(ctx, store.Running, rssi, -51.0)
	assert.ErrorIs(t, err, ErrReadOnly)

	require.NoError(t, r.DeleteState(ctx, rssi))
	_, err = st.Get(ctx, store.Running, rssi)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// Deleting an already absent state leaf is a no-op.
	require.NoError(t, r.DeleteState(ctx, rssi))
}

// A template list entry whose stored leaves were all deleted disappears from
// the snapshot, so a domain sees the datastore, not the boot template.
func TestSnapshotDropsDeletedListEntry(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	device, err := r.Snapshot(ctx, store.Running)
	require.NoError(t, err)
	require.Len(t, device.VLANs, 1)

	leaves, err := r.List(ctx, store.Running, "vlans/vlan[id=100]")
	require.NoError(t, err)
	require.NotEmpty(t, leaves)
	for _, leaf := range leaves {
		require.NoError(t, r.Delete(ctx, store.Running, leaf.Path))
	}

	device, err = r.Snapshot(ctx, store.Running)
	require.NoError(t, err)
	assert.Empty(t, device.VLANs)

	// Opening the list again through a writable leaf brings it back.
	_, err = r.Set(ctx, store.Running, "vlans/vlan[id=100]/name", "DATA")
	require.NoError(t, err)
	device, err = r.Snapshot(ctx, store.Running)
	require.NoError(t, err)
	require.Len(t, device.VLANs, 1)
	assert.Equal(t, "DATA", device.VLANs[0].Name)
}

// An empty datastore keeps the boot template, which is what validating a
// freshly created candidate needs.
func TestSnapshotOfEmptyDatastoreKeepsTemplate(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	device, err := r.Snapshot(ctx, store.Startup)
	require.NoError(t, err)
	require.Len(t, device.VLANs, 1)
	assert.Equal(t, uint16(100), device.VLANs[0].ID)
}
