package router

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// nodeNames returns the child names in order, for compact assertions.
func nodeNames(nodes []Node) []string {
	names := make([]string, 0, len(nodes))
	for _, node := range nodes {
		names = append(names, node.Name)
	}
	return names
}

// findNode returns the child named name.
func findNode(t *testing.T, nodes []Node, name string) Node {
	t.Helper()
	for _, node := range nodes {
		if node.Name == name {
			return node
		}
	}
	t.Fatalf("child %q not found in %v", name, nodeNames(nodes))
	return Node{}
}

func TestChildrenOfRoot(t *testing.T) {
	r, _ := newTestRouter(t)

	children, err := r.Children("")
	require.NoError(t, err)
	assert.Equal(t, []string{"system-info", "interfaces", "vlans", "mac-table", "stp", "lldp"}, nodeNames(children))
	assert.Equal(t, KindContainer, findNode(t, children, "system-info").Kind)
	assert.Equal(t, KindContainer, findNode(t, children, "interfaces").Kind)
	assert.Equal(t, KindContainer, findNode(t, children, "vlans").Kind)
	assert.Equal(t, KindContainer, findNode(t, children, "mac-table").Kind)
	assert.Equal(t, KindContainer, findNode(t, children, "lldp").Kind)
	// stp/state is one field with a two-segment tag: stp is the container.
	assert.Equal(t, KindContainer, findNode(t, children, "stp").Kind)
}

func TestChildrenLeafKinds(t *testing.T) {
	r, _ := newTestRouter(t)

	children, err := r.Children("system-info")
	require.NoError(t, err)

	tests := []struct {
		name string
		kind LeafKind
	}{
		{name: "device-id", kind: LeafString},
		{name: "name", kind: LeafString},
		{name: "description", kind: LeafString},
		{name: "contact", kind: LeafString},
		{name: "location", kind: LeafString},
		{name: "uptime", kind: LeafUint32},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node := findNode(t, children, tt.name)
			assert.Equal(t, KindLeaf, node.Kind)
			assert.Equal(t, tt.kind, node.Leaf)
			assert.Empty(t, node.Key)
		})
	}
}

func TestChildrenExpandMultiSegmentTag(t *testing.T) {
	r, _ := newTestRouter(t)

	// Device.Interfaces has the tag interfaces/interface: the container and the
	// list are one Go field but two schema nodes.
	children, err := r.Children("interfaces")
	require.NoError(t, err)
	require.Equal(t, []string{"interface"}, nodeNames(children))

	iface := children[0]
	assert.Equal(t, KindList, iface.Kind)
	assert.Equal(t, "name", iface.Key)
	assert.Empty(t, iface.Leaf)
}

func TestChildrenOfListEntry(t *testing.T) {
	r, _ := newTestRouter(t)

	children, err := r.Children("interfaces/interface[name=radio0]")
	require.NoError(t, err)
	assert.Equal(t,
		[]string{"name", "type", "enabled", "mtu", "mac-address", "radio-link", "counters"},
		nodeNames(children))
	assert.Equal(t, LeafUint32, findNode(t, children, "mtu").Leaf)
	assert.Equal(t, LeafBool, findNode(t, children, "enabled").Leaf)
	assert.Equal(t, KindContainer, findNode(t, children, "radio-link").Kind)
	assert.Equal(t, KindContainer, findNode(t, children, "counters").Kind)
}

func TestChildrenOfNestedRadioLink(t *testing.T) {
	r, _ := newTestRouter(t)

	children, err := r.Children("interfaces/interface[name=radio0]/radio-link")
	require.NoError(t, err)
	assert.Equal(t,
		[]string{"name", "tx-power", "rssi", "fade-margin", "capacity", "link-budget", "atpc", "acm", "modulation-profile"},
		nodeNames(children))
	assert.Equal(t, LeafFloat64, findNode(t, children, "tx-power").Leaf)

	profiles := findNode(t, children, "modulation-profile")
	assert.Equal(t, KindList, profiles.Kind)
	assert.Equal(t, "id", profiles.Key)

	entry, err := r.Children("interfaces/interface[name=radio0]/radio-link/modulation-profile[id=5]")
	require.NoError(t, err)
	assert.Equal(t,
		[]string{"id", "name", "modulation", "coding-rate", "spectral-efficiency", "rsl-threshold", "capacity"},
		nodeNames(entry))
	assert.Equal(t, LeafUint8, findNode(t, entry, "id").Leaf)

	atpc, err := r.Children("interfaces/interface[name=radio0]/radio-link/atpc")
	require.NoError(t, err)
	assert.Equal(t,
		[]string{"enabled", "target-rsl", "min-power", "max-power", "range", "current-power"},
		nodeNames(atpc))
}

// TestChildrenAreInstanceAgnostic documents that the schema is derived from the
// model type: eth0 carries no radio link in the template, but the schema still
// describes the node. Convert, Get and Set enforce the instance.
func TestChildrenAreInstanceAgnostic(t *testing.T) {
	r, _ := newTestRouter(t)

	children, err := r.Children("interfaces/interface[name=eth0]/radio-link")
	require.NoError(t, err)
	assert.Contains(t, nodeNames(children), "tx-power")
}

func TestChildrenRejectsInvalidPaths(t *testing.T) {
	r, _ := newTestRouter(t)

	tests := []struct {
		name string
		path string
		want error
	}{
		{name: "unknown node", path: "system-info/nope", want: ErrNotFound},
		{name: "leaf has no children", path: "system-info/device-id", want: ErrInvalidPath},
		{name: "leaf in the middle", path: "system-info/device-id/nope", want: ErrNotFound},
		{name: "list needs a predicate", path: "interfaces/interface", want: ErrInvalidPath},
		{name: "wrong key name", path: "interfaces/interface[id=1]", want: ErrInvalidPath},
		{name: "predicate on a container", path: "system-info[name=x]", want: ErrInvalidPath},
		{name: "empty segment", path: "system-info//name", want: ErrInvalidPath},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := r.Children(tt.path)

			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.want), "want %v, got %v", tt.want, err)
		})
	}
}
