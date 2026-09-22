# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Repository skeleton: `README.md`, `CONTRIBUTING.md`, `AGENTS.md`, `ROADMAP.md`, MIT `LICENSE.md`.
- Go module `github.com/DisMosGit/triadsim` (Go 1.27+) and the modular-monolith package layout under `cmd/` and `internal/`.
- Approved dependency stack fixed in `go.mod` (`gosnmp`, `x/crypto/ssh`, `chi/v5`, `cobra`, `prometheus/client_golang`, `testify`, `testcontainers-go`, `yaml.v3`).
- `internal/config`: YAML configuration with `Default`, `Load` and `Validate`, plus `configs/default.yaml`.
- `internal/log`: `log/slog` JSON logging to stderr with level parsing and `Debug`/`Info`/`Warn`/`Error` helpers.
- `internal/event`: channel-based `EventBus` with `AlarmRaised`, `AlarmCleared`, `ConfigChanged` and `StateTransition` events.
- `internal/store`: `Store` interface for the running/candidate/startup datastores.
- `internal/model`: managed objects for the radio link (`RadioLink`, `ATPC`, `ACM`, `ModProfile`, `LinkBudget`), L2 (`Interface`, `InterfaceCounters`, `VLAN`, `MACEntry`, `STPState`, `LLDPNeighbor`), synchronization (`PTPClock`, `SyncEState`, `QL`, `ESMC`) and the device (`Device`, `SystemInfo`), each with `Validate()` and `path`/`xml`/`json` tags.
- `internal/store`: in-memory `Memory` implementation of running/candidate/startup with `Diff`, `Commit` (injected `Validator`, atomic apply and persist), `Rollback` and `LoadStartup`, plus the versioned JSON `Save`/`Load` used for `startup.json`.
- `internal/clock`: injectable `Clock`/`Timer` interfaces with `RealClock` and `FakeClock` test implementation.
- `internal/router`: path parser for the `a/b[c=d]/e` grammar, reflective navigation over the model `path` tags, MIB-II and vendor OID tables, `Get`/`Set`/`Delete`/`List`/`Dispatch`, SNMP `Bindings` and the `store.Validator` that validates a candidate against a model copy.
- `internal/model`: `DefaultDevice()` seed with `radio0`, `eth0`, `eth1` and the 12-row ACM profile table.
- `internal/snmp`: SNMP v2c agent answering `Get`, `GetNext` and `GetBulk` for community `public`, plus `SetRequest` with MIB access checks and whole-device validation; traps arrive in Phase 6.
- `internal/metrics`: `simulator_uptime_seconds` and `simulator_snmp_requests_total{op}` on a private Prometheus registry served from `/metrics`.
- `cmd/simulator` cobra entrypoint with the `start` command that loads config, sets up logging and runs the EventBus.
- `start` now builds the store, router and seed device, loads `startup.json`, seeds on the first boot and serves the SNMP and Prometheus endpoints until SIGINT/SIGTERM.
- Tooling: `scripts/check.sh`, `scripts/demo.sh` (stub) and a `Makefile` with `build`, `test`, `lint`, `run`, `demo`, `clean`.
- Documentation: `docs/README.md` (index), `docs/architecture.md`, `docs/store.md`, `docs/eventbus.md`, `docs/config.md`, `docs/protocols/SNMP.md`, `docs/adr/0001-record-architecture-decisions.md`, `docs/adr/template.md`.
- `internal/router`: type-driven schema tree (`Node`, `Children`) that maps protocol element trees onto router paths.
- `internal/netconf`: SSH subsystem `netconf` with an ephemeral ed25519 host key and no authentication, one NETCONF session per channel with a monotonic session-id.
- `internal/netconf`: `<hello>` exchange, capability advertisement (`base:1.0`, `base:1.1`, `candidate`, `writable-running`) and framing negotiation.
- `internal/netconf`: end-of-message and chunked framing with hex chunk sizes, size limits, malformed-frame detection and first-byte sniffing of the client hello.
- `internal/netconf`: `<rpc>`/`<rpc-reply>`/`<rpc-error>` handling, operation dispatch and the RFC 6241 error-tag mapping.
- `internal/netconf/ops`: `get-config` for running/candidate/startup with subtree filters (key, subtree and content match) and module namespaces on output.
- `internal/netconf/ops`: `edit-config` with `merge`, `replace`, `create`, `delete`, `remove`, `<default-operation>` and `nc:operation` attributes, validated as a whole before anything is written.
- `internal/netconf/ops`: `commit` (validate, apply to running, persist `startup.json`, publish `ConfigChanged`) and `discard-changes`.
- `start` now also serves the NETCONF subsystem; the default port is the unprivileged `1830` instead of the privileged IANA `830`.
- Golden NETCONF transcripts in `testdata/netconf/` replayed by `internal/netconf/golden_test.go`; regenerate with `go test ./internal/netconf -update`.
- Documentation: `docs/protocols/NETCONF.md` rewritten for the implemented operations, framing, data model, error tags and a full walkthrough.
- `internal/store`: `Snapshot` (a detached copy of running) and `Restore` (put a snapshot back as running and startup, leaving candidate untouched), the rollback primitive of a confirmed commit.
- `internal/netconf/ops`: confirmed commit (`<commit><confirmed/><confirm-timeout>`): the timeout defaults to 600 s, `FakeClock` drives it in tests, a confirming `<commit/>` cancels the rollback, and the session that issued the unconfirmed commit reverts it when it ends. `Deps` gained `SessionID` and `Confirmed`.
- `internal/netconf/notif`: RFC 5277 `create-subscription` for the `sim-events` stream, the `<notification>` document (`<eventTime>` plus a `sim-events:event` payload) and a dispatcher that fans EventBus events out to every subscribed session, one buffered channel per session.
- `internal/netconf`: capabilities `confirmed-commit:1.0` and `notification:1.0`, notification writes serialized with the RPC replies through the session's message writer, and the session lifecycle releasing the notification subscription and reverting an unconfirmed confirmed commit.
- Golden NETCONF transcript `testdata/netconf/confirmed-commit.xml`; the two existing transcripts were regenerated for the new hello capabilities.

### Notes

- Phase 3 is complete: `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>` reverts the configuration unless a `<commit/>` confirms it in time (or the issuing session ends first), and a session that sends `<create-subscription>` receives a `<notification>` for every EventBus event. Phase 2 delivered the NETCONF base (`ssh -p 1830 -s admin@localhost netconf`, `edit-config` → `commit` → `get-config`), and Phase 1 the SNMP walk (`ifDescr` plus the vendor RSSI OID) and `/metrics`. RESTCONF, gNMI and the radio/L2/sync domain logic land in Phases 4–7. See [ROADMAP.md](ROADMAP.md).
