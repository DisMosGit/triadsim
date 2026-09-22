# AGENTS.md

## Project
TriadSim — pure-Go, single-binary simulator of a telecom device combining three domains:
radio link (RRL), L2 switching, synchronization. Modular monolith: one state store,
one EventBus, management planes SNMP v2c / NETCONF / RESTCONF (+ optional gNMI).
No CGO, no sidecar processes, no external services, no Web UI (separate repo).

## See also
- `ROADMAP.md` — phased plan, atomic tasks and their definition of done. Pick the lowest open phase first.
- `CONTRIBUTING.md` — branches, Conventional Commits, code style and test rules.
- `docs/` — architecture, store, event bus, configuration and ADRs; `docs/protocols/` — protocol references.

## Commands
```bash
go build ./...                                              # build
go test ./...                                               # all tests
go test ./internal/store -run TestCandidate                 # single test
go test ./... -update                                       # regenerate golden files, then review the diff
gofmt -w . && go vet ./...                                  # required before finishing
go run ./cmd/simulator start --config configs/default.yaml  # run
```
Integration tests use testcontainers and require Docker.

## Architecture
- `cmd/simulator` — cobra CLI entrypoint.
- `internal/model` — managed-object structs with `path`/`xml`/`json` tags. YANG files are documentation only; the runtime never parses YANG.
- `internal/store` — running/candidate/startup datastores; JSON persistence.
- `internal/router` — path↔model and OID↔path mapping, RPC dispatch.
- `internal/event` — EventBus on channels. Domains don't import each other; cross-domain reactions flow through events.
- `internal/radio`, `internal/l2`, `internal/sync` — domain logic. Simplified state machines, not real protocol stacks.
- `internal/{snmp,netconf,restconf,gnmi,cli,metrics}` — management planes (`gnmi` optional).

## Scope (do not exceed MVP)
- SNMP v2c only, community `public`. No v3, no informs, no auth.
- NETCONF: candidate, commit, discard-changes, confirmed-commit, notifications — nothing more.
- RESTCONF: no auth. STP/RSTP, PTP/SyncE: simplified state machines — never implement real protocols.
- No Web UI.

## Conventions
- Go 1.27+. Dependencies are a closed list: `gosnmp`, `x/crypto/ssh`, `chi`, `cobra`,
  `prometheus`, `testify`, `testcontainers`, `yaml.v3` (+ stdlib). Ask before adding anything.
- Logging via `log/slog` only; no `fmt.Println`/`log.Print*`.
- Wrap errors with `%w`; no panics outside `main`; no silently ignored errors.
- Anything blocking takes `context.Context` as the first parameter; respect cancellation in EventBus consumers.
- Every model type implements `Validate() error`; read-only nodes carry `config:"false"`.
- Table-driven tests with testify; `t.TempDir()` for filesystem tests; no property-based tests.
- Never hand-edit `testdata/*.golden.json` — regenerate with `-update` and inspect the diff.

## Changing a managed object — touch all of
1. Struct in `internal/model` (+ tags)
2. Its `Validate()`
3. Path/OID maps in `internal/router`
4. Golden files (regenerate, don't hand-edit)
5. YANG doc file (docs only)
6. Defaults in `configs/default.yaml`, if applicable
