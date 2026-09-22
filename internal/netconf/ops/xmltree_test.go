package ops

import (
	"encoding/xml"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseElementTree(t *testing.T) {
	data := `<rpc message-id="1" xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0">` +
		`<edit-config><target><candidate/></target>` +
		`<config><radio-link nc:operation="merge"><tx-power>20.5</tx-power></radio-link></config>` +
		`</edit-config></rpc>`

	root, err := ParseElement([]byte(data))
	require.NoError(t, err)
	assert.Equal(t, "rpc", root.Name)

	value, ok := root.Attr("message-id")
	require.True(t, ok)
	assert.Equal(t, "1", value)

	editConfig := root.Child("edit-config")
	require.NotNil(t, editConfig)
	require.NotNil(t, editConfig.Child("target"))
	require.NotNil(t, editConfig.Child("target").Child("candidate"))

	config := editConfig.Child("config")
	require.NotNil(t, config)
	radioLink := config.Child("radio-link")
	require.NotNil(t, radioLink)

	// Namespaced attributes are matched by local name.
	operation, ok := radioLink.Attr("operation")
	require.True(t, ok)
	assert.Equal(t, "merge", operation)

	assert.Equal(t, "20.5", radioLink.Child("tx-power").TrimmedText())
}

func TestParseElementTextAndWhitespace(t *testing.T) {
	element, err := ParseElement([]byte("<tx-power>\n   20.5\n</tx-power>"))
	require.NoError(t, err)

	assert.Equal(t, "\n   20.5\n", element.Text)
	assert.Equal(t, "20.5", element.TrimmedText())
}

func TestParseElementChildrenNamed(t *testing.T) {
	element, err := ParseElement([]byte(`<interfaces><interface>a</interface><other/><interface>b</interface></interfaces>`))
	require.NoError(t, err)

	interfaces := element.ChildrenNamed("interface")
	require.Len(t, interfaces, 2)
	assert.Equal(t, "a", interfaces[0].TrimmedText())
	assert.Equal(t, "b", interfaces[1].TrimmedText())
	assert.Nil(t, element.Child("nope"))
}

func TestParseElementErrors(t *testing.T) {
	tests := []struct {
		name string
		data string
	}{
		{name: "empty document", data: ""},
		{name: "malformed xml", data: "<a><b>"},
		{name: "two roots", data: "<a/><b/>"},
		{name: "only text", data: "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseElement([]byte(tt.data))

			assert.Error(t, err)
		})
	}
}

func TestElementMarshal(t *testing.T) {
	element := &Element{Space: "urn:sim:device", Name: "system-info", Children: []*Element{
		{Name: "device-id", Text: "sim-001"},
		{Name: "description", Text: `a & b <c>`},
	}}

	data, err := xml.Marshal(element)
	require.NoError(t, err)

	assert.Equal(t,
		`<system-info xmlns="urn:sim:device"><device-id>sim-001</device-id><description>a &amp; b &lt;c&gt;</description></system-info>`,
		string(data))
}

func TestElementMarshalEmpty(t *testing.T) {
	data, err := xml.Marshal(NewElement("data"))
	require.NoError(t, err)

	assert.Equal(t, `<data></data>`, string(data))
}

func TestElementAppendReturnsReceiver(t *testing.T) {
	element := NewElement("data").Append(NewElement("a"), NewElement("b"))

	require.Len(t, element.Children, 2)
	assert.Equal(t, "a", element.Children[0].Name)
	assert.Equal(t, "b", element.Children[1].Name)
}
