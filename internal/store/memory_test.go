package store

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMemory() *Memory {
	return NewMemory(Options{})
}

func newPersistentMemory(t *testing.T) (*Memory, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "startup.json")
	return NewMemory(Options{StartupFile: path}), path
}

// set is a test helper that fails the test when Set returns an error.
func set(t *testing.T, m *Memory, ds Datastore, path string, value any) {
	t.Helper()
	require.NoError(t, m.Set(context.Background(), ds, path, value))
}

func TestNewMemoryIsEmpty(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	for _, ds := range []Datastore{Running, Candidate, Startup} {
		_, err := m.Get(ctx, ds, "a/1")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrNotFound))

		paths, err := m.List(ctx, ds, "")
		require.NoError(t, err)
		assert.Empty(t, paths)
	}

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestMemorySetGet(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name  string
		value any
	}{
		{name: "bool", value: true},
		{name: "int", value: -5},
		{name: "uint8", value: uint8(7)},
		{name: "uint16", value: uint16(4094)},
		{name: "uint32", value: uint32(42)},
		{name: "uint64", value: uint64(1) << 40},
		{name: "float64", value: 3.5},
		{name: "string", value: "radio0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, ds := range []Datastore{Running, Candidate, Startup} {
				m := newMemory()
				set(t, m, ds, "radio-link/tx-power", tt.value)

				got, err := m.Get(ctx, ds, "radio-link/tx-power")
				require.NoError(t, err)
				assert.Equal(t, tt.value, got)
				assert.IsType(t, tt.value, got)
			}
		})
	}
}

func TestMemoryUnknownDatastore(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	_, err := m.Get(ctx, "shadow", "a/1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownDatastore))

	err = m.Set(ctx, "shadow", "a/1", 1)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownDatastore))

	err = m.Delete(ctx, "shadow", "a/1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownDatastore))

	_, err = m.List(ctx, "shadow", "")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnknownDatastore))
}

func TestMemorySetInvalidPath(t *testing.T) {
	ctx := context.Background()

	for _, path := range []string{"", "/a", "a/", "a//b", " a", "a "} {
		t.Run(path, func(t *testing.T) {
			m := newMemory()

			err := m.Set(ctx, Running, path, 1)

			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidPath))
		})
	}
}

func TestMemorySetInvalidValue(t *testing.T) {
	ctx := context.Background()

	for _, value := range []any{int64(1), int32(1), float32(1), nil, struct{}{}, []int{1}} {
		t.Run(fmt.Sprintf("%T", value), func(t *testing.T) {
			m := newMemory()

			err := m.Set(ctx, Running, "a/1", value)

			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrInvalidValue))
		})
	}
}

func TestMemoryDelete(t *testing.T) {
	ctx := context.Background()
	m := newMemory()
	set(t, m, Running, "a/1", 1)

	require.NoError(t, m.Delete(ctx, Running, "a/1"))

	_, err := m.Get(ctx, Running, "a/1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))

	err = m.Delete(ctx, Running, "a/1")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound))
}

// A write to running must also reach candidate: Commit makes running a copy of
// candidate, so a running-only write would be reverted by the next commit.
func TestMemorySetRunningMirrorsCandidate(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	set(t, m, Running, "a/1", 1)

	got, err := m.Get(ctx, Candidate, "a/1")
	require.NoError(t, err)
	assert.Equal(t, 1, got)

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes, "running and candidate must agree")
}

// A write to candidate is a pending edit and must leave running untouched.
func TestMemorySetCandidateDoesNotTouchRunning(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	set(t, m, Candidate, "a/1", 1)

	_, err := m.Get(ctx, Running, "a/1")
	assert.True(t, errors.Is(err, ErrNotFound))

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Change{{Op: OpCreate, Path: "a/1", New: 1}}, changes)
}

