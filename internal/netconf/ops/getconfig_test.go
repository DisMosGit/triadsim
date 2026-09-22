package ops

import (
	"context"
	"encoding/xml"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/model"
	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// newTestDeps returns operations dependencies over a store seeded with
// DefaultDevice.
func newTestDeps(t *testing.T) Deps {
	t.Helper()

	deps, _ := newTestDepsWithFile(t)
	return deps
}

// newTestDepsWithFile also returns the startup file the store persists commits
// to.
func newTestDepsWithFile(t *testing.T) (Deps, string) {
	t.Helper()
	ctx := context.Background()

	startupFile := filepath.Join(t.TempDir(), "startup.json")
	st := store.NewMemory(store.Options{StartupFile: startupFile})
	r, err := router.New(model.DefaultDevice(), st)
	require.NoError(t, err)
	st.SetValidator(r.Validate)
	require.NoError(t, r.Seed(ctx, store.Candidate))
	require.NoError(t, st.Commit(ctx))

	return Deps{Router: r, Store: st}, startupFile
}

// parseOperation parses one operation element from its XML text.
func parseOperation(t *testing.T, body string) *Element {
	t.Helper()

	element, err := ParseElement([]byte(body))
	require.NoError(t, err)
	return element
}

// serialize renders an element tree back to XML.
func serialize(t *testing.T, element *Element) string {
	t.Helper()

	data, err := xml.Marshal(element)
	require.NoError(t, err)
	return string(data)
}

// getConfig runs get-config over the given operation body.
func getConfig(t *testing.T, deps Deps, body string) string {
	t.Helper()

	data, err := GetConfig(context.Background(), deps, parseOperation(t, body))
	require.NoError(t, err)
	require.Equal(t, "data", data.Name)
	return serialize(t, data)
}

func TestGetConfigReturnsConfigurationDataOnly(t *testing.T) {
	deps := newTestDeps(t)

	got := getConfig(t, deps, `<get-config><source><running/></source></get-config>`)

	// Configuration data of the device and radio modules is present.
	assert.Contains(t, got, `<data>`)
	assert.Contains(t, got, `<system-info xmlns="urn:sim:device"><device-id>sim-001</device-id>`)
	assert.Contains(t, got, `<interfaces xmlns="urn:sim:device"><interface><name>eth0</name>`)
	assert.Contains(t, got, `<radio-link xmlns="urn:sim:radio-link"><name>radio0</name><tx-power>20</tx-power>`)

	// State leaves (config:"false") are not part of <config>.
	for _, state := range []string{"rssi", "fade-margin", "uptime", "counters", "current-power", "current-profile"} {
		assert.NotContains(t, got, ">"+state+"<", "state leaf %s must not be returned", state)
	}

	// Siblings belonging to a module declare its namespace themselves: a
	// declaration on system-info is not in scope for interfaces.
	assert.NotContains(t, got, `<interfaces xmlns="urn:sim:radio-link"`)
	assert.NotContains(t, got, `<radio-link xmlns="urn:sim:device"`)
}

func TestGetConfigOrdersNumericListKeysNumerically(t *testing.T) {
	deps := newTestDeps(t)

	got := getConfig(t, deps, `<get-config><source><running/></source></get-config>`)

	first := strings.Index(got, "<id>1</id>")
	second := strings.Index(got, "<id>2</id>")
	last := strings.Index(got, "<id>12</id>")
	require.Positive(t, first)
	require.Positive(t, second)
	require.Positive(t, last)
	assert.Less(t, first, second)
	assert.Less(t, second, last)
}

func TestGetConfigSubtreeFilterByKey(t *testing.T) {
	deps := newTestDeps(t)

	got := getConfig(t, deps, `<get-config><source><running/></source><filter type="subtree">`+
		`<interfaces><interface><name>radio0</name></interface></interfaces>`+
		`</filter></get-config>`)

	assert.Equal(t, `<data><interfaces xmlns="urn:sim:device"><interface><name>radio0</name></interface></interfaces></data>`, got)
}

func TestGetConfigSubtreeFilterSelectsSubtrees(t *testing.T) {
	deps := newTestDeps(t)

	got := getConfig(t, deps, `<get-config><source><running/></source><filter>`+
		`<interfaces><interface><name>radio0</name><radio-link><tx-power/></radio-link></interface></interfaces>`+
		`</filter></get-config>`)

	assert.Contains(t, got, `<interface><name>radio0</name><radio-link xmlns="urn:sim:radio-link"><tx-power>20</tx-power></radio-link></interface>`)
	assert.NotContains(t, got, "rssi")
	assert.NotContains(t, got, "<type>")
}

func TestGetConfigFilterWithoutKeySelectsEveryInstance(t *testing.T) {
	deps := newTestDeps(t)

	got := getConfig(t, deps, `<get-config><source><running/></source><filter>`+
		`<interfaces><interface><mtu/></interface></interfaces>`+
		`</filter></get-config>`)

	// Every interface is selected, and each entry keeps its key leaf.
	assert.Contains(t, got, `<interface><name>radio0</name><mtu>1500</mtu></interface>`)
	assert.Contains(t, got, `<interface><name>eth0</name><mtu>1500</mtu></interface>`)
	assert.Contains(t, got, `<interface><name>eth1</name><mtu>1500</mtu></interface>`)
	assert.NotContains(t, got, "<enabled>")
}

func TestGetConfigContentMatch(t *testing.T) {
	deps := newTestDeps(t)

	matching := getConfig(t, deps, `<get-config><source><running/></source><filter>`+
		`<system-info><name>triadsim-01</name></system-info></filter></get-config>`)
	assert.Contains(t, matching, "<name>triadsim-01</name>")
	assert.NotContains(t, matching, "device-id")

	notMatching := getConfig(t, deps, `<get-config><source><running/></source><filter>`+
		`<system-info><name>other</name></system-info></filter></get-config>`)
	assert.Equal(t, `<data></data>`, notMatching)
}

func TestGetConfigEmptyFilterSelectsEverything(t *testing.T) {
	deps := newTestDeps(t)
	everything := getConfig(t, deps, `<get-config><source><running/></source></get-config>`)

	got := getConfig(t, deps, `<get-config><source><running/></source><filter type="subtree"/></get-config>`)

	assert.Equal(t, everything, got)
}

func TestGetConfigReadsTheAddressedDatastore(t *testing.T) {
	ctx := context.Background()
	deps := newTestDeps(t)

	path := "interfaces/interface[name=radio0]/radio-link/tx-power"
	_, err := deps.Router.Set(ctx, store.Candidate, path, 15.5)
	require.NoError(t, err)

	running := getConfig(t, deps, `<get-config><source><running/></source><filter><interfaces><interface><name>radio0</name><radio-link><tx-power/></radio-link></interface></interfaces></filter></get-config>`)
	candidate := getConfig(t, deps, `<get-config><source><candidate/></source><filter><interfaces><interface><name>radio0</name><radio-link><tx-power/></radio-link></interface></interfaces></filter></get-config>`)

	assert.Contains(t, running, "<tx-power>20</tx-power>")
	assert.Contains(t, candidate, "<tx-power>15.5</tx-power>")
}

func TestGetConfigRejectsInvalidSourcesAndFilters(t *testing.T) {
	deps := newTestDeps(t)

	tests := []struct {
		name string
		body string
		tag  string
	}{
		{
			name: "missing source",
			body: `<get-config><filter/></get-config>`,
			tag:  TagMissingElement,
		},
		{
			name: "empty source",
			body: `<get-config><source/></get-config>`,
			tag:  TagMissingElement,
		},
		{
			name: "unknown datastore",
			body: `<get-config><source><operational/></source></get-config>`,
			tag:  TagUnknownElement,
		},
		{
			name: "two datastores",
			body: `<get-config><source><running/><candidate/></source></get-config>`,
			tag:  TagInvalidValue,
		},
		{
			name: "xpath filter",
			body: `<get-config><source><running/></source><filter type="xpath" select="/x"/></get-config>`,
			tag:  TagOperationNotSupported,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := GetConfig(context.Background(), deps, parseOperation(t, tt.body))

			require.Error(t, err)
			opErr := &Error{}
			require.ErrorAs(t, err, &opErr)
			assert.Equal(t, tt.tag, opErr.Tag)
		})
	}
}
