//go:build integration

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A real SNMP client walks the containerised agent and receives the vendor trap
// its simulation API produces.
func TestSNMPWalkAndTrap(t *testing.T) {
	traps := newTrapReceiver(t)
	sim := startSimulator(t, traps.port)

	client := &gosnmp.GoSNMP{
		Target:    "127.0.0.1",
		Port:      sim.snmpPort,
		Transport: "udp",
		Community: "public",
		Version:   gosnmp.Version2c,
		Timeout:   5 * time.Second,
		Retries:   1,
	}
	require.NoError(t, client.Connect())
	t.Cleanup(func() { _ = client.Close() })

	// ifDescr walks the three seeded interfaces.
	results, err := client.WalkAll("1.3.6.1.2.1.2.2.1.2")
	require.NoError(t, err)

	names := make([]string, 0, len(results))
	for _, pdu := range results {
		names = append(names, octets(pdu.Value))
	}
	assert.Equal(t, []string{"radio0", "eth0", "eth1"}, names)

	// The vendor RSSI object answers as an RFC 5342 OpaqueDouble.
	rssi, err := client.Get([]string{"1.3.6.1.4.1.99999.1.1.1.0"})
	require.NoError(t, err)
	require.Len(t, rssi.Variables, 1)
	assert.Equal(t, gosnmp.OpaqueDouble, rssi.Variables[0].Type)

	// The radio failure sends the simRadioLinkDown trap to the host receiver.
	require.Equal(t, 202, post(t, sim.restconfURL+"/api/simulate/radio-failure", `{"link":"radio0"}`))
	assert.Equal(t, "1.3.6.1.4.1.99999.0.1", traps.trapOID(t))
}

// octets renders an SNMP value that carries text.
func octets(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprint(v)
	}
}
