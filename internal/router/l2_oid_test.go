package router

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/store"
)

// The BRIDGE-MIB and Q-BRIDGE-MIB columns of the seeded device.
func TestBindingsExposeL2MIB(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	bindings, err := r.Bindings(ctx, store.Running)
	require.NoError(t, err)

	byOID := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		byOID[binding.OID] = binding
	}

	// The seeded permanent entry maps 02:00:00:00:00:02 to bridge port 3.
	mac := "2.0.0.0.0.2"
	address, ok := byOID["1.3.6.1.2.1.17.4.3.1.1."+mac]
	require.True(t, ok, "dot1dTpFdbAddress instance")
	assert.Equal(t, TypeOctetString, address.Type)
	assert.Equal(t, []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x02}, address.Value)

	port, ok := byOID["1.3.6.1.2.1.17.4.3.1.2."+mac]
	require.True(t, ok, "dot1dTpFdbPort instance")
	assert.Equal(t, TypeInteger, port.Type)
	assert.Equal(t, uint32(3), port.Value)
	assert.Equal(t, "mac-table/entry[mac-address=02:00:00:00:00:02]/port", port.Path)

	status, ok := byOID["1.3.6.1.2.1.17.4.3.1.3."+mac]
	require.True(t, ok, "dot1dTpFdbStatus instance")
	assert.Equal(t, 5, status.Value, "a permanent entry is mgmt(5)")

	// dot1dStpPortTable is indexed by the bridge port number: radio0=1,
	// eth0=2, eth1=3.
	eth0, ok := byOID["1.3.6.1.2.1.17.2.15.1.3.2"]
	require.True(t, ok, "dot1dStpPortState of eth0")
	assert.Equal(t, 5, eth0.Value, "eth0 forwards")
	eth1, ok := byOID["1.3.6.1.2.1.17.2.15.1.3.3"]
	require.True(t, ok, "dot1dStpPortState of eth1")
	assert.Equal(t, 2, eth1.Value, "an RSTP discarding port is reported as blocking")

	assert.Equal(t, 1, byOID["1.3.6.1.2.1.17.2.15.1.4.2"].Value)
	assert.Equal(t, uint8(128), byOID["1.3.6.1.2.1.17.2.15.1.2.2"].Value)
	assert.Equal(t, uint32(20000), byOID["1.3.6.1.2.1.17.2.15.1.5.2"].Value)

	// dot1dStpPriority, dot1dTpAgingTime and dot1dBaseBridgeAddress.
	assert.Equal(t, uint16(32768), byOID["1.3.6.1.2.1.17.2.2.0"].Value)
	assert.True(t, byOID["1.3.6.1.2.1.17.2.2.0"].Writable)
	assert.Equal(t, uint32(300), byOID["1.3.6.1.2.1.17.4.2.0"].Value)
	assert.True(t, byOID["1.3.6.1.2.1.17.4.2.0"].Writable)
	assert.Equal(t, []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}, byOID["1.3.6.1.2.1.17.1.1.0"].Value)

	// Q-BRIDGE-MIB: the VLAN name table, indexed by the VLAN identifier.
	name, ok := byOID["1.3.6.1.2.1.17.7.1.4.3.1.1.100"]
	require.True(t, ok, "dot1qVlanStaticName instance")
	assert.Equal(t, "DATA", name.Value)
}

// The ifTable counters and their 64-bit ifXTable counterparts.
func TestBindingsExposeInterfaceCounters(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	_, err := r.SetState(ctx, "interfaces/interface[name=eth0]/counters/in-octets", uint64(1<<33))
	require.NoError(t, err)
	_, err = r.SetState(ctx, "interfaces/interface[name=eth0]/counters/in-ucast-pkts", uint64(7))
	require.NoError(t, err)

	bindings, err := r.Bindings(ctx, store.Running)
	require.NoError(t, err)
	byOID := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		byOID[binding.OID] = binding
	}

	// eth0 is interface index 2.
	ifInOctets, ok := byOID["1.3.6.1.2.1.2.2.1.10.2"]
	require.True(t, ok)
	assert.Equal(t, TypeCounter32, ifInOctets.Type)
	assert.Equal(t, uint64(1<<33), ifInOctets.Value)

	hcInOctets, ok := byOID["1.3.6.1.2.1.31.1.1.1.6.2"]
	require.True(t, ok)
	assert.Equal(t, TypeCounter64, hcInOctets.Type)
	assert.Equal(t, uint64(1<<33), hcInOctets.Value)

	assert.Equal(t, uint64(7), byOID["1.3.6.1.2.1.2.2.1.11.2"].Value)
	assert.Equal(t, uint64(7), byOID["1.3.6.1.2.1.31.1.1.1.7.2"].Value)
	assert.Equal(t, TypeCounter64, byOID["1.3.6.1.2.1.31.1.1.1.10.2"].Type)
}

// An entry that appears in the datastore at runtime, such as a learned MAC
// address, gets its MIB instances from the snapshot.
func TestBindingsFollowRuntimeMACEntries(t *testing.T) {
	ctx := context.Background()
	r, _ := newTestRouter(t)

	learned := "02:00:00:00:00:aa"
	_, err := r.Set(ctx, store.Running, "mac-table/entry[mac-address="+learned+"]/port", uint32(2))
	require.NoError(t, err)
	_, err = r.Set(ctx, store.Running, "mac-table/entry[mac-address="+learned+"]/type", "dynamic")
	require.NoError(t, err)

	bindings, err := r.Bindings(ctx, store.Running)
	require.NoError(t, err)
	byOID := make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		byOID[binding.OID] = binding
	}

	mac := "2.0.0.0.0.170"
	port, ok := byOID["1.3.6.1.2.1.17.4.3.1.2."+mac]
	require.True(t, ok)
	assert.Equal(t, uint32(2), port.Value)

	status, ok := byOID["1.3.6.1.2.1.17.4.3.1.3."+mac]
	require.True(t, ok)
	assert.Equal(t, 3, status.Value, "a dynamic entry is learned(3)")

	// A VLAN created at runtime gets its dot1qVlanStaticName instance too.
	_, err = r.Set(ctx, store.Running, "vlans/vlan[id=200]/name", "VOICE")
	require.NoError(t, err)

	bindings, err = r.Bindings(ctx, store.Running)
	require.NoError(t, err)
	byOID = make(map[string]Binding, len(bindings))
	for _, binding := range bindings {
		byOID[binding.OID] = binding
	}
	assert.Equal(t, "VOICE", byOID["1.3.6.1.2.1.17.7.1.4.3.1.1.200"].Value)
}

func TestMacInstance(t *testing.T) {
	assert.Equal(t, "2.0.0.0.0.1", macInstance("02:00:00:00:00:01"))
	assert.Equal(t, "255.255.255.255.255.255", macInstance("ff:ff:ff:ff:ff:ff"))
	assert.Equal(t, "", macInstance("not-a-mac"))
}
