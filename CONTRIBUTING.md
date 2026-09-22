# Contributing

## Branches

`main` is protected. Use short-lived branches:

- `feat/<name>` — new feature
- `fix/<name>` — bug fix
- `chore/<name>` — tooling, deps
- `docs/<name>` — documentation
- `refactor/<name>` — refactoring
- `test/<name>` — tests only

## Commits

Follow [Conventional Commits](https://www.conventionalcommits.org/):

```
<type>(<scope>): <subject>
```

Types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `chore`, `revert`.

Scopes: `radio`, `l2`, `sync`, `model`, `store`, `router`, `event`, `snmp`, `netconf`, `restconf`, `gnmi`, `cli`, `metrics`, `docs`, `deps`.

Examples:

```
feat(radio): add ATPC step simulation
fix(netconf): handle confirmed-commit timeout rollback
docs(snmp): document vendor OID tree and traps
```

## Local development

Requirements: Go 1.27+, optional Docker for integration tests.

```bash
go mod download
cp configs/default.yaml configs/local.yaml
go run ./cmd/simulator start --config configs/local.yaml
```

Common commands:

```bash
go test ./...                  # unit tests
go test -tags=integration ./... # integration tests (testcontainers)
./scripts/check.sh             # fmt, vet, tests
go run ./cmd/simulator dump --format json
go run ./cmd/simulator alarm inject --type radioLinkDown --link radio0
```

Ports: SNMP `1161`, traps `1162`, NETCONF `830`, RESTCONF `8080`, metrics `9090`, gNMI `9339` (optional).

## Pull requests

1. Rebase on `main`.
2. Run `./scripts/check.sh` (or `go test ./...` and `go vet ./...`).
3. Update docs under `docs/` if user-facing.
4. Add or update `docs/protocols/*.md` when changing protocol behavior.
5. One logical change per PR. Squash-merge.

## Code style

- Go 1.23+, `gofmt`, `go vet`.
- Prefer stdlib and the approved stack: `gosnmp`, `x/crypto/ssh`, `encoding/xml`, `chi`, `cobra`, `log/slog`, `prometheus`, `testify`, `testcontainers`, `yaml.v3`.
- No external runtime dependencies beyond standard Go modules.
- Models use `path`/`xml`/`json` tags. Read-only nodes use `config:"false"`.
- Each model has `Validate() error`.
- No auth anywhere. SNMP community `public`; NETCONF SSH accepts any login/password; RESTCONF has no auth.
- STP/RSTP and PTP/SyncE are simplified state machines, not real protocols.
- Keep code AI-friendly: stable APIs, common patterns, no obscure libraries.

Layer rules:

- `internal/model` depends on nothing.
- `internal/radio`, `internal/l2`, `internal/sync` depend on `model`, `store`, `event`.
- `internal/snmp`, `internal/netconf`, `internal/restconf`, `internal/gnmi` depend on `router`, `store`, `event`.
- `internal/cli`, `internal/metrics` depend on all layers as needed.
- No imports between domain packages (`radio`, `l2`, `sync`).

## Tests

| Level | Path | Command |
|-------|------|---------|
| Unit | `internal/...` | `go test ./...` |
| Integration | `test/integration/` | `go test -tags=integration ./...` |
| Golden | `testdata/*.golden.json` | `go test ./... -update` |

No `time.Sleep()` in tests — use the injectable `clock.Clock` interface.  
Use `t.TempDir()` for persistence tests.  
Property-based tests are out of scope.
