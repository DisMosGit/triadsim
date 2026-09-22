# Store

The store is the single source of truth for the simulated device's configuration. Every
management plane reads and writes through the same `internal/store` contract, so SNMP, NETCONF,
RESTCONF and gNMI always observe the same device state.

## Datastores

| Datastore | Meaning | Lifetime |
|---|---|---|
| `running` | The configuration currently active on the device. | in memory |
| `candidate` | Work-in-progress copy of running; NETCONF edits land here. | in memory |
| `startup` | The configuration loaded at boot, persisted on disk. | `startup.json` |

`Get`, `Set`, `Delete` and `List` address a datastore explicitly. That is not an
over-generalisation: NETCONF `get-config` can read `<source><candidate/></source>`,
`copy-config` can target `<startup/>`, and RESTCONF accepts `?datastore=candidate` as a
TriadSim extension. See `docs/protocols/NETCONF.md` (datastores) and
`docs/protocols/RESTCONF.md` (URL structure).

## Contract

```go
type Datastore string // Running, Candidate, Startup

type Op string // OpCreate, OpUpdate, OpDelete

type Change struct {
    Op   Op
    Path string
    Old  any
    New  any
}

var ErrNotFound = errors.New("store: path not found")

type Store interface {
    Get(ctx context.Context, ds Datastore, path string) (any, error)
    Set(ctx context.Context, ds Datastore, path string, value any) error
    Delete(ctx context.Context, ds Datastore, path string) error
    List(ctx context.Context, ds Datastore, prefix string) ([]string, error)
    Diff(ctx context.Context) ([]Change, error)
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
}
```

Paths are router paths, for example
`interfaces/interface[name=radio0]/radio-link/tx-power`. `List` returns the paths below a prefix
in lexicographic order; `Get` and `Delete` return `ErrNotFound` for a path that is absent from
the addressed datastore. Values are the managed-object leaves (`int`, `uint32`, `float64`,
`bool`, `string`), so the store stays independent of the model types.

## Diff, commit and rollback

- `Diff` compares candidate with running and reports one `Change` per path: `create` when the
  path exists only in candidate, `update` when the value differs, `delete` when it exists only
  in running.
- `Commit` validates the candidate (model `Validate()` rules), applies it to running and
  persists the new running configuration as startup. A validation failure aborts the commit and
  leaves running untouched.
- `Rollback` discards every candidate change by copying running back into candidate.

NETCONF `commit` and RESTCONF write operations use this sequence; `discard-changes` is
`Rollback`.

## Read-only nodes

Managed objects mark read-only nodes with the `config:"false"` tag (for example `rssi` and
`fade-margin`). Enforcement lives in `internal/router`, which rejects a write to such a node
before it reaches the store — the store itself has no schema knowledge.

## Persistence

`startup` is persisted as JSON (`startup.json`, path configurable through `startup.file` in
`configs/default.yaml`). It is written on commit and loaded at boot into running; the file is
listed in `.gitignore` because it is runtime state. Persistence uses `encoding/json` and
`os.WriteFile` only — no database.

## Status

Phase 0 freezes the interface and this contract. The in-memory implementation
(`internal/store/memory.go`), diff, commit and JSON persistence land in Phase 1.5; the datastore
paths become reachable from SNMP, NETCONF and RESTCONF in the phases that follow.
