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

var (
    ErrNotFound         = errors.New("store: path not found")
    ErrUnknownDatastore = errors.New("store: unknown datastore")
    ErrInvalidPath      = errors.New("store: invalid path")
    ErrInvalidValue     = errors.New("store: unsupported value type")
    ErrValidation       = errors.New("store: candidate validation failed")
)

type Store interface {
    Get(ctx context.Context, ds Datastore, path string) (any, error)
    Set(ctx context.Context, ds Datastore, path string, value any) error
    Delete(ctx context.Context, ds Datastore, path string) error
    List(ctx context.Context, ds Datastore, prefix string) ([]string, error)
    Diff(ctx context.Context) ([]Change, error)
    Commit(ctx context.Context) error
    Rollback(ctx context.Context) error
    Snapshot(ctx context.Context) (map[string]any, error)
    Restore(ctx context.Context, values map[string]any) error
}
```

Paths are router paths, for example
`interfaces/interface[name=radio0]/radio-link/tx-power`. `Get` and `Delete` return `ErrNotFound`
for a path that is absent from the addressed datastore. Values are the managed-object leaves
(`bool`, `int`, `uint8`, `uint16`, `uint32`, `uint64`, `float64`, `string`), so the store stays
independent of the model types; `Set` rejects any other Go type with `ErrInvalidValue` and any
path that is empty, has a leading or trailing slash, or contains an empty segment with
`ErrInvalidPath`.

`List` returns the paths **strictly below** a prefix in lexicographic order. `List(ctx, ds, "")`
lists the whole datastore. The prefix itself is never included, because the store holds leaves
only, so `List(..., "a")` returns `a/1` but not `a`, and `List(..., "a/1")` returns nothing. The
result is a fresh slice, so callers may keep it without aliasing the store.

## Diff, commit, rollback and restore

- `Diff` compares candidate with running and reports one `Change` per path: `create` when the
  path exists only in candidate, `update` when the value differs, `delete` when it exists only
  in running. Changes are ordered lexicographically by path.
- `Commit` validates the candidate (model `Validate()` rules), applies it to running and
  persists the new running configuration as startup. Candidate is authoritative: running becomes
  a copy of the candidate. The startup file is written **before** running is swapped, so a
  rejected candidate or a failed write leaves every datastore untouched. A validation failure is
  wrapped with `ErrValidation` so NETCONF can map it to `invalid-value`.
- `Rollback` discards every candidate change by copying running back into candidate.
- `Snapshot` returns a detached copy of running. It is taken **before** a confirmed commit, so
  the configuration the client has not confirmed yet can be put back.
- `Restore` puts a snapshot back: it replaces running, persists it as startup and leaves
  candidate untouched (the uncommitted configuration survives and can be re-committed). Like
  `Commit`, it writes the startup file before swapping running, and it does not validate, because
  a snapshot of running is by definition a configuration the device already accepted.

NETCONF `commit` and RESTCONF write operations use this sequence; `discard-changes` is
`Rollback`, and the rollback of an unconfirmed confirmed commit is `Restore`
(see `docs/protocols/NETCONF.md`).

Validation is injected, because the store has no schema knowledge: `NewMemory(Options{Validator:
v})` takes a `Validator func(ctx context.Context, values map[string]any) error`. It receives a
copy of the candidate's flat path → leaf map, so it cannot mutate the store, and `Commit` holds
the store lock while calling it — the snapshot exists so the validator never calls back into the
same `Memory`. A nil `Validator` accepts every candidate; the router wires the real one in
Phase 1.6.

## Read-only nodes

Managed objects mark read-only nodes with the `config:"false"` tag (for example `rssi` and
`fade-margin`). Enforcement lives in `internal/router`, which rejects a write to such a node
before it reaches the store — the store itself has no schema knowledge.

## Domain state writes

`internal/router` gives the domains a second write path. `SetState` writes a leaf tagged
`config:"false"` — a learned MAC entry, a counter, a measured radio level — to the running **and**
the candidate datastore, and `DeleteState` removes it from both. Candidate is what `Commit` copies
into running, so a state leaf written only to running would disappear at the next commit; the
seeded read-only leaves already live in both for the same reason. Management planes keep using
`Set`, which writes the addressed datastore and rejects a read-only leaf.

Configuration written through RESTCONF goes to the running datastore only, so it is visible
immediately but not part of candidate; a later NETCONF commit replaces it with the candidate
contents. This is a deliberate Phase 4 limitation, recorded in `ROADMAP.md`.

## Persistence

`startup` is persisted as JSON (`startup.json`, path configurable through `startup.file` in
`configs/default.yaml`). It is written on commit and loaded at boot into running; the file is
listed in `.gitignore` because it is runtime state. Persistence uses `encoding/json` and
`os.WriteFile` only — no database.

The document is versioned and every leaf carries its Go type, because plain JSON would decode
every number as `float64` and silently change the type of an `int` or `uint32` leaf across a
restart:

```json
{
  "version": 1,
  "values": {
    "interfaces/interface[name=radio0]/enabled": { "kind": "bool", "value": true },
    "interfaces/interface[name=radio0]/radio-link/tx-power": { "kind": "float64", "value": 20.5 }
  }
}
```

`Load` accepts only version `1` and the supported kinds (`bool`, `int`, `uint8`, `uint16`,
`uint32`, `uint64`, `float64`, `string`); malformed JSON, another version or an unknown kind is an
error. A **missing** file is not an error: it yields an empty map, because the first boot has no
startup file yet. `(*Memory).LoadStartup` loads the configured file into running, candidate and
startup and is a no-op when persistence is disabled or the file does not exist.

## Status

The in-memory implementation is complete: `internal/store/memory.go` (the three datastores),
`diff.go` (`diffValues`) and `persist.go` (`Save`/`Load`). `start` wires it up: it loads the
persisted startup, seeds `DefaultDevice` into the candidate and commits it on the first boot, and
installs the router as the commit validator. Phase 4 added the domain write path above; the
persisted document format is unchanged.