// Deleting from running must delete from candidate too, or a commit would
// resurrect the leaves of a removed list entry.
func TestMemoryDeleteRunningMirrorsCandidate(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	set(t, m, Running, "a/1", 1)
	require.NoError(t, m.Delete(ctx, Running, "a/1"))

	for _, ds := range []Datastore{Running, Candidate} {
		_, err := m.Get(ctx, ds, "a/1")
		assert.True(t, errors.Is(err, ErrNotFound), "%s must not keep the deleted leaf", ds)
	}

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

// Deleting a pending candidate edit must leave the running value alone.
func TestMemoryDeleteCandidateKeepsRunning(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	set(t, m, Running, "a/1", 1)
	require.NoError(t, m.Delete(ctx, Candidate, "a/1"))

	got, err := m.Get(ctx, Running, "a/1")
	require.NoError(t, err)
	assert.Equal(t, 1, got)
}

// A running write made after the last commit must survive the next commit,
// which is the P1 regression: a plane that writes running directly (SNMP SET, a
// NETCONF or RESTCONF edit targeting running, a domain) must not be reverted.
func TestMemoryCommitKeepsRunningWrite(t *testing.T) {
	ctx := context.Background()
	m := newMemory()
	set(t, m, Running, "a/1", 1)
	require.NoError(t, m.Commit(ctx))

	set(t, m, Running, "a/2", 2)
	require.NoError(t, m.Commit(ctx))

	got, err := m.Get(ctx, Running, "a/2")
	require.NoError(t, err)
	assert.Equal(t, 2, got)
}

// Apply must validate the whole batch before it writes anything, so one bad
// entry leaves the datastore untouched.
func TestMemoryApplyIsAtomic(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name      string
		values    map[string]any
		deletions []string
	}{
		{
			name:   "invalid path",
			values: map[string]any{"a/1": 1, "/bad": 2},
		},
		{
			name:   "invalid value",
			values: map[string]any{"a/1": 1, "a/2": int64(2)},
		},
		{
			name:      "invalid deletion path",
			values:    map[string]any{"a/1": 1},
			deletions: []string{"a//b"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newMemory()
			set(t, m, Running, "existing/1", 7)

			err := m.Apply(ctx, Running, tt.values, tt.deletions)
			require.Error(t, err)

			for _, ds := range []Datastore{Running, Candidate} {
				got, getErr := m.Get(ctx, ds, "existing/1")
				require.NoError(t, getErr, "%s must be untouched", ds)
				assert.Equal(t, 7, got)

				_, getErr = m.Get(ctx, ds, "a/1")
				assert.True(t, errors.Is(getErr, ErrNotFound), "%s must not hold a rejected entry", ds)
			}
		})
	}
}

// Apply mirrors a batch targeting running into candidate, exactly like Set.
func TestMemoryApplyMirrorsRunning(t *testing.T) {
	ctx := context.Background()
	m := newMemory()
	set(t, m, Running, "a/9", 9)

	require.NoError(t, m.Apply(ctx, Running, map[string]any{"a/1": 1, "a/2": 2}, []string{"a/9"}))

	for _, ds := range []Datastore{Running, Candidate} {
		got, err := m.Get(ctx, ds, "a/1")
		require.NoError(t, err)
		assert.Equal(t, 1, got)

		_, err = m.Get(ctx, ds, "a/9")
		assert.True(t, errors.Is(err, ErrNotFound), "%s must not keep the deleted leaf", ds)
	}

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

// A deletion of a path that is already gone is not an error: removing the
// leaves of a subtree uses Apply.
func TestMemoryApplyToleratesMissingDeletions(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	require.NoError(t, m.Apply(ctx, Running, nil, []string{"a/1"}))

	_, err := m.Get(ctx, Running, "a/1")
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestMemoryList(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	for _, path := range []string{"b/2", "a/1", "a/2", "a", "ab/1"} {
		set(t, m, Running, path, 1)
	}

	tests := []struct {
		name   string
		prefix string
		want   []string
	}{
		{name: "empty prefix lists everything", prefix: "", want: []string{"a", "a/1", "a/2", "ab/1", "b/2"}},
		{name: "prefix segment", prefix: "a", want: []string{"a/1", "a/2"}},
		{name: "leaf prefix has no descendants", prefix: "a/1", want: []string{}},
		{name: "unrelated prefix", prefix: "b", want: []string{"b/2"}},
		{name: "no match", prefix: "c", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, err := m.List(ctx, Running, tt.prefix)

			require.NoError(t, err)
			assert.Equal(t, tt.want, paths)
		})
	}
}

func TestMemoryContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	m := newMemory()

	_, err := m.Get(ctx, Running, "a/1")
	assert.True(t, errors.Is(err, context.Canceled))

	err = m.Set(ctx, Running, "a/1", 1)
	assert.True(t, errors.Is(err, context.Canceled))

	err = m.Delete(ctx, Running, "a/1")
	assert.True(t, errors.Is(err, context.Canceled))

	_, err = m.List(ctx, Running, "")
	assert.True(t, errors.Is(err, context.Canceled))

	_, err = m.Diff(ctx)
	assert.True(t, errors.Is(err, context.Canceled))

	err = m.Commit(ctx)
	assert.True(t, errors.Is(err, context.Canceled))

	err = m.Rollback(ctx)
	assert.True(t, errors.Is(err, context.Canceled))

	err = m.LoadStartup(ctx)
	assert.True(t, errors.Is(err, context.Canceled))

	_, err = m.Snapshot(ctx)
	assert.True(t, errors.Is(err, context.Canceled))

	err = m.Restore(ctx, map[string]any{"a/1": 1})
	assert.True(t, errors.Is(err, context.Canceled))
}

