# CLI

`cmd/simulator` is a thin `main` over the cobra command tree in `internal/cli`:

```bash
go run ./cmd/simulator <command> [flags]
# or, after make build:
./bin/simulator <command> [flags]
```

| Command | Purpose |
|---|---|
| `start` | run the simulator until SIGINT/SIGTERM |
| `alarm inject` | post a simulated alarm to a running simulator |
| `dump` | dump the datastore of a running simulator |
| `config validate` | validate a YAML configuration file |
| `schema` | print the model schema tree or the embedded YANG modules |
| `version` | print the build version |

`alarm` and `dump` talk to a **running** simulator over its RESTCONF endpoint, because the
simulator keeps its state in memory: a second process cannot reach it any other way. Both accept
`--addr` (default `http://127.0.0.1:8080`, the default `restconf.port`) and fail with a non-zero
exit code when nothing answers.

## start

```bash
go run ./cmd/simulator start --config configs/default.yaml
```

Loads and validates the YAML, installs JSON logging on stderr, then serves the SNMP v2c agent
(`:1161`, traps to `snmp.trap-host:snmp.trap-port`), the NETCONF SSH subsystem (`:1830`), the
RESTCONF HTTP server (`:8080`) and the Prometheus endpoint (`:9090`). The radio, L2 and sync
domains run their periodic loops for the lifetime of the process; the command blocks until
SIGINT/SIGTERM and shuts every plane down.

## alarm inject

```bash
go run ./cmd/simulator alarm inject --type radioLinkDown --link radio0
go run ./cmd/simulator alarm inject --type radioLinkDegraded --link radio0
go run ./cmd/simulator alarm inject --type radioLinkUp --link radio0
```

| Flag | Default | Meaning |
|---|---|---|
| `--type` | `radioLinkDown` | alarm to inject: `radioLinkDown`, `radioLinkDegraded`, `radioLinkUp` |
| `--link` | empty | radio link name; empty selects the first radio link |
| `--fade-db` | type-specific | injected fade depth in dB; overrides the type's default |
| `--addr` | `http://127.0.0.1:8080` | base URL of the running simulator |

The command is a thin RESTCONF client; the alarm itself is raised by the radio domain:

| `--type` | Endpoint | Default fade |
|---|---|---|
| `radioLinkDown` | `POST /api/simulate/radio-failure` | the domain's failure depth (60 dB) |
| `radioLinkDegraded` | `POST /api/simulate/radio-failure` | 32 dB, which lands the seeded link in the degraded band |
| `radioLinkUp` | `POST /api/simulate/radio-restore` | — |

The response is printed as received:

```json
{"link":"radio0","state":"down","status":"ok"}
```

An unknown `--type` lists the accepted values, and an endpoint error (an unknown link, say) is
reported with the RESTCONF status and error document.

## dump

```bash
go run ./cmd/simulator dump --format json | jq .
go run ./cmd/simulator dump --format xml --content config
```

| Flag | Default | Meaning |
|---|---|---|
| `--format` | `json` | `json` (indented) or `xml` (as received) |
| `--content` | `all` | `config`, `nonconfig` or `all` |
| `--addr` | `http://127.0.0.1:8080` | base URL of the running simulator |

`dump` performs `GET /restconf/data?content=<content>` with the matching `Accept`
(`application/yang-data+json` or `+xml`) and writes the body to stdout, so `--content config` is the
committed configuration and `--content nonconfig` the state leaves.

## config validate

```bash
go run ./cmd/simulator config validate --file configs/default.yaml
# configs/default.yaml: OK
```

`--file` defaults to `configs/default.yaml`. Validation is exactly what `start` does: the file is
decoded on top of the built-in defaults with unknown keys rejected, then `Config.Validate` runs.
The command prints `<file>: OK` on success and the error otherwise.

## schema

```bash
go run ./cmd/simulator schema | head -20
go run ./cmd/simulator schema --yang
go run ./cmd/simulator schema --yang --module sim-sync
```

`schema` prints the model schema tree as indented JSON. The tree is derived from the Go model, so
it needs no running simulator. Each node carries its router path, the YANG module that owns it
(`router.ModuleFor`), its kind (`container`, `list` or `leaf`), the key of a list and, for a leaf,
its type:

```json
{
  "name": "interface",
  "path": "interfaces/interface",
  "module": "sim-device",
  "kind": "list",
  "key": "name",
  "children": [
    {"name": "name", "path": "interfaces/interface/name", "module": "sim-device", "kind": "leaf", "leaf": "string"}
  ]
}
```

| Flag | Default | Meaning |
|---|---|---|
| `--yang` | `false` | print the embedded YANG modules instead of the schema tree |
| `--module` | empty | with `--yang`, print exactly this module (`sim-sync` or `sim-sync.yang`) |

With `--yang` and no `--module`, every embedded module is printed after a
`// --- yang/<file> ---` banner, in sorted file order. The modules are documentation: the runtime
never parses them, and `yang/yang_test.go` checks their node names against the model schema. See
[adr/0002-model-vs-yang.md](adr/0002-model-vs-yang.md).

## version

```bash
go run ./cmd/simulator version
# triadsim 0.1.0-dev
```

`Version` is a package variable, so a release build can override it:

```bash
go build -ldflags "-X github.com/DisMosGit/triadsim/internal/cli.Version=v0.1.0" ./cmd/simulator
```

## Exit codes

Every command returns its error to cobra, which prints it to stderr, and the process exits non-zero:
an unreadable configuration file, an unknown flag, an unreachable simulator, an unknown alarm type
or a non-2xx RESTCONF response. A successful command exits zero.
