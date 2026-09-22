// Package metrics exposes the Prometheus instrumentation of the simulator —
// uptime, SNMP request, alarm, PTP state-transition and configuration-change
// counters — over an HTTP /metrics endpoint. Run turns a bus subscription into
// the counters, so a domain publishes an event and the metric follows.
package metrics
