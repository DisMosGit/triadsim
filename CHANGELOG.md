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

### Notes

- Phase 1 is complete: the SNMP walk returns `ifDescr` and the vendor RSSI OID, and `/metrics` serves the simulator counters. NETCONF, RESTCONF, gNMI and the radio/L2/sync domain logic land in Phases 2–7. See [ROADMAP.md](ROADMAP.md).
