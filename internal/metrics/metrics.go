package metrics

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/DisMosGit/triadsim/internal/clock"
)

// namespace prefixes every simulator metric.
const namespace = "simulator"

// Metrics is the simulator's Prometheus instrumentation. Every instance owns a
// private registry, so tests and multiple runs never collide on the default
// registry.
type Metrics struct {
	registry *prometheus.Registry
	requests *prometheus.CounterVec
}

// New builds the simulator metrics. start anchors simulator_uptime_seconds; a
// nil clock means clock.RealClock{}.
func New(clk clock.Clock, start time.Time) *Metrics {
	if clk == nil {
		clk = clock.RealClock{}
	}

	registry := prometheus.NewRegistry()
	uptime := prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "uptime_seconds",
		Help:      "Seconds since the simulator started.",
	}, func() float64 {
		return clk.Now().Sub(start).Seconds()
	})
	requests := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "snmp_requests_total",
		Help:      "SNMP requests handled, by operation.",
	}, []string{"op"})
	// Pre-create the known operations so the family is visible at zero.
	for _, op := range []string{"get", "getnext", "getbulk", "set"} {
		requests.WithLabelValues(op)
	}
	registry.MustRegister(uptime, requests)

	return &Metrics{registry: registry, requests: requests}
}

// ObserveSNMPRequest counts one handled SNMP request. op is the lowercase
// operation name: get, getnext, getbulk or set.
func (m *Metrics) ObserveSNMPRequest(op string) {
	m.requests.WithLabelValues(op).Inc()
}

// Handler serves the registry in the Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
