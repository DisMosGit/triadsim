package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/DisMosGit/triadsim/internal/clock"
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
