//go:build integration

package integration

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The main cross-domain scenario of .docs/desicion.md 3.6 runs against the
// containerised simulator: one RESTCONF request produces the radio alarm, the
// PTP holdover, the vendor trap and the Prometheus counter.
func TestCrossDomainScenario(t *testing.T) {
	traps := newTrapReceiver(t)
	sim := startSimulator(t, traps.port)

	assert.Equal(t, "locked", ptpState(sim.restconfURL))

	// The failure: the radio alarm first, the holdover transition it causes
	// second.
	require.Equal(t, 202, post(t, sim.restconfURL+"/api/simulate/radio-failure", `{"link":"radio0"}`))
	require.Eventually(t, func() bool { return ptpState(sim.restconfURL) == "holdover-in-spec" },
		10*time.Second, 50*time.Millisecond, "the clock must enter holdover")
	assert.Equal(t, "1.3.6.1.4.1.99999.0.1", traps.trapOID(t), "simRadioLinkDown")
	assert.Equal(t, "1.3.6.1.4.1.99999.0.3", traps.trapOID(t), "simSyncHoldover")
	require.Eventually(t, func() bool {
		return strings.Contains(getBody(sim.metricsURL+"/metrics"),
			`simulator_alarms_total{severity="critical",type="radio"} 1`)
	}, 10*time.Second, 50*time.Millisecond, "the alarm must be counted")

	// The restore: the cleared alarm brings the clock back.
	require.Equal(t, 202, post(t, sim.restconfURL+"/api/simulate/radio-restore", `{"link":"radio0"}`))
	require.Eventually(t, func() bool { return ptpState(sim.restconfURL) == "locked" },
		10*time.Second, 50*time.Millisecond, "the clock must lock again")
	assert.Equal(t, "1.3.6.1.4.1.99999.0.2", traps.trapOID(t), "simRadioLinkUp")
	assert.Equal(t, "1.3.6.1.4.1.99999.0.4", traps.trapOID(t), "simSyncRestored")
	require.Eventually(t, func() bool {
		return strings.Contains(getBody(sim.metricsURL+"/metrics"),
			`simulator_alarms_total{severity="cleared",type="radio"} 1`)
	}, 10*time.Second, 50*time.Millisecond, "the clear must be counted")
}
