package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
)

// startup is a state filter for tests: uptime and MAC age are volatile leaves
// (the model tags them config:"false").
func testStateFilter(path string) bool {
	return path == "system-info/uptime" || filepath.Base(path) == "age"
}

// The persisted startup document holds configuration only: a state leaf
// (uptime, a counter, a MAC entry's age) is volatile and must not reappear
// stale after a restart.
func TestCommitPersistsConfigurationOnly(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "startup.json")
	m := NewMemory(Options{StartupFile: file, StateFilter: testStateFilter})

	require.NoError(t, m.Set(ctx, Running, "system-info/name", "triadsim-02"))
	require.NoError(t, m.Set(ctx, Running, "system-info/uptime", uint32(7)))
	require.NoError(t, m.Set(ctx, Running, "mac-table/entry[mac-address=02:00:00:00:00:aa]/age", uint32(5)))
	require.NoError(t, m.Commit(ctx))

	persisted, err := Load(ctx, file)
	require.NoError(t, err)
	assert.Equal(t, "triadsim-02", persisted["system-info/name"])
	assert.NotContains(t, persisted, "system-info/uptime", "a state leaf must never be persisted")
	assert.NotContains(t, persisted, "mac-table/entry[mac-address=02:00:00:00:00:aa]/age")

	// Restore persists the same way.
	require.NoError(t, m.Restore(ctx, map[string]any{
		"system-info/name":   "triadsim-03",
		"system-info/uptime": uint32(9),
	}))
	persisted, err = Load(ctx, file)
	require.NoError(t, err)
	assert.Equal(t, "triadsim-03", persisted["system-info/name"])
	assert.NotContains(t, persisted, "system-info/uptime")
}

// LoadStartup treats the startup file as untrusted operator input: the
// validator runs before anything is installed and reports the offending path,
// and a state leaf a document written by an older version still carries is
// dropped instead of being restored stale.
func TestLoadStartupValidatesAndDropsState(t *testing.T) {
	ctx := context.Background()
	file := filepath.Join(t.TempDir(), "startup.json")
	validate := func(_ context.Context, values map[string]any) error {
		if _, ok := values["bad/path"]; ok {
			return errors.New("bad/path: not a model leaf")
		}
		return nil
	}

	require.NoError(t, Save(ctx, file, map[string]any{
		"system-info/name":   "triadsim-02",
		"system-info/uptime": uint32(7),
		"bad/path":           "boom",
	}))

	m := NewMemory(Options{StartupFile: file, StateFilter: testStateFilter, Validator: validate})
	err := m.LoadStartup(ctx)
	require.Error(t, err, "a hand-edited startup file must be rejected at the boundary")
	assert.ErrorContains(t, err, "invalid startup file")
	assert.ErrorContains(t, err, "bad/path")

	// Nothing is installed when the file is rejected.
	_, err = m.Get(ctx, Running, "system-info/name")
	assert.ErrorIs(t, err, ErrNotFound)

	// A valid file loads, minus its state leaves.
	require.NoError(t, Save(ctx, file, map[string]any{
		"system-info/name":   "triadsim-02",
		"system-info/uptime": uint32(7),
	}))
	m = NewMemory(Options{StartupFile: file, StateFilter: testStateFilter, Validator: validate})
	require.NoError(t, m.LoadStartup(ctx))

	value, err := m.Get(ctx, Running, "system-info/name")
	require.NoError(t, err)
	assert.Equal(t, "triadsim-02", value)
	_, err = m.Get(ctx, Running, "system-info/uptime")
	assert.ErrorIs(t, err, ErrNotFound, "a stale state leaf must not be restored at boot")

	// A missing file is still a no-op.
	m = NewMemory(Options{StartupFile: filepath.Join(t.TempDir(), "absent.json"), Validator: validate})
	assert.NoError(t, m.LoadStartup(ctx))
}

// Generation tracks every write so cached derived views know they are stale;
// ConfigChangedAt tracks configuration only, because RFC 8040 §3.4.1.1
// forbids the RESTCONF validators from moving on state-data writes.
func TestChangeTrackingIgnoresStateWrites(t *testing.T) {
	ctx := context.Background()
	fake := clock.NewFakeClock()
	m := NewMemory(Options{StateFilter: testStateFilter, Clock: fake})

	changed, err := m.ConfigChangedAt(ctx, Running)
	require.NoError(t, err)
	assert.True(t, changed.IsZero())

	generation, err := m.Generation(ctx, Running)
	require.NoError(t, err)

	require.NoError(t, m.Set(ctx, Running, "system-info/uptime", uint32(1)))

	changed, err = m.ConfigChangedAt(ctx, Running)
	require.NoError(t, err)
	assert.True(t, changed.IsZero(), "a state write is not a configuration change")
	afterState, err := m.Generation(ctx, Running)
	require.NoError(t, err)
	assert.Greater(t, afterState, generation, "a state write must still invalidate caches")

	fake.Advance(time.Second)
	require.NoError(t, m.Set(ctx, Running, "system-info/name", "triadsim-02"))

	changed, err = m.ConfigChangedAt(ctx, Running)
	require.NoError(t, err)
	assert.False(t, changed.IsZero(), "a configuration write moves the timestamp")
}
