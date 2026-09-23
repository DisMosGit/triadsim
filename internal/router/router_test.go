package router

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/store"
)

const (
	radioPowerPath = "interfaces/interface[name=radio0]/radio-link/tx-power"
	radioPowerOID  = "1.3.6.1.4.1.99999.1.1.4.0"
	ifDescrOID     = "1.3.6.1.2.1.2.2.1.2"
)

// newTestRouter returns a router over a Memory store seeded with DefaultDevice.
func newTestRouter(t *testing.T) (*Router, *store.Memory) {
	t.Helper()
	ctx := context.Background()

	st := store.NewMemory(store.Options{})
	r, err := New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	require.NoError(t, r.Seed(ctx, store.Running))
	require.NoError(t, st.Rollback(ctx))
	return r, st
}

func TestNewRejectsNilInputs(t *testing.T) {
	_, err := New(nil, store.NewMemory(store.Options{}))
	assert.Error(t, err)

	_, err = New(model.DefaultDevice(), nil)
	assert.Error(t, err)
}

func TestGetByPathAndOID(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	// The Phase 1.6 DoD: the same node is reachable by path and by OID.
	result, err := r.Get(ctx, store.Running, radioPowerPath)
	require.NoError(t, err)
	assert.Equal(t, 20.0, result.Value)
	assert.Equal(t, radioPowerOID, result.OID)
	assert.True(t, result.Writable)

	oid, ok := r.OIDForPath(radioPowerPath)
	require.True(t, ok)
	assert.Equal(t, radioPowerOID, oid)

	path, ok := r.PathForOID(radioPowerOID)
	require.True(t, ok)
	assert.Equal(t, radioPowerPath, path.String())
}

func TestGetReadOnlyNode(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	result, err := r.Get(ctx, store.Running, "interfaces/interface[name=radio0]/radio-link/rssi")
	require.NoError(t, err)
	assert.Equal(t, -72.5, result.Value)
	assert.False(t, result.Writable)
}

func TestGetNotFound(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	tests := []struct {
		name string
		path string
	}{
		{name: "unknown interface", path: "interfaces/interface[name=nope]/name"},
		{name: "missing radio link", path: "interfaces/interface[name=eth0]/radio-link/rssi"},
		{name: "unknown leaf", path: "interfaces/interface[name=radio0]/radio-link/nope"},
		{name: "container is not a leaf", path: "interfaces/interface[name=radio0]/radio-link"},
		{name: "root is not a leaf", path: "interfaces"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Get(ctx, store.Running, tt.path)

			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrNotFound) || errors.Is(err, ErrInvalidPath))
		})
	}
}

func TestResolveRequiresListPredicate(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	_, err := r.Get(ctx, store.Running, "interfaces/interface/name")

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestSetAndDelete(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)

	result, err := r.Set(ctx, store.Candidate, radioPowerPath, 25.5)
	require.NoError(t, err)
	assert.Equal(t, 25.5, result.Value)

	stored, err := st.Get(ctx, store.Candidate, radioPowerPath)
	require.NoError(t, err)
	assert.Equal(t, 25.5, stored)

	// The running datastore is untouched by a candidate write.
	stored, err = st.Get(ctx, store.Running, radioPowerPath)
	require.NoError(t, err)
	assert.Equal(t, 20.0, stored)

	require.NoError(t, r.Delete(ctx, store.Candidate, radioPowerPath))

	_, err = st.Get(ctx, store.Candidate, radioPowerPath)
	assert.True(t, errors.Is(err, store.ErrNotFound))
}

func TestSetRejectsReadOnly(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	_, err := r.Set(ctx, store.Running, "interfaces/interface[name=radio0]/radio-link/rssi", -50.0)

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReadOnly)
}

func TestSetRejectsMismatchedValue(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	tests := []struct {
		name  string
		path  string
		value any
		want  error
	}{
		{name: "string for float", path: radioPowerPath, value: "high", want: ErrTypeMismatch},
		{name: "overflow for uint32", path: "interfaces/interface[name=eth0]/mtu", value: uint64(1) << 40, want: ErrBadValue},
		{name: "negative for uint32", path: "interfaces/interface[name=eth0]/mtu", value: -1, want: ErrBadValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Set(ctx, store.Running, tt.path, tt.value)

			require.Error(t, err)
			assert.ErrorIs(t, err, tt.want)
		})
	}
}

func TestSetConvertsCompatibleValues(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	// An SNMP Integer arrives as int and becomes the model's uint8.
	result, err := r.Set(ctx, store.Running, "interfaces/interface[name=radio0]/radio-link/acm/min-profile", 2)
	require.NoError(t, err)
	assert.Equal(t, uint8(2), result.Value)
}

func TestList(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	results, err := r.List(ctx, store.Running, "interfaces/interface[name=radio0]/radio-link")
	require.NoError(t, err)
	require.NotEmpty(t, results)

	paths := make([]string, 0, len(results))
	for _, result := range results {
		paths = append(paths, result.Path)
	}
	assert.Contains(t, paths, radioPowerPath)
	assert.Contains(t, paths, "interfaces/interface[name=radio0]/radio-link/rssi")
}

func TestDispatch(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	result, err := r.Dispatch(ctx, Op{Kind: OpGet, Datastore: store.Running, Path: radioPowerPath})
	require.NoError(t, err)
	assert.Equal(t, 20.0, result.Value)

	result, err = r.Dispatch(ctx, Op{Kind: OpSet, Datastore: store.Candidate, Path: radioPowerPath, Value: 21.0})
	require.NoError(t, err)
	assert.Equal(t, 21.0, result.Value)

	result, err = r.Dispatch(ctx, Op{Kind: OpList, Datastore: store.Candidate, Path: "interfaces/interface[name=radio0]/radio-link"})
	require.NoError(t, err)
	assert.NotNil(t, result.Value)

	_, err = r.Dispatch(ctx, Op{Kind: "bogus", Datastore: store.Running, Path: radioPowerPath})
	assert.ErrorIs(t, err, ErrInvalidPath)
}

