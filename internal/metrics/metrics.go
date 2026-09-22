package metrics

import (
	"context"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/DisMosGit/triadsim/internal/clock"
	"github.com/DisMosGit/triadsim/internal/event"
)

// namespace prefixes every simulator metric.
const namespace = "simulator"

// alarmTypes are the alarm families simulator_alarms_total labels.
var alarmTypes = []string{event.DomainRadio, event.DomainL2, event.DomainSync}

// alarmSeverities are the severities the domains publish. A cleared alarm keeps
// the name of the alarm it ends and is counted with severity "cleared".
var alarmSeverities = []string{"critical", "major", "cleared"}

// ptpTransitions are the G.8275.1 state pairs the sync domain emits.
// Pre-creating them keeps the counter family visible before the first
// transition.
var ptpTransitions = [][2]string{
	{"freerun", "acquiring"},
	{"acquiring", "locked"},
	{"locked", "holdover-in-spec"},
	{"holdover-in-spec", "holdover-out-of-spec"},
	{"holdover-in-spec", "locked"},
	{"holdover-out-of-spec", "locked"},
}

// Metrics is the simulator's Prometheus instrumentation. Every instance owns a
// private registry, so tests and multiple runs never collide on the default
// registry.
type Metrics struct {
	registry      *prometheus.Registry
	requests      *prometheus.CounterVec
	alarms        *prometheus.CounterVec
	transitions   *prometheus.CounterVec
	configChanges prometheus.Counter
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

	alarms := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "alarms_total",
		Help:      "Alarm events, by domain and severity; a clear is counted with severity \"cleared\".",
	}, []string{"type", "severity"})
	for _, domain := range alarmTypes {
		for _, severity := range alarmSeverities {
			alarms.WithLabelValues(domain, severity)
		}
	}

	transitions := prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "ptp_state_transitions_total",
		Help:      "PTP clock state transitions, by the state left and entered.",
	}, []string{"from", "to"})
	for _, pair := range ptpTransitions {
		transitions.WithLabelValues(pair[0], pair[1])
	}

	configChanges := prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "config_changes_total",
		Help:      "Commits that changed the running configuration.",
	})

	registry.MustRegister(uptime, requests, alarms, transitions, configChanges)

	return &Metrics{
		registry:      registry,
		requests:      requests,
		alarms:        alarms,
		transitions:   transitions,
		configChanges: configChanges,
	}
}

// ObserveSNMPRequest counts one handled SNMP request. op is the lowercase
// operation name: get, getnext, getbulk or set.
func (m *Metrics) ObserveSNMPRequest(op string) {
	m.requests.WithLabelValues(op).Inc()
}

// ObserveEvent counts one bus event: an alarm, a PTP state transition or a
// configuration change. Every other event is ignored.
func (m *Metrics) ObserveEvent(e event.Event) {
	switch e.Type {
	case event.TypeAlarmRaised, event.TypeAlarmCleared:
		if e.Domain == "" {
			return
		}
		m.alarms.WithLabelValues(e.Domain, e.Severity).Inc()
	case event.TypeStateTransition:
		if e.Domain != event.DomainSync || e.From == "" || e.To == "" {
			return
		}
		m.transitions.WithLabelValues(e.From, e.To).Inc()
	case event.TypeConfigChanged:
		m.configChanges.Inc()
	}
}

// Run observes every event of a bus subscription until ctx is cancelled or the
// subscription is closed.
func (m *Metrics) Run(ctx context.Context, events <-chan event.Event) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-events:
			if !ok {
				return
			}
			m.ObserveEvent(e)
		}
	}
}

// Handler serves the registry in the Prometheus text exposition format.
func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}
