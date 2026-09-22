package l2

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

// A forwarding-database entry created through a running-targeted write — how
// NETCONF with <target><running/>, RESTCONF with the default datastore and the
// L2 domain itself create one — must survive a MAC aging tick and a <commit>.
//
// Before writes to running were mirrored into candidate, one aging sweep wrote
// the read-only age leaf to both datastores while the entry's configuration
// lived in running only, so candidate held an age-only orphan that validated as
// a MACEntry with no type and port 0: every later commit was rejected with
// invalid-value. The same asymmetry let a later commit resurrect a deleted
// entry.
func TestRunningCreatedMACEntrySurvivesACommit(t *testing.T) {
	manager, r, st := newTestManager(t)
	ctx := t.Context()

	const mac = "02:00:00:00:00:aa"
	// The datastore keys a MAC entry by the address exactly as the writer typed
	// it, so use the canonical lowercase form the L2 domain itself writes.
	const prefix = "mac-table/entry[mac-address=" + mac + "]"

	// Shorten the seeded aging time and create the entry the way a management
	// plane targeting running does.
	_, err := r.Set(ctx, store.Running, "mac-table/aging-time", uint32(10))
	require.NoError(t, err)
	for path, value := range map[string]any{
		prefix + "/vlan-id":   uint16(100),
		prefix + "/port":      uint32(1),
		prefix + "/type":      model.MACEntryTypeDynamic,
		prefix + "/permanent": false,
	} {
		_, err = r.Set(ctx, store.Running, path, value)
		require.NoError(t, err)
	}

	// Two sweeps: the first starts the aging clock, the second writes the age
	// leaf through SetState.
	fake := manager.Clock().(*clock.FakeClock)
	require.NoError(t, manager.Tick(ctx))
	fake.Advance(5 * time.Second)
	require.NoError(t, manager.Tick(ctx))

	// Candidate must hold the whole entry, configuration and state. An age leaf
	// without the entry's configuration is the orphan that used to make every
	// later commit fail validation.
	for _, suffix := range []string{"/vlan-id", "/port", "/type", "/permanent", "/age"} {
		_, err = st.Get(ctx, store.Candidate, prefix+suffix)
		require.NoError(t, err, "candidate must hold the whole entry: %s", suffix)
	}

	// The commit that used to answer invalid-value must succeed.
	require.NoError(t, st.Commit(ctx), "a running-created entry must not block a commit")

	entry, err := manager.MACEntry(ctx, mac)
	require.NoError(t, err, "the entry must survive the commit")
	assert.Equal(t, model.MACEntryTypeDynamic, entry.Type)
	assert.Equal(t, uint16(100), entry.VLAN)
	assert.Equal(t, uint32(5), entry.Age)

	// Removing it must not be undone by the next commit.
	require.NoError(t, manager.DeleteMAC(ctx, mac))
	require.NoError(t, st.Commit(ctx))

	_, err = manager.MACEntry(ctx, mac)
	assert.ErrorIs(t, err, ErrMACNotFound, "a deleted entry must not be resurrected by a commit")
}
