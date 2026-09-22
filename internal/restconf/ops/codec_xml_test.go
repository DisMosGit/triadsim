package ops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/datatree"
	"github.com/DisMosGit/triadsim/internal/router"
)

func TestDecodeXMLObject(t *testing.T) {
	payload, opErr := decodeXML([]byte(`<vlan xmlns="urn:sim:l2-switching"><id>100</id><name>DATA</name></vlan>`))
	require.Nil(t, opErr)
	assert.Equal(t, "vlan", payload.Name)
	require.Len(t, payload.Documents, 1)

	document := payload.Documents[0]
	require.NotNil(t, document.Child("id"))
	assert.Equal(t, "100", document.Child("id").Text)
	assert.Equal(t, "DATA", document.Child("name").Text)
}

func TestDecodeXMLErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		tag  string
	}{
		{name: "empty", body: "  ", tag: datatree.TagMalformedMessage},
		{name: "malformed", body: "<vlan>", tag: datatree.TagMalformedMessage},
		{name: "two roots", body: "<a/><b/>", tag: datatree.TagMalformedMessage},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, opErr := decodeXML([]byte(tt.body))
			require.NotNil(t, opErr)
			assert.Equal(t, tt.tag, opErr.Tag)
		})
	}
}

func TestEncodeXMLShapes(t *testing.T) {
	tests := []struct {
		name string
		node *datatree.Node
		want []string
	}{
		{
			name: "leaf",
			node: &datatree.Node{
				Path: "system-info/device-id", Name: "device-id",
				Kind: router.KindLeaf, Leaf: router.LeafString, Value: "sim-001",
			},
			want: []string{`<device-id xmlns="urn:sim:device">sim-001</device-id>`},
		},
		{
			name: "container with radio module switch",
			node: &datatree.Node{
				Path: "interfaces/interface[name=radio0]", Name: "interface", Kind: router.KindContainer,
				Children: []*datatree.Node{
					{Path: "interfaces/interface[name=radio0]/name", Name: "name", Kind: router.KindLeaf, Leaf: router.LeafString, Value: "radio0"},
					{
						Path: "interfaces/interface[name=radio0]/radio-link", Name: "radio-link", Kind: router.KindContainer,
						Children: []*datatree.Node{
							{Path: "interfaces/interface[name=radio0]/radio-link/tx-power", Name: "tx-power", Kind: router.KindLeaf, Leaf: router.LeafFloat64, Value: 20.5},
						},
					},
				},
			},
			want: []string{
				`<interface xmlns="urn:sim:device">`,
				`<name>radio0</name>`,
				`<radio-link xmlns="urn:sim:radio-link">`,
				`<tx-power>20.5</tx-power>`,
				`</radio-link>`,
				`</interface>`,
			},
		},
		{
			name: "list",
			node: &datatree.Node{
				Path: "vlans/vlan", Name: "vlan", Kind: router.KindList, Key: "id",
				Children: []*datatree.Node{
					{
						Path: "vlans/vlan[id=100]", Name: "vlan", Kind: router.KindContainer, Key: "id", KeyValue: "100",
						Children: []*datatree.Node{{Path: "vlans/vlan[id=100]/id", Name: "id", Kind: router.KindLeaf, Leaf: router.LeafUint16, Value: uint16(100)}},
					},
					{
						Path: "vlans/vlan[id=200]", Name: "vlan", Kind: router.KindContainer, Key: "id", KeyValue: "200",
						Children: []*datatree.Node{{Path: "vlans/vlan[id=200]/id", Name: "id", Kind: router.KindLeaf, Leaf: router.LeafUint16, Value: uint16(200)}},
					},
				},
			},
			want: []string{
				`<vlan xmlns="urn:sim:l2-switching"><id>100</id></vlan>`,
				`<vlan><id>200</id></vlan>`,
			},
		},
		{
			name: "datastore root",
			node: &datatree.Node{Kind: router.KindContainer, Children: []*datatree.Node{
				{Path: "system-info/device-id", Name: "device-id", Kind: router.KindLeaf, Leaf: router.LeafString, Value: "sim-001"},
			}},
			want: []string{`<data xmlns="urn:ietf:params:xml:ns:yang:ietf-restconf">`, `<device-id xmlns="urn:sim:device">sim-001</device-id>`, `</data>`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, opErr := encodeXML(tt.node)
			require.Nil(t, opErr)
			for _, fragment := range tt.want {
				assert.Contains(t, string(got), fragment)
			}
		})
	}
}
