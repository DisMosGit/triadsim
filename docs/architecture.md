# Architecture

TriadSim is a **modular monolith**: one binary, one state store, one event bus. There are no
external services, no sidecar processes, no CGO and no Web UI (that lives in a separate
repository). The simulated device combines three domains — radio link (RRL), L2 switching and
synchronization — behind one managed-object model and one management plane.

```
                        ┌──────────────────────────────────────────┐
                        │            cmd/simulator                 │
                        │            internal/cli (cobra)          │
                        └───────────────────┬──────────────────────┘
                                            │
   ┌──────────────┬──────────────┬──────────┴─────────┬──────────────┐
   │  internal/   │  internal/   │      internal/     │   internal/  │
   │  snmp :1161  │ netconf :830 │   restconf :8080   │  gnmi :9339  │
   │  traps :1162 │              │                    │  (optional)  │
   └──────┬───────┴──────┬───────┴─────────┬──────────┴──────┬───────┘
          │              │                 │                 │
          └──────────────┴────────┬────────┴─────────────────┘
                                  ▼
                        ┌───────────────────┐
                        │  internal/router  │  path ↔ model, OID ↔ path, RPC dispatch
                        └─────────┬─────────┘
                                  ▼
                        ┌───────────────────┐
                        │  internal/store   │  running / candidate / startup
                        └─────────┬─────────┘
                                  ▼
                        ┌───────────────────┐
                        │  internal/event   │  EventBus on Go channels
                        └─────────┬─────────┘
                                  ▼
        ┌───────────────┬─────────┴─────────┬───────────────┐
        │ internal/radio│      internal/l2  │  internal/sync│
        │ link budget,  │ VLAN, MAC, STP,   │ PTP, SyncE,   │
        │ ATPC, ACM     │ LLDP, counters    │ ESMC/SSM      │
        └───────────────┴───────────────────┴───────────────┘
```

## Packages

| Package | Responsibility | Phase 0 state |
|---|---|---|
| `cmd/simulator` | Binary entry point | delegates to `internal/cli` |
| `internal/cli` | cobra commands; today only `start` | implemented |
| `internal/config` | YAML configuration, defaults, `Load`, `Validate` | implemented |
| `internal/log` | `log/slog` JSON logging on stderr | implemented |
| `internal/event` | typed events and the channel-based bus | implemented |
| `internal/store` | running/candidate/startup, diff/commit/rollback, JSON persistence | implemented (Phase 1.5) |
| `internal/clock` | injectable clock and `FakeClock` | implemented |
| `internal/model` | managed-object structs with `path`/`xml`/`json` tags | radio, L2, sync, device (Phases 1.1-1.4) |
| `internal/router` | path ↔ model, OID ↔ path, RPC dispatch | Phase 1 |
| `internal/radio` | RRL: link budget, RSSI, ATPC, ACM, alarms | Phase 6 |
| `internal/l2` | VLAN/QinQ, MAC table, STP, LLDP, counters | Phase 4 |
| `internal/sync` | PTP, SyncE, ESMC/SSM, holdover | Phase 5 |
| `internal/snmp` | SNMP v2c agent and trap sender | Phase 1 |
| `internal/netconf` | SSH subsystem, framing, RPC, notifications | Phase 2 |
| `internal/restconf` | chi router, codecs, CRUD | Phase 4 |
| `internal/gnmi` | optional gRPC service | Phase 7 |
| `internal/metrics` | Prometheus collectors and `/metrics` | Phase 1 |
| `internal/tools` | blank imports pinning the approved dependency stack | build tag `tools` only |

## Dependency rules

- `internal/model` depends on nothing.
- `internal/clock` and `internal/log` are leaf utilities.
- `internal/event` depends only on `internal/clock`.
- `internal/radio`, `internal/l2`, `internal/sync` depend on `model`, `store` and `event`.
- `internal/snmp`, `internal/netconf`, `internal/restconf`, `internal/gnmi` depend on `router`,
  `store` and `event`.
- `internal/cli` and `internal/metrics` may depend on everything.
- Domain packages never import each other: radio, L2 and sync interact only through events.

## Managed objects

`internal/model` is the runtime schema. Its root is `Device`:

- `system-info/` — `device-id`, `name`, `description`, `contact`, `location` and the read-only `uptime`;
- `interfaces/interface[name=<if>]/` — `name`, `type` (`radio` or `ethernet`), `enabled`, `mtu`,
  `mac-address`, the read-only `counters`, and for a radio interface a `radio-link/` subtree with
  `tx-power`, the read-only `rssi`/`fade-margin`/`capacity`, `link-budget/`, `atpc/`, `acm/` and
  `modulation-profile/`.

The other domains are standalone managed-object trees: `VLAN`, `MACEntry`, `STPState` and
`LLDPNeighbor` for L2, and `PTPClock`, `SyncEState`, `ESMC` and `QL` for synchronization. Every
type validates its ranges and enumerations in `Validate()`, and read-only nodes carry
`config:"false"`, which the router rejects when a management plane tries to write them.

## Data flow

1. A management plane receives a request and resolves the target node through `internal/router`.
2. Reads go to `internal/store` (running by default, candidate on request); writes go to the
   addressed datastore.
3. A commit validates the candidate, applies it to running and persists startup, then publishes
   `ConfigChanged` on the bus.
4. Domains subscribe to the events they care about. `internal/radio` reacting to a simulated
   failure publishes `AlarmRaised`; `internal/sync` turns that into a PTP `StateTransition`, and
   the management planes turn both into traps, notifications and metrics. No domain calls
   another domain directly.

## Startup

`cmd/simulator/main.go` calls `cli.Execute`, which installs a context cancelled by SIGINT or
SIGTERM and runs the cobra command tree. `start` then:

1. loads and validates `configs/default.yaml` (`internal/config`),
2. installs the JSON stderr logger at the configured level (`internal/log`),
3. creates the `EventBus` (`internal/event`) with a `clock.RealClock`,
4. blocks until the context is cancelled, then closes the bus and exits 0.

See [store.md](store.md), [eventbus.md](eventbus.md) and [config.md](config.md) for the
contracts behind those steps, and `docs/protocols/` for the per-protocol references.