func TestMemorySnapshotIsDetached(t *testing.T) {
	ctx := context.Background()
	m := newMemory()
	set(t, m, Running, "a/1", 1)
	set(t, m, Running, "a/2", 2)

	snapshot, err := m.Snapshot(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a/1": 1, "a/2": 2}, snapshot)

	// Later writes must not leak into the snapshot.
	set(t, m, Running, "a/1", 99)
	require.NoError(t, m.Delete(ctx, Running, "a/2"))
	set(t, m, Running, "a/3", 3)

	assert.Equal(t, map[string]any{"a/1": 1, "a/2": 2}, snapshot)
}

func TestMemorySnapshotOfEmptyStore(t *testing.T) {
	ctx := context.Background()

	snapshot, err := newMemory().Snapshot(ctx)

	require.NoError(t, err)
	assert.Empty(t, snapshot)
}

func TestMemoryRestoreReplacesRunningAndStartup(t *testing.T) {
	ctx := context.Background()
	m := newMemory()
	set(t, m, Running, "a/1", 1)
	snapshot, err := m.Snapshot(ctx)
	require.NoError(t, err)

	// Commit a new configuration, then keep uncommitted candidate changes.
	set(t, m, Candidate, "a/1", 2)
	require.NoError(t, m.Commit(ctx))
	set(t, m, Candidate, "a/1", 3)
	set(t, m, Candidate, "a/9", 9)

	require.NoError(t, m.Restore(ctx, snapshot))

	got, err := m.Get(ctx, Running, "a/1")
	require.NoError(t, err)
	assert.Equal(t, 1, got)

	got, err = m.Get(ctx, Startup, "a/1")
	require.NoError(t, err)
	assert.Equal(t, 1, got)

	// Candidate is untouched: the uncommitted configuration survives.
	got, err = m.Get(ctx, Candidate, "a/1")
	require.NoError(t, err)
	assert.Equal(t, 3, got)

	got, err = m.Get(ctx, Candidate, "a/9")
	require.NoError(t, err)
	assert.Equal(t, 9, got)

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Change{
		{Op: OpUpdate, Path: "a/1", Old: 1, New: 3},
		{Op: OpCreate, Path: "a/9", New: 9},
	}, changes)
}

func TestMemoryRestorePersistsStartup(t *testing.T) {
	ctx := context.Background()
	m, path := newPersistentMemory(t)
	set(t, m, Running, "a/1", uint32(42))

	snapshot, err := m.Snapshot(ctx)
	require.NoError(t, err)

	set(t, m, Candidate, "a/1", uint32(7))
	require.NoError(t, m.Commit(ctx))
	require.NoError(t, m.Restore(ctx, snapshot))

	values, err := Load(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a/1": uint32(42)}, values)

	got, err := m.Get(ctx, Running, "a/1")
	require.NoError(t, err)
	assert.Equal(t, uint32(42), got)
}

func TestMemoryRestorePersistFailureLeavesRunning(t *testing.T) {
	ctx := context.Background()
	// The startup path is a directory, so the atomic write must fail before
	// running is swapped.
	m := NewMemory(Options{StartupFile: t.TempDir()})
	set(t, m, Running, "a/1", 2)
	snapshot, err := m.Snapshot(ctx)
	require.NoError(t, err)

	set(t, m, Running, "a/1", 1)
	err = m.Restore(ctx, snapshot)

	require.Error(t, err)

	got, getErr := m.Get(ctx, Running, "a/1")
	require.NoError(t, getErr)
	assert.Equal(t, 1, got, "running must stay untouched")
}

func TestMemoryRollback(t *testing.T) {
	ctx := context.Background()
	m := newMemory()
	set(t, m, Running, "a/1", 1)
	set(t, m, Candidate, "a/1", 2)
	set(t, m, Candidate, "a/2", 3)

	require.NoError(t, m.Rollback(ctx))

	got, err := m.Get(ctx, Candidate, "a/1")
	require.NoError(t, err)
	assert.Equal(t, 1, got)

	_, err = m.Get(ctx, Candidate, "a/2")
	assert.True(t, errors.Is(err, ErrNotFound))

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestMemoryCommitAppliesCandidate(t *testing.T) {
	ctx := context.Background()
	m := newMemory()
	set(t, m, Running, "a/1", 1)
	set(t, m, Running, "a/2", 2)
	require.NoError(t, m.Rollback(ctx))

	set(t, m, Candidate, "a/1", 10)
	require.NoError(t, m.Delete(ctx, Candidate, "a/2"))
	set(t, m, Candidate, "a/3", 30)

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Equal(t, []Change{
		{Op: OpUpdate, Path: "a/1", Old: 1, New: 10},
		{Op: OpDelete, Path: "a/2", Old: 2},
		{Op: OpCreate, Path: "a/3", New: 30},
	}, changes)

	require.NoError(t, m.Commit(ctx))

	for _, ds := range []Datastore{Running, Candidate, Startup} {
		got, err := m.Get(ctx, ds, "a/1")
		require.NoError(t, err)
		assert.Equal(t, 10, got)

		_, err = m.Get(ctx, ds, "a/2")
		assert.True(t, errors.Is(err, ErrNotFound))

		got, err = m.Get(ctx, ds, "a/3")
		require.NoError(t, err)
		assert.Equal(t, 30, got)
	}

	changes, err = m.Diff(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestMemoryCommitNoChanges(t *testing.T) {
	ctx := context.Background()
	m := newMemory()
	set(t, m, Running, "a/1", 1)
	require.NoError(t, m.Rollback(ctx))

	require.NoError(t, m.Commit(ctx))

	got, err := m.Get(ctx, Running, "a/1")
	require.NoError(t, err)
	assert.Equal(t, 1, got)
}

func TestMemoryCommitValidatorRejects(t *testing.T) {
	ctx := context.Background()
	sentinel := errors.New("tx-power out of range")
	m := NewMemory(Options{Validator: func(context.Context, map[string]any) error { return sentinel }})
	set(t, m, Running, "a/1", 1)
	require.NoError(t, m.Rollback(ctx))
	set(t, m, Candidate, "a/1", 2)

	err := m.Commit(ctx)

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrValidation))
	assert.True(t, errors.Is(err, sentinel))

	got, getErr := m.Get(ctx, Running, "a/1")
	require.NoError(t, getErr)
	assert.Equal(t, 1, got, "running must stay untouched")

	got, getErr = m.Get(ctx, Candidate, "a/1")
	require.NoError(t, getErr)
	assert.Equal(t, 2, got, "candidate must stay untouched")
}

func TestMemoryCommitValidatorGetsCopy(t *testing.T) {
	ctx := context.Background()
	m := NewMemory(Options{Validator: func(_ context.Context, values map[string]any) error {
		values["a/1"] = 999
		return nil
	}})
	set(t, m, Candidate, "a/1", 7)

	require.NoError(t, m.Commit(ctx))

	got, err := m.Get(ctx, Running, "a/1")
	require.NoError(t, err)
	assert.Equal(t, 7, got)
}

func TestMemoryCommitPersistsStartup(t *testing.T) {
	ctx := context.Background()
	m, path := newPersistentMemory(t)
	set(t, m, Candidate, "a/1", uint32(42))

	require.NoError(t, m.Commit(ctx))

	values, err := Load(ctx, path)
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"a/1": uint32(42)}, values)
}

