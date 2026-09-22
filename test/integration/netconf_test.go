//go:build integration

package integration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A NETCONF client performs an edit-config/commit/get-config round-trip against
// the containerised simulator over the real SSH subsystem.
func TestNETCONFRoundTrip(t *testing.T) {
	sim := startSimulator(t, 1162)
	client := dialNETCONF(t, sim.netconfAddr)

	// get-config returns the seeded system information.
	reply := client.exchange(t, `<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">`+
		`<get-config><source><running/></source><filter>`+
		`<system-info><name/></system-info>`+
		`</filter></get-config></rpc>`)
	assert.Contains(t, reply, "<name>triadsim-01</name>")

	// edit-config into candidate, commit it and read it back from running.
	reply = client.exchange(t, `<rpc message-id="2" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">`+
		`<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>radio0</name>`+
		`<radio-link><tx-power>22.5</tx-power></radio-link>`+
		`</interface></interfaces>`+
		`</config></edit-config></rpc>`)
	require.Contains(t, reply, "<ok>")

	reply = client.exchange(t, `<rpc message-id="3" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><commit/></rpc>`)
	require.Contains(t, reply, "<ok>")

	reply = client.exchange(t, `<rpc message-id="4" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">`+
		`<get-config><source><running/></source><filter>`+
		`<interfaces><interface><name>radio0</name>`+
		`<radio-link><tx-power/></radio-link>`+
		`</interface></interfaces>`+
		`</filter></get-config></rpc>`)
	assert.Contains(t, reply, "<tx-power>22.5</tx-power>")

	// A value the model rejects comes back as an rpc-error, so the running
	// configuration keeps the committed value.
	reply = client.exchange(t, `<rpc message-id="5" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">`+
		`<edit-config><target><candidate/></target><config>`+
		`<interfaces><interface><name>radio0</name>`+
		`<radio-link><tx-power>999</tx-power></radio-link>`+
		`</interface></interfaces>`+
		`</config></edit-config></rpc>`)
	assert.Contains(t, reply, "invalid-value")
}
