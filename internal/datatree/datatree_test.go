package datatree_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// newTestTree returns a router and store seeded with DefaultDevice.
func newTestTree(t *testing.T) (*router.Router, *store.Memory) {
	t.Helper()
	ctx := context.Background()

	st := store.NewMemory(store.Options{})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	require.NoError(t, r.Seed(ctx, store.Running))
	require.NoError(t, st.Rollback(ctx))
	return r, st
}

func TestReadConfigExcludesState(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestTree(t)

	root, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{})
	require.NoError(t, err)

	radio := findEntry(t, findChild(t, root, "interfaces", "interface"), "radio0")
	link := findChild(t, radio, "radio-link")

	assert.Equal(t, 20.0, findChild(t, link, "tx-power").Value)
	assert.Nil(t, link.Child("rssi"), "config-only reads omit read-only leaves")
	assert.Nil(t, findChild(t, root, "system-info").Child("uptime"))
}

func TestReadStateIncludesReadOnly(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestTree(t)

	root, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{State: true})
	require.NoError(t, err)

	radio := findEntry(t, findChild(t, root, "interfaces", "interface"), "radio0")
	assert.Equal(t, -72.5, findChild(t, findChild(t, radio, "radio-link"), "rssi").Value)
	assert.Equal(t, uint32(0), findChild(t, findChild(t, root, "system-info"), "uptime").Value)
}

func TestReadAddressesLeafListEntryAndContainer(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestTree(t)

	mtu, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{
		Prefix: "interfaces/interface[name=eth0]/mtu",
		State:  true,
	})
	require.NoError(t, err)
	require.NotNil(t, mtu)
	assert.Equal(t, router.KindLeaf, mtu.Kind)
	assert.Equal(t, uint32(1500), mtu.Value)

	vlan, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{Prefix: "vlans/vlan[id=100]", State: true})
	require.NoError(t, err)
	require.NotNil(t, vlan)
	require.Equal(t, router.KindList, vlan.Kind)
	require.Len(t, vlan.Children, 1)
	assert.Equal(t, "100", vlan.Children[0].KeyValue)

	system, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{Prefix: "system-info", State: true})
	require.NoError(t, err)
	require.NotNil(t, system)
	assert.Equal(t, "sim-001", findChild(t, system, "device-id").Value)

	// An instance the datastore does not hold is "no data", not an error.
	missing, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{Prefix: "vlans/vlan[id=999]", State: true})
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestReadListsEntriesInKeyOrder(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestTree(t)

	require.NoError(t, apply(ctx, r, "vlans", datatree.OpMerge, vlan(9, "MGT")))

	list, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{Prefix: "vlans/vlan", State: true})
	require.NoError(t, err)
	require.Len(t, list.Children, 2)
	assert.Equal(t, "9", list.Children[0].KeyValue)
	assert.Equal(t, "100", list.Children[1].KeyValue)
}

func TestApplyMergesAndDeletes(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestTree(t)

	require.NoError(t, apply(ctx, r, "vlans", datatree.OpMerge, vlan(200, "VOICE")))

	name, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{Prefix: "vlans/vlan[id=200]/name", State: true})
	require.NoError(t, err)
	require.NotNil(t, name)
	assert.Equal(t, "VOICE", name.Value)

	require.NoError(t, apply(ctx, r, "vlans", datatree.OpDelete, keyed("vlan", "id", "200")))
	deleted, err := datatree.Read(ctx, r, store.Running, datatree.ReadOptions{Prefix: "vlans/vlan[id=200]/name", State: true})
	require.NoError(t, err)
	assert.Nil(t, deleted)
}

func TestApplyRejectsInvalidSnapshotWithoutWriting(t *testing.T) {
	ctx := context.Background()
	r, st := newTestTree(t)

	err := applyLeaf(ctx, r, "interfaces/interface[name=radio0]/radio-link", datatree.OpMerge, "tx-power", "999")
	require.Error(t, err)

	treeErr, ok := datatree.AsError(err)
	require.True(t, ok)
	assert.Equal(t, datatree.TagInvalidValue, treeErr.Tag)
	assert.True(t, treeErr.Validation, "a rejected snapshot is a validation failure")

	stored, getErr := st.Get(ctx, store.Running, "interfaces/interface[name=radio0]/radio-link/tx-power")
	require.NoError(t, getErr)
	assert.Equal(t, 20.0, stored)
}

func TestApplyCreateOnExistingDataFails(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestTree(t)

	err := apply(ctx, r, "vlans", datatree.OpCreate, vlan(100, "DATA"))
	require.Error(t, err)

	treeErr, ok := datatree.AsError(err)
	require.True(t, ok)
	assert.Equal(t, datatree.TagDataExists, treeErr.Tag)
}

func TestApplyRejectsReadOnlyLeaf(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestTree(t)

	err := applyLeaf(ctx, r, "interfaces/interface[name=radio0]/radio-link", datatree.OpMerge, "rssi", "-40")
	require.Error(t, err)

	treeErr, ok := datatree.AsError(err)
	require.True(t, ok)
	assert.Equal(t, datatree.TagAccessDenied, treeErr.Tag)
}

// apply edits the running datastore with one document.
func apply(ctx context.Context, r *router.Router, base string, op datatree.Op, document *datatree.Document) error {
	return datatree.Apply(ctx, r, store.Running, datatree.Request{
		BasePath:  base,
		Default:   op,
		Documents: []*datatree.Document{document},
	})
}

// applyLeaf edits one leaf at base.
func applyLeaf(ctx context.Context, r *router.Router, base string, op datatree.Op, name, text string) error {
	return apply(ctx, r, base, op, &datatree.Document{Name: name, Text: text, HasText: text != ""})
}

// vlan builds a VLAN list-entry document.
func vlan(id uint16, name string) *datatree.Document {
	return &datatree.Document{
		Name: "vlan",
		Children: []*datatree.Document{
			{Name: "id", Text: itoa(id), HasText: true},
			{Name: "name", Text: name, HasText: true},
		},
	}
}

// keyed builds a list-entry document that only carries its key leaf, which is
// enough for a delete.
func keyed(name, key, value string) *datatree.Document {
	return &datatree.Document{
		Name:     name,
		Children: []*datatree.Document{{Name: key, Text: value, HasText: true}},
	}
}

func itoa(value uint16) string {
	return strconv.FormatUint(uint64(value), 10)
}

// findChild returns the nested child addressed by names, failing when it is
// missing.
func findChild(t *testing.T, node *datatree.Node, names ...string) *datatree.Node {
	t.Helper()
	require.NotNil(t, node, "parent of %v", names)
	for _, name := range names {
		node = node.Child(name)
		require.NotNil(t, node, "child %q", name)
	}
	return node
}

// findEntry returns the list entry with the given key value.
func findEntry(t *testing.T, list *datatree.Node, key string) *datatree.Node {
	t.Helper()
	require.NotNil(t, list)
	for _, entry := range list.Children {
		if entry.KeyValue == key {
			return entry
		}
	}
	t.Fatalf("entry %q not found in %s", key, list.Path)
	return nil
}
