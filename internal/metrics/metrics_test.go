package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
)

// scrape runs the handler and returns the response body.
func scrape(t *testing.T, m *Metrics) string {
	t.Helper()

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	m.Handler().ServeHTTP(recorder, request)

	require.Equal(t, http.StatusOK, recorder.Code)
	return recorder.Body.String()
}

func TestEndpointExposesUptimeAndRequests(t *testing.T) {
	clk := clock.NewFakeClock()
	m := New(clk, clk.Now())

	clk.Advance(90 * time.Second)
	m.ObserveSNMPRequest("get")
	m.ObserveSNMPRequest("get")
	m.ObserveSNMPRequest("getbulk")

	body := scrape(t, m)

	assert.Contains(t, body, "simulator_uptime_seconds 90")
	assert.Contains(t, body, `simulator_snmp_requests_total{op="get"} 2`)
	assert.Contains(t, body, `simulator_snmp_requests_total{op="getbulk"} 1`)
}

func TestEndpointBeforeAnyRequest(t *testing.T) {
	clk := clock.NewFakeClock()
	m := New(clk, clk.Now())

	body := scrape(t, m)

	assert.Contains(t, body, "simulator_uptime_seconds")
	assert.Contains(t, body, "simulator_snmp_requests_total")
}

func TestNilClockUsesRealTime(t *testing.T) {
	m := New(nil, time.Now().Add(-time.Second))

	body := scrape(t, m)

	assert.Contains(t, body, "simulator_uptime_seconds")
}

func TestRegistriesAreIndependent(t *testing.T) {
	clk := clock.NewFakeClock()
	first := New(clk, clk.Now())
	second := New(clk, clk.Now())

	first.ObserveSNMPRequest("set")

	assert.Contains(t, scrape(t, second), `simulator_snmp_requests_total{op="set"} 0`)
	assert.Contains(t, scrape(t, first), `simulator_snmp_requests_total{op="set"} 1`)
}

func TestObserveAlarmEvents(t *testing.T) {
	clk := clock.NewFakeClock()
	m := New(clk, clk.Now())

	m.ObserveEvent(event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "radio0",
		Severity: "critical",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	})
	m.ObserveEvent(event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "radio0",
		Severity: "critical",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	})
	m.ObserveEvent(event.Event{
		Type:     event.TypeAlarmCleared,
		Resource: "radio0",
		Severity: "cleared",
		Domain:   event.DomainRadio,
		Alarm:    event.AlarmRadioLinkDown,
	})
	m.ObserveEvent(event.Event{
		Type:     event.TypeAlarmRaised,
		Resource: "l2/storm/eth0",
		Severity: "major",
		Domain:   event.DomainL2,
		Alarm:    event.AlarmL2Storm,
	})

	// An alarm event without a domain cannot be attributed to a family.
	m.ObserveEvent(event.Event{Type: event.TypeAlarmRaised, Severity: "major"})

	body := scrape(t, m)
	assert.Contains(t, body, `simulator_alarms_total{severity="critical",type="radio"} 2`)
	assert.Contains(t, body, `simulator_alarms_total{severity="cleared",type="radio"} 1`)
	assert.Contains(t, body, `simulator_alarms_total{severity="major",type="l2"} 1`)
	assert.Contains(t, body, `simulator_alarms_total{severity="critical",type="sync"} 0`)
}

func TestObserveStateTransitionsAndConfigChanges(t *testing.T) {
	clk := clock.NewFakeClock()
	m := New(clk, clk.Now())

	m.ObserveEvent(event.Event{
		Type:   event.TypeStateTransition,
		Domain: event.DomainSync,
		From:   "locked",
		To:     "holdover-in-spec",
	})
	// An L2 transition is not a PTP transition.
	m.ObserveEvent(event.Event{
		Type:   event.TypeStateTransition,
		Domain: event.DomainL2,
		From:   "discarding",
		To:     "forwarding",
	})
	m.ObserveEvent(event.Event{Type: event.TypeConfigChanged, Resource: "device"})

	body := scrape(t, m)
	assert.Contains(t, body, `simulator_ptp_state_transitions_total{from="locked",to="holdover-in-spec"} 1`)
	assert.Contains(t, body, "simulator_config_changes_total 1")
	assert.NotContains(t, body, `from="discarding"`)
}

// Run turns a bus subscription into the counters and returns when the channel
// closes.
func TestRunObservesTheBus(t *testing.T) {
	bus := event.New(event.DefaultBuffer, clock.RealClock{})
	t.Cleanup(bus.Close)

	clk := clock.NewFakeClock()
	m := New(clk, clk.Now())
	events := bus.Subscribe()
	done := make(chan struct{})
	go func() {
		m.Run(t.Context(), events)
		close(done)
	}()

	bus.Publish(event.Event{Type: event.TypeConfigChanged, Resource: "device"})
	require.Eventually(t, func() bool {
		return strings.Contains(scrape(t, m), "simulator_config_changes_total 1")
	}, 5*time.Second, 5*time.Millisecond)

	bus.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the bus closed")
	}
}
