package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"
	"github.com/stretchr/testify/require"
)

// trapSocket is a UDP socket that collects the traps of a run.
type trapSocket struct {
	conn net.PacketConn
	addr string
}

// newTrapSocket binds an ephemeral loopback port.
func newTrapSocket(t *testing.T) *trapSocket {
	t.Helper()

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return &trapSocket{conn: conn, addr: conn.LocalAddr().String()}
}

// trapOID waits for one trap and returns its snmpTrapOID.0 value.
func (s *trapSocket) trapOID(t *testing.T) string {
	t.Helper()

	require.NoError(t, s.conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 65535)
	n, _, err := s.conn.ReadFrom(buf)
	require.NoError(t, err)

	packet, err := (&gosnmp.GoSNMP{Version: gosnmp.Version2c, Community: "public"}).SnmpDecodePacket(buf[:n])
	require.NoError(t, err)
	for _, v := range packet.Variables {
		if strings.TrimPrefix(v.Name, ".") != "1.3.6.1.6.3.1.1.4.1.0" {
			continue
		}
		value, ok := v.Value.(string)
		require.True(t, ok, "snmpTrapOID.0 is an ObjectIdentifier")
		return strings.TrimPrefix(value, ".")
	}
	require.Fail(t, "the trap carries no snmpTrapOID.0 varbind")
	return ""
}

// post sends a simulation request and returns its status.
func post(t *testing.T, url, body string) int {
	t.Helper()

	response, err := http.Post(url, "application/json", strings.NewReader(body))
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

// ptpState reads the PTP clock state, or returns an empty string while the
// server is not answering yet.
func ptpState(base string) string {
	response, err := http.Get(base + "/restconf/data/sim-sync:ptp/clock/state")
	if err != nil {
		return ""
	}
	defer func() { _ = response.Body.Close() }()

	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(response.Body); err != nil {
		return ""
	}
	var document map[string]any
	if err := json.Unmarshal(body.Bytes(), &document); err != nil {
		return ""
	}
	state, _ := document["sim-sync:state"].(string)
	return state
}

// metricsBody scrapes the Prometheus endpoint, or returns an empty string while
// the server is not answering yet.
func metricsBody(addr string) string {
	response, err := http.Get("http://" + addr + "/metrics")
	if err != nil {
		return ""
	}
	defer func() { _ = response.Body.Close() }()

	body := new(bytes.Buffer)
	if _, err := body.ReadFrom(response.Body); err != nil {
		return ""
	}
	return body.String()
}

// The main cross-domain scenario of .docs/desicion.md 3.6, end to end and
// without Docker: one RESTCONF request fails the radio link, the radio domain
// raises an alarm, the sync domain takes the PTP clock into holdover, the trap
// sender reports the vendor trap and the collector counts the alarm.
func TestCrossDomainRadioFailureScenario(t *testing.T) {
	isolateDefaultLogger(t)
	path := writeConfig(t, "log:\n  level: info\n")

	deps := testDeps(t)
	traps := newTrapSocket(t)
	deps.trapAddr = traps.addr

	out := &lockedBuffer{}
	done, cancel := startRun(t, context.Background(), path, out, deps)
	defer func() {
		cancel()
		<-done
	}()

	start := waitForRecord(t, out, "simulator starting")
	restconfAddr, ok := start["restconf_addr"].(string)
	require.True(t, ok)
	metricsAddr, ok := start["metrics_addr"].(string)
	require.True(t, ok)
	base := "http://" + restconfAddr

	// The seeded clock is a locked master and the link is up.
	require.Equal(t, "locked", ptpState(base))
	require.Contains(t, metricsBody(metricsAddr), `simulator_alarms_total{severity="critical",type="radio"} 0`)

	// The failure: one request produces the alarm, the holdover, the traps and
	// the metric. The radio alarm comes first, the PTP holdover transition the
	// sync domain derives from it second.
	require.Equal(t, http.StatusAccepted, post(t, base+"/api/simulate/radio-failure", `{"link":"radio0"}`))
	require.Eventually(t, func() bool { return ptpState(base) == "holdover-in-spec" },
		5*time.Second, 5*time.Millisecond, "the clock must enter holdover")
	require.Equal(t, "1.3.6.1.4.1.99999.0.1", traps.trapOID(t), "simRadioLinkDown")
	require.Equal(t, "1.3.6.1.4.1.99999.0.3", traps.trapOID(t), "simSyncHoldover")
	require.Eventually(t, func() bool {
		return strings.Contains(metricsBody(metricsAddr), `simulator_alarms_total{severity="critical",type="radio"} 1`)
	}, 5*time.Second, 5*time.Millisecond, "the alarm must be counted")

	// The restore: the cleared alarm brings the clock back and clears the
	// metric.
	require.Equal(t, http.StatusAccepted, post(t, base+"/api/simulate/radio-restore", `{"link":"radio0"}`))
	require.Eventually(t, func() bool { return ptpState(base) == "locked" },
		5*time.Second, 5*time.Millisecond, "the clock must lock again")
	require.Equal(t, "1.3.6.1.4.1.99999.0.2", traps.trapOID(t), "simRadioLinkUp")
	require.Equal(t, "1.3.6.1.4.1.99999.0.4", traps.trapOID(t), "simSyncRestored")
	require.Eventually(t, func() bool {
		return strings.Contains(metricsBody(metricsAddr), `simulator_alarms_total{severity="cleared",type="radio"} 1`)
	}, 5*time.Second, 5*time.Millisecond, "the clear must be counted")
}