func TestMemoryCommitPersistFailureLeavesRunning(t *testing.T) {
	ctx := context.Background()
	// The startup path is a directory, so the atomic write must fail before
	// running is swapped.
	m := NewMemory(Options{StartupFile: t.TempDir()})
	set(t, m, Candidate, "a/1", 2)

	err := m.Commit(ctx)

	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrValidation))

	_, getErr := m.Get(ctx, Running, "a/1")
	assert.True(t, errors.Is(getErr, ErrNotFound))

	_, getErr = m.Get(ctx, Candidate, "a/1")
	assert.NoError(t, getErr)
}

func TestMemoryLoadStartup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "startup.json")
	require.NoError(t, Save(ctx, path, map[string]any{"a/1": 1, "a/2": "two"}))

	m := NewMemory(Options{StartupFile: path})
	require.NoError(t, m.LoadStartup(ctx))

	for _, ds := range []Datastore{Running, Candidate, Startup} {
		got, err := m.Get(ctx, ds, "a/2")
		require.NoError(t, err)
		assert.Equal(t, "two", got)
	}

	changes, err := m.Diff(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes)
}

func TestMemoryLoadStartupMissingFileIsNoop(t *testing.T) {
	ctx := context.Background()
	m, _ := newPersistentMemory(t)

	require.NoError(t, m.LoadStartup(ctx))

	_, err := m.Get(ctx, Running, "a/1")
	assert.True(t, errors.Is(err, ErrNotFound))
}

func TestMemoryLoadStartupDisabledIsNoop(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	require.NoError(t, m.LoadStartup(ctx))
}

func TestMemoryConcurrentAccess(t *testing.T) {
	ctx := context.Background()
	m := newMemory()

	const workers = 8
	const perWorker = 50

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < perWorker; i++ {
				path := "a/" + string(rune('a'+worker))
				assert.NoError(t, m.Set(ctx, Running, path, worker*perWorker+i))
				_, _ = m.Get(ctx, Running, path)
				_, _ = m.List(ctx, Running, "a")
			}
		}(w)
	}
	wg.Wait()

	paths, err := m.List(ctx, Running, "a")
	require.NoError(t, err)
	assert.Len(t, paths, workers)
}
