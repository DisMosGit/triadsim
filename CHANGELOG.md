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
- `internal/clock`: injectable `Clock`/`Timer` interfaces with `RealClock` and `FakeClock` test implementation.
- `cmd/simulator` cobra entrypoint with the `start` command that loads config, sets up logging and runs the EventBus.
- Tooling: `scripts/check.sh`, `scripts/demo.sh` (stub) and a `Makefile` with `build`, `test`, `lint`, `run`, `demo`, `clean`.
- Documentation: `docs/architecture.md`, `docs/store.md`, `docs/eventbus.md`, `docs/config.md`, `docs/adr/0001-record-architecture-decisions.md`, `docs/adr/template.md`.

### Notes

- Management planes (SNMP v2c, NETCONF, RESTCONF, optional gNMI) and the radio/L2/sync domains land in Phases 1–7; see [ROADMAP.md](ROADMAP.md).
