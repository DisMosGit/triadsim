# Event bus

`internal/event` is the only channel between domains. Radio, L2 and sync never import each
other: a domain publishes a typed event and reacts to the events it subscribed to. The
management planes subscribe to the same bus, which is how one simulated failure becomes an SNMP
trap, a NETCONF notification and a Prometheus counter.

## Events

```go
type Event struct {
    Type      EventType // AlarmRaised, AlarmCleared, ConfigChanged, StateTransition
    Resource  string    // affected object, for example "radio0"
    Severity  string    // alarm severity; empty for non-alarm events
    Message   string    // human-readable detail
    Domain    string    // publishing domain: radio, l2 or sync
    Alarm     string    // alarm condition, e.g. radioLinkDown; empty otherwise
    From, To  string    // states a StateTransition left and entered
    Timestamp time.Time // set by the publisher, normally from its injected clock.Clock
}
```

The optional structured fields exist so consumers — the SNMP trap sender, the metrics collector —
never parse `Message` text. An alarm keeps its **name** across both events: `AlarmRaised` with
`Severity=critical|major` raises it and `AlarmCleared` with `Severity=cleared` ends it. `Domain` is
what labels an alarm family in `simulator_alarms_total`, and `From`/`To` are the state names of a
`StateTransition`. They are absent for events that do not have them, and the NETCONF notification
carries the ones it has as `<alarm>`, `<from>` and `<to>`.

| Type | Published when | Expected consumers |
|---|---|---|
| `AlarmRaised` | a domain detects an alarm condition (`radioLinkDown`, `radioLinkDegraded`, holdover expiry, broadcast storm) | SNMP trap sender, NETCONF notification dispatcher, metrics |
| `AlarmCleared` | the condition ends | SNMP trap sender, NETCONF notification dispatcher, metrics |
| `ConfigChanged` | a commit applied candidate to running | SNMP trap sender, NETCONF notification dispatcher, metrics |
| `StateTransition` | a domain state machine changed state (PTP `locked` → `holdover-in-spec`, an STP port phase) | SNMP trap sender, NETCONF notification dispatcher, metrics |

Publishers stamp `Timestamp` from their `clock.Clock`. When an event arrives with a zero
timestamp, `Bus.Publish` stamps it from the bus clock so every record on the bus is ordered.

## Bus semantics

```go
bus := event.New(event.DefaultBuffer, clock.RealClock{})
defer bus.Close()

ch := bus.Subscribe()
defer bus.Unsubscribe(ch)

bus.Publish(event.Event{Type: event.TypeAlarmRaised, Resource: "radio0", Severity: "critical"})

for e := range ch {
    // handle e; the loop ends when the bus or the subscription is closed
}
```

- **Fan-out, not work sharing.** Every subscriber has its own buffered channel and receives every
  event published after it subscribed. Two subscribers therefore observe the same event.
- **Non-blocking publish.** `Publish` never waits for a consumer. When a subscriber's buffer is
  full the event is dropped *for that subscriber only* and a warning is logged
  (`event dropped, subscriber buffer full`, with `type`, `resource` and `buffer` attributes). A
  slow consumer can neither stall the publisher nor delay other subscribers. Because publishing
  never blocks it takes no `context.Context`.
- **Buffer size.** `New(bufferSize, clk)` uses `bufferSize` per subscriber; a non-positive value
  means `DefaultBuffer` (64). A nil clock means `clock.RealClock{}`.
- **Lifecycle.** `Subscribe` returns a receive-only channel; `Unsubscribe` closes it and
  `Close` closes every channel and marks the bus closed. `Close` is idempotent, `Publish` after
  `Close` is a no-op (logged at debug level), and `Subscribe` after `Close` returns an
  already-closed channel so `for range` terminates immediately. Closing the bus is how `start`
  shuts subscribers down on SIGINT/SIGTERM.
- **Concurrency.** The subscriber set is guarded by a mutex, so publishing, subscribing,
  unsubscribing and closing may happen from different goroutines. Consumers must respect their
  own context: draining a channel after cancellation is the consumer's responsibility.

## Consumers

| Consumer | Status |
|---|---|
| `internal/netconf/notif` | implemented (Phase 3): the NETCONF notification dispatcher |
| SNMP trap sender (`internal/snmp`) | implemented (Phase 6.3) |
| Prometheus counters (`internal/metrics`) | implemented (Phase 6.4) |
| `internal/sync` reactions | implemented (Phase 6.5): a radio alarm drives the PTP clock |

The NETCONF dispatcher (`internal/netconf/notif`) takes one bus subscription for the whole server,
renders each event once as an RFC 5277 `<notification>` document and queues it for every subscribed
session, so a client that does not read loses its own notifications (logged as `notification
dropped, session is not reading`) without delaying the other sessions or the publisher.

The SNMP trap sender (`internal/snmp/trap.go`) also owns one subscription for the whole server: it
maps an event to a vendor trap OID, resolves the payload through the router and writes one
fire-and-forget SNMPv2-Trap-PDU to `snmp.trap-host:snmp.trap-port`; an event that is not a trap is
dropped.

The metrics collector (`internal/metrics`) turns a subscription into
`simulator_alarms_total`, `simulator_ptp_state_transitions_total` and
`simulator_config_changes_total`.

`internal/sync` is the consumer that closes the cross-domain loop: `Run` drains a subscription taken
in `New` (so an alarm that fires before the loop starts is not lost) and reacts to a raised
`radioLinkDown` as a lost reference — `locked` → `holdover-in-spec` — and to the cleared alarm as a
restored one, which locks the clock again. Subscribing in `New` follows the dispatcher's precedent:
the subscription covers the whole window from construction to shutdown.

`Bus.Publish` stamps events that arrive without a timestamp, which is what gives a notification its
`eventTime`.

## Testing

`internal/event/bus_test.go` covers fan-out, timestamp stamping, buffer overflow and recovery,
`Unsubscribe`, `Close` idempotency and concurrent use (run with `go test -race ./internal/event`).
The bus uses `slog.Default()`, so tests capture drop records by installing a JSON handler writing
into a buffer. Timers and timestamps use `internal/clock`, never `time.Sleep`.
