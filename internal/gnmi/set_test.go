package gnmi

import (
	"context"
	"testing"
	"time"

	"github.com/openconfig/gnmi/proto/gnmi"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/DisMosGit/triadsim/internal/event"
	"github.com/DisMosGit/triadsim/internal/store"
)

// stringValue builds a string-typed gNMI value.
func stringValue(text string) *gnmi.TypedValue {
	return &gnmi.TypedValue{Value: &gnmi.TypedValue_StringVal{StringVal: text}}
}

// uintValue builds an unsigned-typed gNMI value.
func uintValue(number uint64) *gnmi.TypedValue {
	return &gnmi.TypedValue{Value: &gnmi.TypedValue_UintVal{UintVal: number}}
}

// getString reads one leaf of the running datastore.
func getString(ctx context.Context, t *testing.T, st *store.Memory, path string) string {
	t.Helper()

	value, err := st.Get(ctx, store.Running, path)
	require.NoError(t, err)
	text, ok := value.(string)
	require.True(t, ok, "%s is a string leaf, got %T", path, value)
	return text
}

func TestSetUpdateLeafChangesTheStore(t *testing.T) {
	ts := newTestServer(t, true)
	ctx := context.Background()

	events := ts.bus.Subscribe()
	defer ts.bus.Unsubscribe(events)

	response, err := ts.client.Set(ctx, &gnmi.SetRequest{
		Update: []*gnmi.Update{{
			Path: path(elem("system-info"), elem("location")),
			Val:  stringValue("field-site-7"),
		}},
	})
	require.NoError(t, err)
	require.Len(t, response.GetResponse(), 1)
	assert.Equal(t, gnmi.UpdateResult_UPDATE, response.GetResponse()[0].GetOp())
	assert.Equal(t, "system-info/location", relativeString(response.GetResponse()[0].GetPath()))

	assert.Equal(t, "field-site-7", getString(ctx, t, ts.store, "system-info/location"))

	select {
	case busEvent := <-events:
		assert.Equal(t, event.TypeConfigChanged, busEvent.Type)
		assert.Equal(t, "device", busEvent.Resource)
	case <-time.After(2 * time.Second):
		require.Fail(t, "a successful Set must publish ConfigChanged")
	}
}

func TestSetReplaceAndDeleteLeaf(t *testing.T) {
	ts := newTestServer(t, false)
	ctx := context.Background()
	target := path(elem("system-info"), elem("contact"))

	_, err := ts.client.Set(ctx, &gnmi.SetRequest{
		Replace: []*gnmi.Update{{Path: target, Val: stringValue("noc@triadsim.test")}},
	})
	require.NoError(t, err)
	assert.Equal(t, "noc@triadsim.test", getString(ctx, t, ts.store, "system-info/contact"))

	response, err := ts.client.Set(ctx, &gnmi.SetRequest{Delete: []*gnmi.Path{target}})
	require.NoError(t, err)
	require.Len(t, response.GetResponse(), 1)
	assert.Equal(t, gnmi.UpdateResult_DELETE, response.GetResponse()[0].GetOp())

	_, err = ts.store.Get(ctx, store.Running, "system-info/contact")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestSetDeleteListEntry(t *testing.T) {
	ts := newTestServer(t, false)
	ctx := context.Background()

	// The seeded MAC entry is at vlan 100, port 3.
	_, err := ts.client.Set(ctx, &gnmi.SetRequest{
		Delete: []*gnmi.Path{path(elem("mac-table"),
			keyed("entry", "mac-address", "02:00:00:00:00:02"))},
	})
	require.NoError(t, err)

	_, err = ts.store.Get(ctx, store.Running, "mac-table/entry[mac-address=02:00:00:00:00:02]/vlan-id")
	assert.ErrorIs(t, err, store.ErrNotFound)
}

func TestSetIsAtomic(t *testing.T) {
	ts := newTestServer(t, false)
	ctx := context.Background()
	before := getString(ctx, t, ts.store, "system-info/location")

	// The second update violates the PTP domain range, so the first must not be
	// written either.
	_, err := ts.client.Set(ctx, &gnmi.SetRequest{
		Update: []*gnmi.Update{
			{Path: path(elem("system-info"), elem("location")), Val: stringValue("field-site-7")},
			{Path: path(elem("ptp"), elem("clock"), elem("domain")), Val: uintValue(200)},
		},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
	assert.Equal(t, before, getString(ctx, t, ts.store, "system-info/location"))
}

func TestSetRejectsSubtreeValue(t *testing.T) {
	ts := newTestServer(t, false)

	_, err := ts.client.Set(context.Background(), &gnmi.SetRequest{
		Update: []*gnmi.Update{{
			Path: path(elem("ptp"), elem("clock")),
			Val:  stringValue("locked"),
		}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSetRejectsListEntryValue(t *testing.T) {
	ts := newTestServer(t, false)

	_, err := ts.client.Set(context.Background(), &gnmi.SetRequest{
		Update: []*gnmi.Update{{
			Path: path(elem("vlans"), keyed("vlan", "id", "100")),
			Val:  stringValue("VOICE"),
		}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSetRejectsUnknownPath(t *testing.T) {
	ts := newTestServer(t, false)

	_, err := ts.client.Set(context.Background(), &gnmi.SetRequest{
		Update: []*gnmi.Update{{
			Path: path(elem("no-such-container"), elem("leaf")),
			Val:  stringValue("x"),
		}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestSetRejectsReadOnlyLeaf(t *testing.T) {
	ts := newTestServer(t, false)

	_, err := ts.client.Set(context.Background(), &gnmi.SetRequest{
		Update: []*gnmi.Update{{
			Path: path(elem("ptp"), elem("clock"), elem("state")),
			Val:  stringValue("freerun"),
		}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestSetRejectsUnionReplace(t *testing.T) {
	ts := newTestServer(t, false)

	_, err := ts.client.Set(context.Background(), &gnmi.SetRequest{
		UnionReplace: []*gnmi.Update{{
			Path: path(elem("system-info"), elem("location")),
			Val:  stringValue("x"),
		}},
	})
	require.Error(t, err)
	assert.Equal(t, codes.Unimplemented, status.Code(err))
}

func TestSetWithoutOperationsIsANoOp(t *testing.T) {
	ts := newTestServer(t, true)
	ctx := context.Background()

	events := ts.bus.Subscribe()
	defer ts.bus.Unsubscribe(events)

	response, err := ts.client.Set(ctx, &gnmi.SetRequest{})
	require.NoError(t, err)
	assert.Empty(t, response.GetResponse())

	select {
	case busEvent := <-events:
		require.Failf(t, "a no-op Set must not publish", "got %s", busEvent.Type)
	default:
	}
}
