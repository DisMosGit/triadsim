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
before it reaches the store — the store itself has no schema knowledge. The one exception is the
state filter below, which hands the store the single fact it needs about those leaves.

## Domain state writes

`internal/router` gives the domains a second write path. `SetState` writes a leaf tagged
`config:"false"` — a learned MAC entry, a counter, a measured radio level — and `DeleteState`
removes it. Both address the running datastore, which the store mirrors into candidate, so the
leaf lives in both: candidate is what `Commit` copies into running, and a state leaf written only
to running would disappear at the next commit. Management planes keep using `Set`, which rejects a
read-only leaf.

## The candidate invariant

Candidate is a **superset of running**: every mutation of running — a `Set`, a `Delete`, and
therefore also an SNMP `SET`, a NETCONF or RESTCONF edit targeting running, and every L2
configuration write — is mirrored into candidate inside the same critical section. Candidate
therefore always holds running plus the pending edits, which is exactly what `Commit` assumes when
it makes running a copy of candidate.

What follows:

- A write to running is visible immediately and survives the next commit.
- A list entry removed from running is removed from candidate too, so a commit cannot resurrect it.
- A write to candidate is a pending edit: `running` is untouched until a commit applies it, and
  `Rollback` discards it.
- Cross-plane writes are last-writer-wins: a running-targeted edit also updates candidate, so it
  can overwrite a pending candidate edit for the same path. There is no cross-plane locking.
- `Diff` reports only pending edits; a running-only change can no longer show up as a diff.

The decision and its alternatives are recorded in `docs/adr/0005-running-write-through.md`.

## Persistence

`startup` is persisted as JSON (`startup.json`, path configurable through `startup.file` in
`configs/default.yaml`). It is written on commit and loaded at boot into running; the file is
listed in `.gitignore` because it is runtime state. Persistence uses `encoding/json` only — no
database — and the document is replaced atomically (temporary file, `fsync`, `rename`, directory
`fsync`), so a crash or a full disk mid-commit cannot leave a truncated `startup.json` that the
next boot refuses to load.

**Configuration only.** The persisted document holds configuration leaves exclusively. A state
leaf (`config:"false"` — uptime, a counter, a MAC entry's `age`) is volatile and would reappear
stale after a restart, so `Commit` and `Restore` drop it before `Save`, and `LoadStartup` drops
such leaves from a document written by an older version. The store knows which leaves those are
through the state filter installed with `SetStateFilter` (the router supplies `IsState`), the
same hook that keeps the RESTCONF entity-tag still under state churn. The in-memory datastores
are unaffected: state lives in running and candidate as before.

**Loaded input is validated.** A `startup.json` is untrusted operator input, so `LoadStartup`
runs the commit validator over the decoded leaves before installing anything and fails with a
path-level error; a hand-edited or corrupted-but-parseable file cannot put the device into a
state the validator rejects later. A missing file stays a no-op.

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

## Bulk reads and change tracking

Three read-side methods serve the consumers that need more than one leaf (`store.Store`):

- `Values(ctx, ds)` copies a whole datastore in one locked pass. It is the bulk read behind every flattening consumer — the SNMP request index, the data-tree read engine, `Router.Values` — replacing a `List` with one `Get` per path.
- `Generation(ctx, ds)` counts every mutation of a datastore, state writes included, so a cached derived view (the memoized SNMP object index in `internal/router`) knows when it is stale.
- `ConfigChangedAt(ctx, ds)` stamps the last configuration change and ignores state writes, because RFC 8040 §3.4.1.1 forbids the RESTCONF `ETag`/`Last-Modified` validators from moving on state churn. The RESTCONF plane derives both validators from it.

## Status

The in-memory implementation is complete: `internal/store/memory.go` (the three datastores),
`diff.go` (`diffValues`) and `persist.go` (`Save`/`Load`). `start` wires it up: it loads the
persisted startup, seeds `DefaultDevice` into the candidate and commits it on the first boot, and
installs the router as the commit validator. Phase 4 added the domain write path above; the
persisted document format is unchanged.
