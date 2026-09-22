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
    Timestamp time.Time // set by the publisher, normally from its injected clock.Clock
}
```

| Type | Published when | Expected consumers |
|---|---|---|
| `AlarmRaised` | a domain detects an alarm condition (`radioLinkDown`, `radioLinkDegraded`, holdover expiry) | SNMP trap sender, NETCONF notification dispatcher, metrics |
| `AlarmCleared` | the condition ends | SNMP trap sender, NETCONF notification dispatcher, metrics |
| `ConfigChanged` | a commit applied candidate to running | NETCONF notification dispatcher, metrics |
| `StateTransition` | a domain state machine changed state (PTP `master` → `holdover`) | NETCONF notification dispatcher, metrics |

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
| SNMP trap sender | Phase 6 |
| Prometheus counters | Phase 6 |
| `internal/radio`, `internal/l2`, `internal/sync` reactions | Phases 4-6 |

The NETCONF dispatcher (`internal/netconf/notif`) is the first concrete consumer. It takes one
bus subscription for the whole server, renders each event once as an RFC 5277 `<notification>`
document and queues it for every subscribed session, so a client that does not read loses its
own notifications (logged as `notification dropped, session is not reading`) without delaying the
other sessions or the publisher. `Bus.Publish` stamps events that arrive without a timestamp,
which is what gives a notification its `eventTime`.

## Testing

`internal/event/bus_test.go` covers fan-out, timestamp stamping, buffer overflow and recovery,
`Unsubscribe`, `Close` idempotency and concurrent use (run with `go test -race ./internal/event`).
The bus uses `slog.Default()`, so tests capture drop records by installing a JSON handler writing
into a buffer. Timers and timestamps use `internal/clock`, never `time.Sleep`.
