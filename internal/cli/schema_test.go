package cli

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/yang"
)

// runSchema executes the schema command and returns its output.
func runSchema(t *testing.T, args ...string) (string, error) {
	t.Helper()

	out := &bytes.Buffer{}
	cmd := newSchemaCmd(out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
}

// schemaPath finds the node whose path is exactly path.
func schemaPath(t *testing.T, node *schemaNodeJSON, path string) *schemaNodeJSON {
	t.Helper()

	if node.Path == path {
		return node
	}
	for i := range node.Children {
		if found := schemaPath(t, &node.Children[i], path); found != nil {
			return found
		}
	}
	return nil
}

func TestSchemaPrintsTheModelTree(t *testing.T) {
	output, err := runSchema(t)
	require.NoError(t, err)

	var root schemaNodeJSON
	require.NoError(t, json.Unmarshal([]byte(output), &root))
	assert.Equal(t, "container", root.Kind)
	assert.Len(t, root.Children, 8, "every top-level model node must be listed")

	tests := []struct {
		path   string
		module string
		kind   string
		key    string
		leaf   string
	}{
		{path: "system-info", module: "sim-device", kind: "container"},
		{path: "system-info/device-id", module: "sim-device", kind: "leaf", leaf: "string"},
		{path: "interfaces/interface", module: "sim-device", kind: "list", key: "name"},
		{
			path:   "interfaces/interface/radio-link/tx-power",
			module: "sim-radio-link",
			kind:   "leaf",
			leaf:   "float64",
		},
		{
			path:   "interfaces/interface/radio-link/modulation-profile",
			module: "sim-radio-link",
			kind:   "list",
			key:    "id",
		},
		{path: "vlans/vlan", module: "sim-l2-switching", kind: "list", key: "id"},
		{path: "stp/state", module: "sim-l2-switching", kind: "container"},
		{path: "ptp/clock/state", module: "sim-sync", kind: "leaf", leaf: "string"},
		{path: "synce/interfaces/interface", module: "sim-sync", kind: "list", key: "name"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			node := schemaPath(t, &root, test.path)
			require.NotNil(t, node, "schema must document %s", test.path)
			assert.Equal(t, test.module, node.Module)
			assert.Equal(t, test.kind, node.Kind)
			assert.Equal(t, test.key, node.Key)
			assert.Equal(t, test.leaf, node.Leaf)
		})
	}
}

func TestSchemaYangPrintsEveryModule(t *testing.T) {
	output, err := runSchema(t, "--yang")
	require.NoError(t, err)

	for _, module := range []string{"sim-device", "sim-radio-link", "sim-l2-switching", "sim-sync"} {
		assert.Contains(t, output, "// --- yang/"+module+".yang ---")
		assert.Contains(t, output, "module "+module+" {")
	}
	assert.NotContains(t, output, "--module needs --yang")
}

func TestSchemaYangPrintsASingleModule(t *testing.T) {
	output, err := runSchema(t, "--yang", "--module", "sim-sync")
	require.NoError(t, err)

	expected, err := yang.Read("sim-sync")
	require.NoError(t, err)
	assert.Equal(t, string(expected), output)
	assert.NotContains(t, output, "// --- yang/")
	assert.NotContains(t, output, "module sim-device {")
}

func TestSchemaRejectsAnUnknownModule(t *testing.T) {
	_, err := runSchema(t, "--yang", "--module", "no-such-module")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no-such-module")
}

func TestSchemaRejectsModuleWithoutYang(t *testing.T) {
	_, err := runSchema(t, "--module", "sim-sync")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--module needs --yang")
}

func TestSchemaRejectsArguments(t *testing.T) {
	_, err := runSchema(t, "extra")
	require.Error(t, err)
}
