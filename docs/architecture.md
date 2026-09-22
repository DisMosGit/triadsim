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
   │  snmp :1161  │ netconf :1830│   restconf :8080   │  gnmi :9339  │
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
| `internal/store` | running/candidate/startup, diff/commit/rollback/snapshot/restore, JSON persistence | implemented (Phase 1.5, snapshot/restore Phase 3) |
| `internal/clock` | injectable clock and `FakeClock` | implemented |
| `internal/model` | managed-object structs with `path`/`xml`/`json` tags | radio, L2, sync, device (Phases 1.1-1.4) |
| `internal/router` | path ↔ model, OID ↔ path, RPC dispatch, commit validation, schema tree for protocol codecs | implemented (Phase 1.6, schema Phase 2) |
| `internal/radio` | RRL: link budget, RSSI, ATPC, ACM, alarms | Phase 6 |
| `internal/l2` | VLAN/QinQ, MAC table, STP, LLDP, counters | Phase 4 |
| `internal/sync` | PTP, SyncE, ESMC/SSM, holdover | Phase 5 |
| `internal/snmp` | SNMP v2c agent (`get`/`next`/`bulk`/`set`) | implemented (Phase 1.8); traps Phase 6 |
| `internal/netconf` | SSH subsystem, hello, EOM/chunked framing, RPC, get-config/edit-config/commit/discard-changes, confirmed commit | implemented (Phase 2, confirmed commit Phase 3) |
| `internal/netconf/notif` | RFC 5277 create-subscription, subscription registry and notification dispatch | implemented (Phase 3) |
| `internal/restconf` | chi router, codecs, CRUD | Phase 4 |
| `internal/gnmi` | optional gRPC service | Phase 7 |
| `internal/metrics` | Prometheus collectors and `/metrics` | implemented (Phase 1.9) |
| `internal/tools` | blank imports pinning the approved dependency stack | build tag `tools` only |

## Dependency rules

- `internal/model` depends on nothing.
- `internal/clock` and `internal/log` are leaf utilities.
- `internal/event` depends only on `internal/clock`.
- `internal/router` depends on `model` and `store`: it resolves paths and OIDs against a model
  template and reads and writes leaf values in the store.
- `internal/radio`, `internal/l2`, `internal/sync` depend on `model`, `store` and `event`.
- `internal/snmp`, `internal/netconf`, `internal/restconf`, `internal/gnmi` depend on `router`,
  `store` and `event`. `internal/netconf/ops` holds the configuration operations (get-config,
  edit-config, commit, discard-changes, the confirmed-commit state machine) and owns the protocol
  error type, so the parent package can render an `<rpc-error>` from it without an import cycle.
  `internal/netconf/notif` holds the RFC 5277 side (create-subscription, the notification
  document, the dispatcher): it depends on `internal/event`, `internal/netconf/ops` (for the XML
  element tree and the error type) and `internal/clock`, and never on the parent package. A
  session's notification write goes through the session's own message writer, which is safe for
  concurrent use, so a reply and a notification never interleave.
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

## Router

`internal/router` bridges protocol addresses and the model. A path is parsed into segments
(`a/b[c=d]/e`); a list element is selected by the field tagged `key:"true"`, whose `path` tag is
the predicate name. Navigation is reflective, so a node's Go type and its `config:"false` access
come from the schema rather than from a second table.

`Get`, `Set`, `Delete`, `List` and `Dispatch` read and write the store through those resolved
paths, and `Bindings` returns every exposed OID with its value in numeric OID order, which is what
the SNMP agent walks. The OID tables in `router/oid.go` cover the MIB-II system and interface
groups and the vendor `1.3.6.1.4.1.99999.1.*` radio objects. Interface columns use the 1-based
index of the interface in `Device.Interfaces`; the vendor radio objects are scalar (`.0`) because
the MVP has one radio link.

`Validate` is the `store.Validator`: it applies a candidate snapshot to a deep copy of the model
template and runs `Device.Validate()`, so a commit can never store a value the model rejects.

`Children` exposes the schema tree derived from the `path` tags (containers, lists with their key
name, leaves with their store kind). Protocol codecs map their own element tree onto router paths
with it, which is how `internal/netconf/ops` turns XML into paths and back; the tree is
type-driven, so it does not depend on which list instances a datastore holds.

## Data flow

1. A management plane receives a request and resolves the target node through `internal/router`.
2. Reads go to `internal/store` (running by default, candidate on request); writes go to the
   addressed datastore.
3. A commit validates the candidate through `router.Validate`, applies it to running, persists
   startup and publishes `ConfigChanged` on the bus. NETCONF `edit-config` validates its proposed
   candidate snapshot the same way before writing anything.
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
4. builds the store (`internal/store`) and router (`internal/router`) around
   `model.DefaultDevice` and installs `router.Validate` as the commit validator,
5. loads the persisted startup; when the running datastore is empty it seeds the default device
   into the candidate and commits it, which writes `startup.json`,
6. listens on the SNMP, NETCONF and metrics ports, then blocks until the context is cancelled and
   shuts every plane down. A bind failure is fatal, so a busy port is reported at startup instead
   of silently degrading the simulator.

See [store.md](store.md), [eventbus.md](eventbus.md) and [config.md](config.md) for the
contracts behind those steps, and `docs/protocols/` for the per-protocol references.
