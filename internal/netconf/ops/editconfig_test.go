package ops

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/router"
	"github.com/DisMosGit/triadsim/internal/store"
)

// Paths used by the edit-config tests.
const (
	txPowerPath   = "interfaces/interface[name=radio0]/radio-link/tx-power"
	atpcPath      = "interfaces/interface[name=radio0]/radio-link/atpc/enabled"
	rssiPath      = "interfaces/interface[name=radio0]/radio-link/rssi"
	mtuPath       = "interfaces/interface[name=eth0]/mtu"
	eth0MACPath   = "interfaces/interface[name=eth0]/mac-address"
	eth1MACPath   = "interfaces/interface[name=eth1]/mac-address"
	description   = "system-info/description"
	uptimePath    = "system-info/uptime"
	counterPath   = "interfaces/interface[name=eth0]/counters/in-octets"
	eth0RadioLink = "interfaces/interface[name=eth0]/radio-link/tx-power"
	interfacePath = "interfaces/interface[name=eth0]/name"
)

// editConfig runs edit-config over the given operation body.
func editConfig(t *testing.T, deps Deps, body string) error {
	t.Helper()
	return EditConfig(context.Background(), deps, parseOperation(t, body))
}

// requireEditConfigError runs edit-config and asserts the protocol error tag.
func requireEditConfigError(t *testing.T, deps Deps, body, tag string) {
	t.Helper()

	err := editConfig(t, deps, body)
	require.Error(t, err)
	opErr := &Error{}
	require.ErrorAs(t, err, &opErr)
	assert.Equal(t, tag, opErr.Tag)
}

// storedValue reads one leaf from a datastore.
func storedValue(t *testing.T, deps Deps, ds store.Datastore, path string) any {
	t.Helper()

	value, err := deps.Store.Get(context.Background(), ds, path)
	require.NoError(t, err)
	return value
}

func TestEditConfigMergesIntoCandidate(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>radio0</name><radio-link>`+
		`<tx-power>25.5</tx-power><atpc><enabled>false</enabled></atpc>`+
		`</radio-link></interface></interfaces></config></edit-config>`)

	require.NoError(t, err)
	assert.Equal(t, 25.5, storedValue(t, deps, store.Candidate, txPowerPath))
	assert.Equal(t, false, storedValue(t, deps, store.Candidate, atpcPath))

	// The running datastore is untouched until a commit.
	assert.Equal(t, 20.0, storedValue(t, deps, store.Running, txPowerPath))
	assert.Equal(t, true, storedValue(t, deps, store.Running, atpcPath))
}

func TestEditConfigMergesIntoRunningWithoutPersisting(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><running/></target><config>`+
		`<system-info><name>lab-1</name></system-info></config></edit-config>`)

	require.NoError(t, err)
	assert.Equal(t, "lab-1", storedValue(t, deps, store.Running, "system-info/name"))
	// The startup datastore only changes on commit.
	assert.Equal(t, "triadsim-01", storedValue(t, deps, store.Startup, "system-info/name"))
}

func TestEditConfigMultipleTargetsInOneEdit(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><candidate/></target><config><interfaces>`+
		`<interface><name>eth0</name><mtu>9000</mtu></interface>`+
		`<interface><name>eth1</name><mtu>8000</mtu></interface>`+
		`</interfaces></config></edit-config>`)

	require.NoError(t, err)
	assert.Equal(t, uint32(9000), storedValue(t, deps, store.Candidate, mtuPath))
	assert.Equal(t, uint32(8000), storedValue(t, deps, store.Candidate, "interfaces/interface[name=eth1]/mtu"))
}

func TestEditConfigLeafReplaceBehavesLikeMerge(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><candidate/></target><config><interfaces>`+
		`<interface><name>eth0</name><mtu operation="replace">9000</mtu></interface>`+
		`</interfaces></config></edit-config>`)

	require.NoError(t, err)
	assert.Equal(t, uint32(9000), storedValue(t, deps, store.Candidate, mtuPath))
	// The other leaves of the entry survive a leaf replace.
	assert.Equal(t, true, storedValue(t, deps, store.Candidate, "interfaces/interface[name=eth0]/enabled"))
}

