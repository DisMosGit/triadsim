package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
)

func TestDecodeJSONObject(t *testing.T) {
	payload, opErr := decodeJSON([]byte(`{"sim-l2-switching:vlan":{"id":100,"name":"DATA"}}`))
	require.Nil(t, opErr)
	require.Equal(t, "vlan", payload.Name)
	require.Len(t, payload.Documents, 1)
	assert.False(t, payload.IsScalar)

	document := payload.Documents[0]
	assert.Equal(t, "vlan", document.Name)
	require.NotNil(t, document.Child("id"))
	assert.Equal(t, "100", document.Child("id").Text)
	assert.Equal(t, "DATA", document.Child("name").Text)
}

func TestDecodeJSONListAndScalar(t *testing.T) {
	payload, opErr := decodeJSON([]byte(`{"vlan":[{"id":100},{"id":200}]}`))
	require.Nil(t, opErr)
	require.Len(t, payload.Documents, 2)
	assert.Equal(t, "100", payload.Documents[0].Child("id").Text)
	assert.Equal(t, "200", payload.Documents[1].Child("id").Text)

	payload, opErr = decodeJSON([]byte(`{"sim-device:name":"triadsim-01"}`))
	require.Nil(t, opErr)
	assert.True(t, payload.IsScalar)
	assert.Equal(t, "triadsim-01", payload.Scalar)
}

func TestDecodeJSONErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		tag  string
	}{
		{name: "empty", body: "", tag: datatree.TagMalformedMessage},
		{name: "malformed", body: `{`, tag: datatree.TagMalformedMessage},
		{name: "trailing data", body: `{"a":1}{"b":2}`, tag: datatree.TagMalformedMessage},
		{name: "not an object", body: `[1,2]`, tag: datatree.TagInvalidValue},
		{name: "two members", body: `{"a":1,"b":2}`, tag: datatree.TagInvalidValue},
		{name: "list of scalars", body: `{"vlan":[1,2]}`, tag: datatree.TagInvalidValue},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, opErr := decodeJSON([]byte(tt.body))
			require.NotNil(t, opErr)
			assert.Equal(t, tt.tag, opErr.Tag)
		})
	}
}

func TestEncodeJSONShapes(t *testing.T) {
	tests := []struct {
		name string
		node *datatree.Node
		want string
	}{
		{
			name: "leaf",
			node: &datatree.Node{
				Path: "system-info/device-id", Name: "device-id",
				Kind: router.KindLeaf, Leaf: router.LeafString, Value: "sim-001",
			},
			want: `{"sim-device:device-id":"sim-001"}`,
		},
		{
			name: "container",
			node: &datatree.Node{
				Path: "stp/state", Name: "state", Kind: router.KindContainer,
				Children: []*datatree.Node{
					{Path: "stp/state/enabled", Name: "enabled", Kind: router.KindLeaf, Leaf: router.LeafBool, Value: true},
					{Path: "stp/state/bridge-priority", Name: "bridge-priority", Kind: router.KindLeaf, Leaf: router.LeafUint16, Value: uint16(32768)},
				},
			},
			want: `{"sim-l2-switching:state":{"enabled":true,"bridge-priority":32768}}`,
		},
		{
			name: "list",
			node: &datatree.Node{
				Path: "vlans/vlan", Name: "vlan", Kind: router.KindList, Key: "id",
				Children: []*datatree.Node{
					{
						Path: "vlans/vlan[id=100]", Name: "vlan", Kind: router.KindContainer, Key: "id", KeyValue: "100",
						Children: []*datatree.Node{
							{Path: "vlans/vlan[id=100]/id", Name: "id", Kind: router.KindLeaf, Leaf: router.LeafUint16, Value: uint16(100)},
						},
					},
				},
			},
			want: `{"sim-l2-switching:vlan":[{"id":100}]}`,
		},
		{
			name: "datastore root",
			node: &datatree.Node{Kind: router.KindContainer, Children: []*datatree.Node{
				{Path: "system-info/device-id", Name: "device-id", Kind: router.KindLeaf, Leaf: router.LeafString, Value: "sim-001"},
				{Path: "vlans/vlan", Name: "vlan", Kind: router.KindList, Key: "id"},
			}},
			want: `{"sim-device:device-id":"sim-001","sim-l2-switching:vlan":[]}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, opErr := encodeJSON(tt.node)
			require.Nil(t, opErr)
			assert.JSONEq(t, tt.want, string(got))
		})
	}
}
