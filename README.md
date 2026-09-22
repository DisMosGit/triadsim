# TriadSim

**TriadSim** is a pure-Go, single-binary simulator of a telecom device that combines three
domains on one managed device:

- **Radio link (RRL)** — link budget, RSSI, ATPC, ACM, modulation profiles, fade margin, capacity.
- **L2 switching** — VLAN (802.1Q, QinQ), MAC table, STP/RSTP, LLDP, interface counters, broadcast storms.
- **Synchronization** — PTP (IEEE 1588), SyncE, ESMC/SSM, holdover, quality levels.

All three domains share one managed-object model, one state store and one event bus, and are
exposed through a single management plane: **SNMP v2c**, **NETCONF**, **RESTCONF** and
(optionally) **gNMI**. No CGO, no sidecar processes, no external services — one binary, one
process, no Web UI (that lives in a separate repository).

> **Status: Phase 1 — model + store.** The repository builds, tests and starts; the
> configuration/logging/EventBus/clock foundations plus the managed-object models (radio, L2,
> sync, device) and the in-memory running/candidate/startup store are in place. The router, SNMP
> agent and later domain logic land in Phases 1.6–7; see [ROADMAP.md](ROADMAP.md).

## Quick start

Requirements: Go 1.27+ (Docker is optional and only needed for integration tests in later phases).

```bash
go mod download
make build                       # or: go build ./...
make test                        # or: go test ./...

# run (loads configs/default.yaml, logs JSON to stderr, Ctrl-C to stop)
go run ./cmd/simulator start --config configs/default.yaml
```

Configuration lives in [`configs/default.yaml`](configs/default.yaml); every field has a
documented default and is described in [docs/config.md](docs/config.md).

## Ports

| Plane | Port | Notes |
|---|---|---|
| SNMP agent | `1161` | v2c, community `public` |
| SNMP traps | `1162` | traps only, no informs |
| NETCONF | `830` | SSH subsystem `netconf`, no auth |
| RESTCONF | `8080` | no auth |
| Prometheus `/metrics` | `9090` | |
| gNMI (optional) | `9339` | disabled by default, TLS off |

Ports are deliberately unprivileged so the simulator runs without root.

## Demo

The canonical end-to-end check (available from Phase 4, when RESTCONF lands; the full
cross-domain scenario is Phase 6):

```bash
curl http://localhost:8080/restconf/data/sim-device:system-info
# {"device-id":"sim-001","uptime":123}
```

The cross-domain scenario — radio failure → PTP holdover → SNMP trap + NETCONF notification +
metric — is described in `.docs/desicion.md` §3.6 and will be reproduced by `scripts/demo.sh`.

## Repository layout

```
cmd/simulator/       binary entry point, delegates to internal/cli
internal/cli/        cobra commands (start, ...)
internal/config/     YAML configuration, defaults and validation
internal/log/        log/slog JSON logging on stderr
internal/event/      EventBus on Go channels
internal/clock/      injectable Clock, RealClock and FakeClock
internal/store/      running / candidate / startup datastores
internal/router/     path <-> model, OID <-> path, RPC dispatch
internal/model/      managed-object structs (path/xml/json tags)
internal/radio/      RRL domain: link budget, ATPC, ACM, alarms
internal/l2/         L2 domain: VLAN/QinQ, MAC table, STP/RSTP, LLDP, counters
internal/sync/       sync domain: PTP, SyncE, ESMC/SSM, holdover
internal/{snmp,netconf,restconf,gnmi,metrics}/   management planes
internal/tools/      blank imports pinning the approved dependency stack
configs/             YAML configuration
docs/                architecture, store, eventbus, config, ADRs, protocols
scripts/             check.sh, demo.sh
test/integration/    testcontainers-based integration tests (build tag `integration`)
testdata/            golden files
yang/                YANG modules (documentation only, embedded)
```

## Documentation

- [ROADMAP.md](ROADMAP.md) — phased plan and definition of done.
- [CONTRIBUTING.md](CONTRIBUTING.md) — branches, commit conventions, code style, tests.
- [AGENTS.md](AGENTS.md) — instructions for agentic IDEs.
- `docs/` — architecture, store, event bus, configuration and ADRs.
- `docs/protocols/` — SNMP, NETCONF, RESTCONF, PTP, SyncE, L2, RRL references.

## License

MIT — see [LICENSE.md](LICENSE.md).
