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
- `internal/netconf`: capabilities `confirmed-commit:1.0` and `notification:1.0`, notification writes serialized with the RPC replies through the session's message writer, and the session lifecycle releasing the notification subscription and reverting an unconfirmed confirmed commit.- Golden NETCONF transcript `testdata/netconf/confirmed-commit.xml`; the two existing transcripts were regenerated for the new hello capabilities.
- `internal/datatree`: shared data-tree engine extracted from the NETCONF operations — `Document`/`Node` read and edit semantics, RFC 6241 error tags with a `Validation` flag, and the leaf parse/format helpers both management planes codec through.
- `internal/model`: Phase 4 L2 containers — `VLAN` with `creatable` member ports and QinQ (`qinq`, `s-tag-vid`, `c-tag-handling`), `MACTable` with aging/limits/entries, `STPState` with the classic and RSTP port-state vocabularies, `LLDPConfig` with static neighbours — plus their `Validate` rules and the Phase 4 seed (VLAN 100, one permanent MAC entry, RSTP bridge state, LLDP defaults).
- `internal/router`: `creatable:"true"` lists whose missing instances are synthesised, snapshot hydration that prunes the list entries the datastore does not hold, `Router.Snapshot` for the domain packages, module mapping (`sim-device`, `sim-radio-link`, `sim-l2-switching`, `sim-sync`) shared by the protocol codecs, and the Phase 4 MIB tables (ifTable/ifXTable counters, BRIDGE-MIB, Q-BRIDGE-MIB VLAN names) with a new `Counter64` SNMP type.
- `internal/restconf`: chi-based RESTCONF server on `:8080` serving `/restconf/data` (GET, PUT, PATCH, POST, DELETE) for the running, candidate and startup datastores with `application/yang-data+json` and `+xml` media negotiation, config/non-config content selection, the `ietf-restconf:errors` document and the RFC 8040 status-code mapping; `/restconf/operations` and `/restconf/streams` answer 501.
- `internal/l2`: the L2 switching domain — a datastore-backed `Manager` with VLAN and QinQ configuration, a VLAN-aware MAC forwarding database with learning and clock-driven aging, the simplified five-phase STP/RSTP machine with root election and event-bus transitions, static LLDP neighbours with a periodic TTL refresh, IF-MIB interface counters with a frame-forwarding traffic simulation, and the broadcast-storm injector with rate limiting and alarms.
- `POST /api/simulate/l2-storm`: injects a broadcast storm on a port, floods the accepted frames, counts the excess as discards and raises an alarm above the configured rate.
- `start` now runs the L2 domain alongside SNMP, NETCONF, RESTCONF and `/metrics`.
- Documentation: `docs/protocols/L2.md` and `docs/protocols/RESTCONF.md` rewritten for the implementation.
- `internal/model`: Phase 5 synchronization objects wired into `Device` — `ptp/clock` (`mode`, `domain`, `priority1`, `priority2`, `clock-class`, `clock-accuracy`, `holdover-timeout` plus the read-only `state`, `offset`, `jitter`) and `synce` (`enabled`, the read-only `selected-source`/`selected-ql`/`selected-extended-ql`, the seeded `interfaces/interface[name=…]` list and the nested `esmc` container) — with their `Validate` rules and the Phase 5 seed (master clock locked on domain 24, 300 s holdover, eth0/QL-PRC selected).
- `internal/sync`: the synchronization domain — a `Manager` (`Run`/`Tick`) that adopts the persisted clock state, re-runs the SyncE source selection every tick and simulates PTP offset and jitter; the G.8275.1 PTP state machine (`Handle`, `SyncLoss`) with holdover timing on the injected `clock.Clock` and `StateTransition`/`AlarmRaised`/`AlarmCleared` events; and the QL/eSSM mapping (`SSMCode`, `QLFromSSM`, `ExtendedSSMCode`, `Rank`, `Message`).
- `internal/router`: vendor synchronization OIDs `1.3.6.1.4.1.99999.2.1.*` — PTP state/offset/domain/priority1/jitter, the selected SyncE quality levels and the SyncE interface table indexed by ifIndex — with the PTP-state, SSM/eSSM and TruthValue converters.
- `internal/snmp`: `fromPDU` accepts the `uint` value `gosnmp` decodes a received Gauge32 into, which the new writable `ptp/clock/domain` object needs.
- `internal/restconf`: `POST /api/simulate/sync-loss` (pulled forward from Phase 6.1) drives the PTP clock into holdover through the new `SyncSimulator` seam; `start` wires the sync manager into it.
- `start` now builds and runs the sync domain alongside the L2 domain.
- Documentation: `docs/protocols/PTP.md` and `docs/protocols/SYNCE.md` rewritten for the implementation, and `docs/architecture.md`, `docs/protocols/SNMP.md`, `docs/protocols/RESTCONF.md` and `docs/config.md` updated for the sync subtree, its OIDs and the simulation endpoint.
- `internal/event`: optional structured fields on `Event` — `Domain` (the publishing domain, the alarm family of the metrics), `Alarm` (the alarm condition, kept across the raised and the cleared event) and `From`/`To` (the states a `StateTransition` left and entered) — so the trap sender and the metrics collector never parse message text. NETCONF notifications carry `alarm`/`from`/`to` as optional elements.
- `internal/model`: the read-only `radio-link/link-state` leaf (`up`, `degraded`, `down`), registered as the vendor object `simRadioLinkState` (`1.3.6.1.4.1.99999.1.1.8`).
- `internal/radio`: the RRL domain — the ITU-R P.530 free-space link budget, the 1 dB ATPC control step, the adaptive ACM profile selection, and the up/degraded/down link state machine that raises `radioLinkDown` (critical, below −85 dBm) and `radioLinkDegraded` (major, fade margin below 6 dB) with hysteresis on the event bus. `Run`/`Tick` recalculate every link on the injected clock; `RadioFailure`/`RadioRestore` inject and clear a fade.
- `internal/restconf`: `POST /api/simulate/radio-failure` (optional `link` and `fade-db`) and `POST /api/simulate/radio-restore` over the new `RadioSimulator` seam; `start` wires and runs the radio domain.
- `internal/snmp`: the trap sender — one bus subscription, the vendor trap OIDs `1.3.6.1.4.1.99999.0.1`–`.0.6`, the mandatory `sysUpTime.0`/`snmpTrapOID.0` varbinds and the per-trap payload (radio RSSI, fade margin and `ifDescr`; PTP state), sent fire-and-forget. The `snmp.trap-host` key (default `127.0.0.1`) configures the destination, and `start` also keeps `system-info/uptime` in step with the wall clock.
- `internal/metrics`: `simulator_alarms_total{type,severity}` (labelled by alarm family), `simulator_ptp_state_transitions_total{from,to}` and `simulator_config_changes_total`, fed from a bus subscription.
- `internal/sync`: the cross-domain reaction — `Run` drains the bus subscription taken in `New`, so a raised `radioLinkDown` takes the PTP clock into holdover and the cleared alarm locks it again. The sync domain still never imports the radio domain.
- `internal/cli`: `alarm inject --type radioLinkDown|radioLinkDegraded|radioLinkUp --link <name>` drives the simulation API of a running simulator; `dump --format json|xml --content config|nonconfig|all` fetches its datastore over RESTCONF; `config validate --file <path>`; and `version`.
- Tests: `internal/cli/crossdomain_test.go` reproduces the whole scenario in process (one RESTCONF request → alarm → PTP holdover → trap on a real UDP socket → metric), and `test/integration` (build tag `integration`, Docker) runs the compiled simulator in a container against real NETCONF, SNMP and HTTP clients.
- Documentation: `docs/demo.md` (the cross-domain scenario step by step), `docs/metrics.md` and `docs/cli.md`; `docs/protocols/RADIO-RRL.md`, `SNMP.md`, `NETCONF.md` and `RESTCONF.md` updated for the implemented radio domain, traps, notification payload and simulation endpoints.

### Notes

- Phase 6 is complete: one `POST /api/simulate/radio-failure` produces the radio alarm, the PTP
  holdover, the SNMP trap, the NETCONF notification and the Prometheus counter, which is the
  cross-domain scenario of `.docs/desicion.md` §3.6. `simulator alarm inject`, `dump`,
  `config validate` and `version` are available, and `go test -tags=integration ./...` drives the
  containerised binary over the real protocols. See [ROADMAP.md](ROADMAP.md).
- Phase 5 is complete: the PTP clock is a locked master on `ptp/clock`, `POST /api/simulate/sync-loss` moves it to `holdover-in-spec` (and its holdover timeout to `holdover-out-of-spec` with an `AlarmRaised`), the transitions are published on the EventBus, and RESTCONF/SNMP read the state (`/restconf/data/sim-sync:ptp/clock/state`, `1.3.6.1.4.1.99999.2.1.1.0`). Phase 4 delivered L2 and RESTCONF, Phase 3 NETCONF confirmed commit and notifications, Phase 2 the NETCONF base and Phase 1 the SNMP walk with `/metrics`. See [ROADMAP.md](ROADMAP.md).