func TestBindings(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	bindings, err := r.Bindings(ctx, store.Running)
	require.NoError(t, err)

	byOID := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		byOID[binding.OID] = binding
	}

	ifDescr, ok := byOID[ifDescrOID+".1"]
	require.True(t, ok)
	assert.Equal(t, "radio0", ifDescr.Value)

	assert.Equal(t, "eth0", byOID[ifDescrOID+".2"].Value)
	assert.Equal(t, "eth1", byOID[ifDescrOID+".3"].Value)
	assert.Equal(t, 1, byOID["1.3.6.1.2.1.2.2.1.1.1"].Value)
	assert.Equal(t, 1, byOID["1.3.6.1.2.1.2.2.1.3.1"].Value)
	assert.Equal(t, 6, byOID["1.3.6.1.2.1.2.2.1.3.2"].Value)
	assert.Equal(t, 1, byOID["1.3.6.1.2.1.2.2.1.8.1"].Value)
	assert.Equal(t, EnterpriseOID+".1", byOID[sysObjectIDOID].Value)
	assert.Equal(t, -72.5, byOID[EnterpriseOID+".1.1.1.0"].Value)
	assert.Equal(t, uint32(112), byOID[EnterpriseOID+".1.1.3.0"].Value)

	power, ok := byOID[radioPowerOID]
	require.True(t, ok)
	assert.Equal(t, 20.0, power.Value)
	assert.True(t, power.Writable)

	// Bindings are sorted by numeric OID.
	for i := 1; i < len(bindings); i++ {
		assert.Equal(t, -1, CompareOID(bindings[i-1].OID, bindings[i].OID))
	}
}

func TestSeedAndCommitValidates(t *testing.T) {
	ctx := context.Background()
	st := store.NewMemory(store.Options{})
	r, err := New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)

	require.NoError(t, r.Seed(ctx, store.Candidate))
	require.NoError(t, st.Commit(ctx))

	stored, err := st.Get(ctx, store.Running, radioPowerPath)
	require.NoError(t, err)
	assert.Equal(t, 20.0, stored)

	changes, err := st.Diff(ctx)
	require.NoError(t, err)
	assert.Empty(t, changes)

	// A write that the model rejects must abort the commit.
	_, err = r.Set(ctx, store.Candidate, radioPowerPath, 999.0)
	require.NoError(t, err)

	err = st.Commit(ctx)
	require.Error(t, err)
	assert.ErrorIs(t, err, store.ErrValidation)

	stored, err = st.Get(ctx, store.Running, radioPowerPath)
	require.NoError(t, err)
	assert.Equal(t, 20.0, stored, "running must be unchanged after a rejected commit")
}

func TestValidate(t *testing.T) {
	ctx := context.Background()
	r, st := newTestRouter(t)

	snapshot, err := snapshotDatastore(ctx, st, store.Running)
	require.NoError(t, err)
	require.NoError(t, r.Validate(ctx, snapshot), "the seeded configuration must be valid")

	snapshot[radioPowerPath] = 999.0
	err = r.Validate(ctx, snapshot)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tx-power")

	snapshot[radioPowerPath] = 20.0
	snapshot["interfaces/interface[name=nope]/name"] = "x"
	assert.Error(t, r.Validate(ctx, snapshot))
}

func TestValidateCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r, _ := newTestRouter(t)
	assert.ErrorIs(t, r.Validate(ctx, nil), context.Canceled)
}

// snapshotDatastore copies a datastore into a flat map, as a commit validator
// receives it.
func snapshotDatastore(ctx context.Context, st store.Store, ds store.Datastore) (map[string]any, error) {
	paths, err := st.List(ctx, ds, "")
	if err != nil {
		return nil, err
	}
	values := make(map[string]any, len(paths))
	for _, path := range paths {
		value, err := st.Get(ctx, ds, path)
		if err != nil {
			return nil, err
		}
		values[path] = value
	}
	return values, nil
}

// The SNMP object index is memoized per datastore generation (one request
// consults it several times), so a write of any kind — state included — must
// invalidate it: the next Bindings call has to reflect the new value.
func TestBindingsFollowStoreWrites(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	before, err := r.Bindings(ctx, store.Running)
	require.NoError(t, err)
	assert.Equal(t, "triadsim-01", bindingValue(t, before, "1.3.6.1.2.1.1.5.0"))

	_, err = r.Set(ctx, store.Running, "system-info/name", "renamed")
	require.NoError(t, err)

	after, err := r.Bindings(ctx, store.Running)
	require.NoError(t, err)
	assert.Equal(t, "renamed", bindingValue(t, after, "1.3.6.1.2.1.1.5.0"))

	// A state write invalidates the cache just as well: uptime is exposed.
	_, err = r.SetState(ctx, "system-info/uptime", uint32(30))
	require.NoError(t, err)

	after, err = r.Bindings(ctx, store.Running)
	require.NoError(t, err)
	assert.Equal(t, uint32(3000), bindingValue(t, after, "1.3.6.1.2.1.1.3.0"), "30s in TimeTicks")
}

// bindingValue returns the value of the binding with the given OID.
func bindingValue(t *testing.T, bindings []Binding, oid string) any {
	t.Helper()
	for _, binding := range bindings {
		if binding.OID == oid {
			return binding.Value
		}
	}
	t.Fatalf("no binding for OID %s", oid)
	return nil
}
