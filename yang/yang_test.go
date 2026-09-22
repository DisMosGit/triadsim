package yang

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// moduleOf maps a module name onto the router.Module constant that owns it.
func moduleOf(t *testing.T, name string) router.Module {
	t.Helper()

	for _, module := range []router.Module{
		router.ModuleDevice,
		router.ModuleRadioLink,
		router.ModuleL2,
		router.ModuleSync,
	} {
		if module.Name == name {
			return module
		}
	}
	t.Fatalf("no router module named %q", name)
	return router.Module{}
}

// sources returns the source of every embedded module keyed by module name.
func sources(t *testing.T) map[string]string {
	t.Helper()

	names, err := Modules()
	require.NoError(t, err)

	out := make(map[string]string, len(names))
	for _, name := range names {
		source, err := Read(name)
		require.NoError(t, err)
		out[strings.TrimSuffix(name, extension)] = string(source)
	}
	return out
}

func TestModulesAreEmbedded(t *testing.T) {
	names, err := Modules()
	require.NoError(t, err)
	assert.Equal(t, []string{
		"sim-device.yang",
		"sim-l2-switching.yang",
		"sim-radio-link.yang",
		"sim-sync.yang",
	}, names)
}

func TestModulesDeclareTheExpectedNameAndNamespace(t *testing.T) {
	for name, source := range sources(t) {
		t.Run(name, func(t *testing.T) {
			module := moduleOf(t, name)

			assert.Contains(t, source, "module "+name+" {")
			assert.Contains(t, source, `namespace "`+module.Namespace+`";`)
			assert.Contains(t, source, "revision 2026-09-22 {")
		})
	}
}

func TestReadAcceptsAFileNameOrAModuleName(t *testing.T) {
	byName, err := Read("sim-sync")
	require.NoError(t, err)

	byFile, err := Read("sim-sync.yang")
	require.NoError(t, err)
	assert.Equal(t, byName, byFile)

	_, err = Read("no-such-module")
	assert.Error(t, err)

	_, err = Read("  ")
	assert.Error(t, err)
}

// TestModulesDocumentEverySchemaNode walks the static schema tree and checks
// that every node name appears in the module that owns it, so a leaf added to
// the Go model fails here until the YANG documentation catches up. The search
// is by name, not by position: the modules describe the same tree, and the
// imported radio-link grouping is declared in sim-radio-link.yang exactly where
// ModuleFor assigns it.
func TestModulesDocumentEverySchemaNode(t *testing.T) {
	r, err := router.New(model.DefaultDevice(), store.NewMemory(store.Options{}))
	require.NoError(t, err)

	modules := sources(t)

	visited := 0
	var walk func(node router.SchemaNode, path string)
	walk = func(node router.SchemaNode, path string) {
		if node.Name != "" {
			visited++
			module := router.ModuleFor(path)
			source, ok := modules[module.Name]
			require.True(t, ok, "module %s is not embedded", module.Name)

			pattern := `\b` + regexp.QuoteMeta(node.Name) + `\b`
			assert.Regexp(t, pattern, source,
				"%s: node %s is not documented in %s.yang", path, node.Name, module.Name)
		}

		next := node.Name
		if path != "" {
			next = path + "/" + node.Name
		}
		for _, child := range node.Children {
			walk(child, next)
		}
	}

	for _, child := range r.Schema().Children {
		walk(child, child.Name)
	}
	assert.Greater(t, visited, 100, "the walk must cover the whole model, not just its roots")
}
