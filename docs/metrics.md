# Metrics

The simulator exposes Prometheus metrics over HTTP on `metrics.port` (default `:9090`):

```bash
curl -s http://localhost:9090/metrics | grep '^simulator_'
```

Every `metrics.Metrics` owns a **private registry**, so two instances in one process — the tests, or
two runs — never collide on the default registry, and the exposition contains only the simulator's
own metrics. `start` builds one collector, serves `Handler()` and feeds it from one EventBus
subscription: `Run(ctx, events)` turns events into counters, so a domain publishes an event and the
metric follows.

## Metrics

| Metric | Type | Labels | Meaning |
|---|---|---|---|
| `simulator_uptime_seconds` | gauge | — | Seconds since the simulator started, read from the injected clock |
| `simulator_snmp_requests_total` | counter | `op` | SNMP requests handled: `get`, `getnext`, `getbulk`, `set` |
| `simulator_alarms_total` | counter | `type`, `severity` | Alarm events |
| `simulator_ptp_state_transitions_total` | counter | `from`, `to` | PTP clock state transitions |
| `simulator_config_changes_total` | counter | — | Commits that changed the running configuration |

The fixed label combinations are pre-created, so a family is visible at zero before its first event:
`op` for the four SNMP operations, `type` × `severity` for the alarm families and the canonical PTP
transition pairs.

## Alarm labels

`type` is the **alarm family**, not the alarm name, so one counter answers "how many radio alarms
did this device raise?":

| `type` | `Event.Domain` | Published by |
|---|---|---|
| `radio` | `radio` | `internal/radio` — `radioLinkDown`, `radioLinkDegraded` |
| `l2` | `l2` | `internal/l2` — broadcast storm |
| `sync` | `sync` | `internal/sync` — holdover expiry |

`severity` is the severity carried by the event: `critical` for a lost radio link, `major` for a
degradation or a holdover expiry, `cleared` for a cleared alarm. The alarm keeps its name across
both events — `Event.Alarm` says which alarm, `Event.Type` says whether it was raised or cleared —
and **both** are counted, so `simulator_alarms_total{severity="cleared"}` counts the clears. An
alarm event without a domain is ignored, because it cannot be attributed to a family.

## State transitions

`simulator_ptp_state_transitions_total` counts only the G.8275.1 clock states of
`internal/sync` (`Domain: "sync"`); the L2 STP transitions use a different state vocabulary and are
reported as notifications and traps, not as this metric. `from` and `to` are the states the
transition left and entered, for example:

```
simulator_ptp_state_transitions_total{from="locked",to="holdover-in-spec"} 1
```

## Example

One `POST /api/simulate/radio-failure` raises the alarm, takes the clock into holdover and produces:

```
simulator_alarms_total{severity="cleared",type="radio"} 0
simulator_alarms_total{severity="critical",type="radio"} 1
simulator_alarms_total{severity="major",type="radio"} 0
simulator_ptp_state_transitions_total{from="locked",to="holdover-in-spec"} 1
simulator_config_changes_total 0
```

See [demo.md](demo.md) for the whole scenario and [eventbus.md](eventbus.md) for the event contract
behind the counters.
