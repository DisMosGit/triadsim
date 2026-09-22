# 3. One modular-monolith binary, not services

- **Status:** Accepted
- **Date:** 2026-09-22
- **Deciders:** TriadSim maintainers

## Context

TriadSim simulates one telecom device that combines a radio link, L2 switching and
synchronization. The three domains share managed objects (an interface is an L2 port and a sync
port), share a datastore, and react to each other: a radio failure must push the PTP clock into
holdover and raise an alarm, and a broadcast storm is an L2 alarm that the metrics and trap planes
report.

The domains could be separate processes or services, communicating over a network or a message
broker. They could also be packages inside one binary, communicating over an in-process bus.

The project constraints are explicit (`AGENTS.md`, `docs/architecture.md`): pure Go, no CGO, no
sidecar processes, no external services, one binary — the point of the simulator is to be started
with one command on a laptop or in CI.

## Decision

The simulator is a modular monolith: one `cmd/simulator` binary that wires six packages together.

- One state: `internal/store` holds the running, candidate and startup datastores; every plane
  reads and writes the same store through `internal/router`.
- One event channel: `internal/event` is an in-process bus on buffered channels. **Domains do not
  import each other**; a cross-domain reaction is a subscription to an event type. `internal/sync`
  reacts to `radioLinkDown` without importing `internal/radio`.
- One process: `start` constructs the domains and the management planes, binds their ports
  (SNMP 1161, traps 1162, NETCONF 1830, RESTCONF 8080, metrics 9090, optional gNMI 9339), runs
  their loops, and shuts everything down on SIGINT/SIGTERM.

Dependency direction is one-way: domains depend on `router`, `store`, `event` and `clock`;
management planes depend on `router`, `store`, `event` and the shared `internal/datatree` engine;
nothing depends on a management plane except `internal/cli`, which wires them.

## Consequences

- The whole device state is consistent by construction: there is one store and one mutex per
  domain, and no replication or eventual consistency to reason about.
- Tests are cheap: a domain test constructs a store, a router and a `FakeClock`, and drives the
  domain directly. The cross-domain scenario is a unit test (plus a container integration test),
  not a multi-service orchestration.
- Cross-domain coupling is explicit and reviewable: the event types are the interface.
- The event bus is best-effort: a subscriber with a full buffer drops the event, and the drop is
  logged. That is acceptable for a simulator whose events drive alarms and metrics, not state.
- A domain cannot be scaled or restarted independently, and a panic in one domain takes the
  process down. Accepted: the simulator is not a high-availability system.
- The bus itself is not persisted or replayed; NETCONF replay is out of scope
  (`docs/protocols/NETCONF.md`).

## Alternatives

- **Microservices (one process per domain) with a broker.** Rejected: it contradicts the
  single-binary goal, adds a broker dependency, and turns a deterministic cross-domain transition
  into an asynchronous, flaky test.
- **Separate binaries that share a store file.** Rejected: file locking and partial writes would
  make the running datastore inconsistent, and `startup.json` is a persistence format, not an IPC
  mechanism.
- **One package, no boundaries.** Rejected: the three domains are genuinely independent, and the
  event bus plus one-way dependencies are what keep a sync change from rippling through L2.
- **Domain code as Go plugins (`plugin` package).** Rejected: CGO-free builds and the closed
  dependency list rule it out, and there is nothing to gain over in-process packages.