func TestEditConfigContainerReplaceWipesTheSubtree(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<system-info operation="replace"><device-id>sim-001</device-id><name>new</name>`+
		`<description>new description</description><contact>noc@example.net</contact><location>lab</location>`+
		`</system-info></config></edit-config>`)

	require.NoError(t, err)
	assert.Equal(t, "new description", storedValue(t, deps, store.Candidate, description))
	assert.Equal(t, "new", storedValue(t, deps, store.Candidate, "system-info/name"))
	// uptime is state (config:"false") and survives a replace.
	assert.Equal(t, uint32(0), storedValue(t, deps, store.Candidate, uptimePath))
}

func TestEditConfigDefaultOperationReplace(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><candidate/></target>`+
		`<default-operation>replace</default-operation><config>`+
		`<interfaces><interface><name>eth0</name><description/></interface></interfaces>`+
		`</config></edit-config>`)

	// <description> does not exist under interface, so the edit is rejected
	// before anything is written.
	require.Error(t, err)

	err = editConfig(t, deps, `<edit-config><target><candidate/></target>`+
		`<default-operation>replace</default-operation><config>`+
		`<interfaces><interface><name>eth0</name><mtu>9000</mtu><type>ethernet</type><enabled>true</enabled></interface></interfaces>`+
		`</config></edit-config>`)

	require.NoError(t, err)
	assert.Equal(t, uint32(9000), storedValue(t, deps, store.Candidate, mtuPath))
	// The replace wiped the MAC address of eth0, which the edit did not carry.
	_, getErr := deps.Store.Get(context.Background(), store.Candidate, eth0MACPath)
	assert.ErrorIs(t, getErr, store.ErrNotFound)
}

func TestEditConfigDefaultOperationNoneWritesNothing(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><candidate/></target>`+
		`<default-operation>none</default-operation><config>`+
		`<interfaces><interface><name>eth0</name><mtu>9000</mtu></interface></interfaces>`+
		`</config></edit-config>`)

	require.NoError(t, err)
	assert.Equal(t, uint32(1500), storedValue(t, deps, store.Candidate, mtuPath))
}

func TestEditConfigCreateAndDelete(t *testing.T) {
	deps := newTestDeps(t)
	target := `<target><candidate/></target>`
	entry := `<interfaces><interface><name>eth0</name>`

	// create on existing data fails.
	requireEditConfigError(t, deps, `<edit-config>`+target+`<config>`+entry+
		`<mtu operation="create">1400</mtu></interface></interfaces></config></edit-config>`,
		TagDataExists)

	// delete removes the leaf.
	require.NoError(t, editConfig(t, deps, `<edit-config>`+target+`<config>`+entry+
		`<mac-address operation="delete"/></interface></interfaces></config></edit-config>`))
	_, err := deps.Store.Get(context.Background(), store.Candidate, eth0MACPath)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// create on the freed path succeeds.
	require.NoError(t, editConfig(t, deps, `<edit-config>`+target+`<config>`+entry+
		`<mac-address operation="create">02:00:00:00:00:0a</mac-address></interface></interfaces></config></edit-config>`))
	assert.Equal(t, "02:00:00:00:00:0a", storedValue(t, deps, store.Candidate, eth0MACPath))

	// delete of missing data fails, remove of missing data succeeds, and a
	// second delete of the now missing leaf fails again.
	require.NoError(t, editConfig(t, deps, `<edit-config>`+target+`<config>`+entry+
		`<mac-address operation="delete"/></interface></interfaces></config></edit-config>`))
	requireEditConfigError(t, deps, `<edit-config>`+target+`<config>`+entry+
		`<mac-address operation="delete"/></interface></interfaces></config></edit-config>`,
		TagDataMissing)
	require.NoError(t, editConfig(t, deps, `<edit-config>`+target+`<config>`+entry+
		`<mac-address operation="remove"/></interface></interfaces></config></edit-config>`))
}

func TestEditConfigSubtreeDeleteKeepsStateLeaves(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces operation="delete"/></config></edit-config>`)

	require.NoError(t, err)
	_, err = deps.Store.Get(context.Background(), store.Candidate, mtuPath)
	assert.ErrorIs(t, err, store.ErrNotFound)

	// State leaves are not configuration data and stay in place.
	assert.Equal(t, uint64(0), storedValue(t, deps, store.Candidate, counterPath))
	assert.Equal(t, -72.5, storedValue(t, deps, store.Candidate, rssiPath))
}

func TestEditConfigRejectsStateLeaves(t *testing.T) {
	deps := newTestDeps(t)
	entry := `<interfaces><interface><name>radio0</name><radio-link>`

	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+entry+
		`<rssi>10</rssi></radio-link></interface></interfaces></config></edit-config>`,
		TagAccessDenied)

	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+entry+
		`<rssi operation="delete"/></radio-link></interface></interfaces></config></edit-config>`,
		TagAccessDenied)

	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+entry+
		`<rssi operation="remove"/></radio-link></interface></interfaces></config></edit-config>`,
		TagAccessDenied)
}

func TestEditConfigInvalidValueLeavesTheDatastoreUntouched(t *testing.T) {
	deps := newTestDeps(t)

	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>radio0</name><radio-link><tx-power>999</tx-power></radio-link></interface></interfaces>`+
		`</config></edit-config>`, TagInvalidValue)

	assert.Equal(t, 20.0, storedValue(t, deps, store.Candidate, txPowerPath))

	// A non-numeric value is rejected by the leaf type.
	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>eth0</name><mtu>not-a-number</mtu></interface></interfaces>`+
		`</config></edit-config>`, TagInvalidValue)
}

