# 4. gosnmp, x/crypto/ssh and encoding/xml as the protocol stack

- **Status:** Accepted
- **Date:** 2026-09-22
- **Deciders:** TriadSim maintainers

## Context

The simulator has to speak four protocol families: SNMP v2c (GET/GETNEXT/GETBULK/SET, traps),
NETCONF over SSH (hello, framing, RPC), RESTCONF over HTTP/JSON/XML, and optionally gNMI over
gRPC. `AGENTS.md` keeps a closed dependency list, and `docs/adr/0003-modular-monolith.md` requires
all of them in one CGO-free binary.

Options per family:

- **SNMP**: a full agent implementation (`gosnmp` server side does not exist), `snmpgo`, or
  hand-rolled BER.
- **NETCONF/SSH**: `x/crypto/ssh` with our own subsystem, or `golang.org/x/net` + a third-party
  NETCONF library, or an external `netopeer2-server`.
- **RESTCONF**: a framework (`chi`) plus `encoding/json`/`encoding/xml`, or generated OpenAPI
  code.
- **gNMI**: gRPC plus the generated `openconfig/gnmi` stubs, or a hand-written protobuf layer.

## Decision

- **SNMP**: `github.com/gosnmp/gosnmp` for encoding and decoding `SnmpPacket` values, with our own
  UDP loop (`internal/snmp/agent.go`) and our own trap sender (`internal/snmp/trap.go`). gosnmp
  supplies ASN.1/BER and the PDU types; the MIB tables, community check and OID tree are ours.
  SNMP v2c only, community `public`, no v3 and no informs.
- **NETCONF**: `golang.org/x/crypto/ssh` for the transport, with our own protocol layer
  (`internal/netconf`): hello and capabilities, end-of-message and chunked framing, `<rpc>`
  dispatch, and the RFC 6241 operations. `encoding/xml` renders and parses documents.
- **RESTCONF**: `github.com/go-chi/chi/v5` for routing and `encoding/json`/`encoding/xml` for the
  yang-data media types; the operations and their HTTP status mapping are ours
  (`internal/restconf`).
- **gNMI**: `google.golang.org/grpc` plus `github.com/openconfig/gnmi` for the generated
  `proto/gnmi` service and message types, with our own service implementation
  (`internal/gnmi`). This is the Phase 7 addition to the closed list: it is the only way to be
  wire-compatible without hand-writing protobuf descriptors, and it is confined to the optional
  gNMI plane (grpc is imported nowhere else; `openconfig/gnmi` contributes only its `proto/gnmi`
  package, so `go mod tidy` adds five indirect modules, not its own heavy dependencies).
- **CLI**: `github.com/spf13/cobra`.
- **Observability and configuration**: `github.com/prometheus/client_golang`, `gopkg.in/yaml.v3`
  and `log/slog` from the standard library.
- **Tests**: `github.com/stretchr/testify` for assertions, `github.com/testcontainers/testcontainers-go`
  for the container integration tests.

Cross-cutting rules: no CGO, no `fmt.Println`, errors wrapped with `%w`, every blocking call takes
a `context.Context`, and nothing parsable is generated at build time (no `protoc`, no code
generator in CI).

## Consequences

- The dependency list stays small and auditable, and every module in it is pinned in `go.mod` and
  referenced from `internal/tools/tools.go`.
- Being wire-compatible with real clients is possible without running those clients in CI: the
  Go implementations of the protocols are the test clients (gosnmp for SNMP, `x/crypto/ssh` for
  NETCONF, the gnmi client for gNMI), and the container integration tests drive the compiled
  binary.
- gosnmp's server-side gap is on us: the agent loop, community check and PDU dispatch are
  hand-written and must be tested (they are: `internal/snmp/agent_test.go`).
- Protocol edge cases that a full stack would cover (SNMP v3, NETCONF `lock`/`copy-config`,
  RESTCONF YANG Patch, gNMI SAMPLE/TLS) are deliberately absent and answer with a protocol-level
  "not supported" instead of doing something subtly wrong.

## Alternatives

- **A full SNMP agent library.** None exists in pure Go with a v2c agent role; `snmpgo` is a
  client, and hand-rolling BER was rejected as unnecessary risk.
- **`golang.org/x/net` + a third-party NETCONF server library.** Rejected: it would still need
  `x/crypto/ssh` for the subsystem, and the library would hide the framing and error semantics
  this project is meant to demonstrate.
- **An external `netopeer2-server` or `snmpd`.** Rejected: sidecar processes and external
  services are out of scope by `AGENTS.md`.
- **Serving RESTCONF through a heavier framework (gin, echo).** Rejected: chi is a thin router, and
  the HTTP semantics (status codes, media types) are the interesting part, not the framework.
- **Hand-written gNMI protobuf messages without `openconfig/gnmi`.** Rejected: without `protoc`
  installed, the generated descriptors cannot be produced reliably, and the effort buys nothing —
  the module only contributes its `proto/gnmi` package to the build.
- **A gNMI implementation over plain HTTP/JSON.** Rejected: it would not be gNMI; real clients
  speak gRPC.
