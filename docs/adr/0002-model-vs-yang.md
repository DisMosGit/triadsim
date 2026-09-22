# 2. Model in Go structs, keep YANG as documentation

- **Status:** Accepted
- **Date:** 2026-09-22
- **Deciders:** TriadSim maintainers

## Context

The device needs a data model for three domains (radio, L2, sync) that five management planes can
address: SNMP (OID ↔ value), NETCONF and RESTCONF (path ↔ value), gNMI (path ↔ value) and the CLI.
The model also has to carry validation, read-only access and list keys, and it has to be
available in tests without a device.

There are two established ways to do that:

1. Parse YANG at runtime, build a schema from it, and keep data as an untyped tree.
2. Declare the model as Go types with struct tags, and derive the schema from reflection.

The project's constraints push hard: one binary, a closed dependency list (no YANG runtime),
`go vet`-level type safety, table-driven tests, and a documented decision that the runtime never
parses YANG (`AGENTS.md`).

## Decision

`internal/model` holds the managed objects as Go structs with `path`, `xml`, `json` tags,
`config:"false"` for read-only nodes, `key:"true"` for a list key and `creatable:"true"` for a list
that grows at runtime. Every type implements `Validate() error`.

`internal/router` derives the schema tree from those types by reflection
(`Router.Schema`, `Router.Children`) and maps paths, OIDs and RPCs onto them.

`yang/*.yang` are documentation: they describe the same tree for readers, and `yang_test.go`
asserts that every node of the reflected schema appears in the module that
`router.ModuleFor` assigns it to. The YANG files are embedded (`yang/embed.go`) only so
`simulator schema --yang` can print them; nothing on the data path parses them.

The rules that follow:

- A new managed object touches one Go struct, its `Validate()`, the router maps, the golden files,
  the YANG doc and the defaults (`AGENTS.md`, "Changing a managed object").
- Wire encodings are codecs over the reflected schema: `internal/datatree` for the shared
  read/edit semantics, `internal/restconf/ops` for yang-data JSON/XML, `internal/netconf` for
  XML, `internal/gnmi` for JSON_IETF and proto scalars.

## Consequences

- Compile-time safety: a typo in a leaf path is caught by the router at construction, not by a
  runtime schema lookup.
- Validation, defaults and seed data live next to the type they describe.
- SNMP's weakly typed objects stay a separate table (`internal/router/oid.go`) rather than being
  forced into the model.
- The YANG files can drift from the model; the cost is the consistency test in `yang/`, which
  fails as soon as a node is added without its documentation.
- Clients that expect a machine-readable YANG schema served over gNMI (`Get` of the schema, or
  `CapabilityResponse` per-module schema) cannot be served fully: gNMI capabilities list the four
  module names and versions only, and the YANG modules are not available over the wire.

## Alternatives

- **Runtime YANG parser (goyang, ygot).** Rejected: it adds a large dependency tree and a
  generation step, and it puts a second source of truth (the YANG file) in front of the Go model,
  where our validation and seed data live. It also brings in a code generator the project does not
  want to run in CI.
- **Generated Go structs from YANG.** Rejected for the same reason: the generated types would be
  the model, making hand-written validation and seed defaults awkward, and the generator would be
  a build-time dependency.
- **An untyped map-based model.** Rejected: no compile-time safety, and `Validate()` for a
  hundreds-of-leaves tree becomes unmaintainable.
- **No YANG files at all.** Rejected: they are the lingua franca of the domain, they make the
  model reviewable by network engineers, and `ROADMAP.md` Phase 7 requires them.