func TestEditConfigRejectsUnknownNodes(t *testing.T) {
	deps := newTestDeps(t)

	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<system-info><nope>1</nope></system-info></config></edit-config>`, TagUnknownElement)

	// eth0 carries no radio link in the device model.
	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>eth0</name><radio-link><tx-power>10</tx-power></radio-link></interface></interfaces>`+
		`</config></edit-config>`, TagUnknownElement)

	// A list entry the device does not have is equally unknown.
	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>eth9</name><mtu>1500</mtu></interface></interfaces>`+
		`</config></edit-config>`, TagUnknownElement)
}

func TestEditConfigRequiresTargetConfigAndListKeys(t *testing.T) {
	deps := newTestDeps(t)

	requireEditConfigError(t, deps, `<edit-config><config><system-info/></config></edit-config>`, TagMissingElement)
	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target></edit-config>`, TagMissingElement)
	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><mtu>1500</mtu></interface></interfaces></config></edit-config>`, TagMissingElement)
	requireEditConfigError(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>a/b</name><mtu>1500</mtu></interface></interfaces></config></edit-config>`, TagInvalidValue)
}

func TestEditConfigRejectsUnsupportedOptions(t *testing.T) {
	deps := newTestDeps(t)
	target := `<target><candidate/></target>`
	body := `<interfaces><interface><name>eth0</name><mtu>1500</mtu></interface></interfaces>`

	tests := []struct {
		name string
		body string
		tag  string
	}{
		{
			name: "test-only",
			body: `<edit-config>` + target + `<test-option>test-only</test-option><config>` + body + `</config></edit-config>`,
			tag:  TagOperationNotSupported,
		},
		{
			name: "rollback-on-error",
			body: `<edit-config>` + target + `<error-option>rollback-on-error</error-option><config>` + body + `</config></edit-config>`,
			tag:  TagOperationNotSupported,
		},
		{
			name: "startup target",
			body: `<edit-config><target><startup/></target><config>` + body + `</config></edit-config>`,
			tag:  TagOperationNotSupported,
		},
		{
			name: "unknown operation attribute",
			body: `<edit-config>` + target + `<config><interfaces><interface><name>eth0</name><mtu operation="upsert">1500</mtu></interface></interfaces></config></edit-config>`,
			tag:  TagInvalidValue,
		},
		{
			name: "unknown default-operation",
			body: `<edit-config>` + target + `<default-operation>upsert</default-operation><config>` + body + `</config></edit-config>`,
			tag:  TagInvalidValue,
		},
		{
			name: "unknown test-option",
			body: `<edit-config>` + target + `<test-option>maybe</test-option><config>` + body + `</config></edit-config>`,
			tag:  TagInvalidValue,
		},
		{
			name: "too many targets",
			body: `<edit-config><target><candidate/><running/></target><config>` + body + `</config></edit-config>`,
			tag:  TagInvalidValue,
		},
		{
			name: "default options are accepted",
			body: `<edit-config>` + target + `<test-option>test-then-set</test-option><error-option>stop-on-error</error-option><config>` + body + `</config></edit-config>`,
			tag:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := editConfig(t, deps, tt.body)

			if tt.tag == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			opErr := &Error{}
			require.ErrorAs(t, err, &opErr)
			assert.Equal(t, tt.tag, opErr.Tag)
		})
	}
}

func TestEditConfigAcceptsNamespacedOperationAttribute(t *testing.T) {
	deps := newTestDeps(t)

	err := editConfig(t, deps, `<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>eth0</name>`+
		`<mtu nc:operation="replace" xmlns:nc="urn:ietf:params:xml:ns:netconf:base:1.0">9000</mtu>`+
		`</interface></interfaces></config></edit-config>`)

	require.NoError(t, err)
	assert.Equal(t, uint32(9000), storedValue(t, deps, store.Candidate, mtuPath))
}

func TestEditConfigEmptyConfigIsANoOp(t *testing.T) {
	deps := newTestDeps(t)

	require.NoError(t, editConfig(t, deps, `<edit-config><target><candidate/></target><config/></edit-config>`))
	assert.Equal(t, 20.0, storedValue(t, deps, store.Candidate, txPowerPath))
}

// TestEditConfigPathsUsedByTests guards the constants above against a rename of
// the model paths. eth0RadioLink is deliberately absent: it must not resolve.
func TestEditConfigPathsUsedByTests(t *testing.T) {
	deps := newTestDeps(t)

	for _, path := range []string{txPowerPath, atpcPath, rssiPath, mtuPath, eth0MACPath, eth1MACPath,
		description, uptimePath, counterPath, interfacePath} {
		_, err := deps.Router.Get(context.Background(), store.Running, path)
		assert.NoError(t, err, "path %s must resolve", path)
	}

	_, err := deps.Router.Get(context.Background(), store.Running, eth0RadioLink)
	assert.ErrorIs(t, err, router.ErrNotFound)
}
