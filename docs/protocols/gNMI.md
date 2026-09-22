# gNMI.md

**Project:** TriadSim — Lightweight Telecom Equipment Simulator  
**Protocol:** gNMI (gRPC Network Management Interface), version 0.4.0  
**Transport:** gRPC over HTTP/2, plaintext (no TLS)  
**Default Port:** 9339  
**Data Encoding:** `JSON`, `JSON_IETF`, `PROTO`  
**Status:** optional — the service listens only when `gnmi.enabled` is true

---

## 1. Introduction

gNMI is a gRPC-based protocol for reading and writing a device's data tree, and for subscribing to
changes in it. It is the streaming, model-driven counterpart of NETCONF and RESTCONF: paths are
gNMI paths, values are typed, and `Subscribe` keeps a stream open instead of polling.

TriadSim implements gNMI as an optional fourth management plane. It is deliberately thin: the
service in `internal/gnmi` translates gNMI paths and values into the same router paths, datastore
operations and event subscriptions the other planes use. It does not implement a schema server or
any of the gNMI extensions (`gnmi_ext`).

The protocol reference is [openconfig/reference](https://github.com/openconfig/reference/blob/master/rpc/gnmi/gnmi-specification.md);
the message definitions this implementation compiles against are the generated stubs of
[github.com/openconfig/gnmi](https://github.com/openconfig/gnmi) (`proto/gnmi`).

### 1.1. Implementation status

The implementation lives in `internal/gnmi`: `server.go` (gRPC lifecycle and `Capabilities`),
`path.go` (path conversion), `values.go` (typed values and error mapping), `get.go`, `set.go`,
`subscribe.go` and `events.go` (EventBus → model paths).

**Implemented**

- `Capabilities` announcing the four simulator models (`sim-device`, `sim-radio-link`,
  `sim-l2-switching`, `sim-sync`, all version `0.1.0`) and the encodings `JSON`, `JSON_IETF` and
  `PROTO`.
- `Get` for data types `ALL`, `CONFIG`, `STATE` and `OPERATIONAL`, returning one `Notification`
  per requested path with one `Update` per leaf.
- `Set` with `replace`, `update` and `delete`. Every operation of one request is applied in a
  single data-tree edit, so the request is atomic: it applies completely or leaves the datastore
  untouched.
- `Subscribe` with mode `ONCE` and mode `STREAM` / `ON_CHANGE`, including `updates_only` and the
  `sync_response` marker.
- Reads and writes against the **running** datastore only.

**Not implemented**

- No TLS and no authentication. The listener is a plain `net.Listen("tcp", …)` and every gRPC
  connection is anonymous, exactly like RESTCONF on 8080.
- No `union_replace`, and no subtree values: `replace` and `update` address one leaf each. A
  container or list value is answered with `INVALID_ARGUMENT`.
- No `SAMPLE` and no `TARGET_DEFINED` subscription modes, no `POLL`, no `heartbeat_interval`, no
  `qos`, no `allow_aggregation` and no `gnmi_ext` extensions. They are either ignored or answered
  with `UNIMPLEMENTED` (see §6).
- No schema retrieval: `CapabilityResponse` carries model names and versions only, and there is no
  `Get` of `/`-rooted schema data. The YANG modules are documentation and are not served over the
  wire; use `simulator schema --yang` to print them.
- No `candidate` or `startup` datastore in a gNMI path, and no gNMI metrics in `/metrics`.

---

## 2. Configuration and startup

```yaml
gnmi:
  enabled: false    # optional gRPC service, TLS off
  port: 9339
```

| Field | Default | Meaning |
|---|---|---|
| `gnmi.enabled` | `false` | start the service; when false nothing listens on `gnmi.port` |
| `gnmi.port` | `9339` | TCP port of the gRPC listener; validated for collisions only when enabled |

`start` binds the listener before serving and reports both the configured port and the bound
address in its startup record:

```json
{"level":"INFO","msg":"simulator starting","gnmi_enabled":true,"gnmi_port":9339,"gnmi_addr":"[::]:9339", ...}
```

`gnmi_addr` is an empty string when the plane is disabled. See [config.md](../config.md).

---

## 3. Paths

A gNMI path is a sequence of elements; an element may carry a key map. The router path grammar is
`a/b[k=v]/c`, described in [architecture.md](../architecture.md).

Rules of the conversion (`internal/gnmi/path.go`):

1. **Elements.** `Path.elem` is the supported form. The deprecated `Path.element` (repeated
   string) is accepted and joined literally. An empty path — or no path at all — addresses the
   model root.
2. **Keys.** An element with exactly one key renders as `[key=value]`, so
   `interfaces/interface{name: radio0}/radio-link/tx-power` becomes
   `interfaces/interface[name=radio0]/radio-link/tx-power`. Two keys in one element are
   rejected with `INVALID_ARGUMENT`: the model addresses a list entry by one key.
3. **Module prefixes.** A `module:` prefix on an element name is stripped
   (`sim-device:system-info` → `system-info`), matching the JSON rule of RESTCONF.
4. **Origin.** `origin` may be empty, one of the four module names (`sim-device`,
   `sim-radio-link`, `sim-l2-switching`, `sim-sync`) or one of their namespaces (`urn:sim:device`,
   `urn:sim:radio-link`, `urn:sim:l2-switching`, `urn:sim:sync`). Anything else is
   `INVALID_ARGUMENT` — the server never quietly ignores an origin that names a foreign schema.
5. **Existence.** An unknown node is `NOT_FOUND`. A valid node that currently holds no data is
   not an error: `Get` answers with no notification.

| Model path | gNMI path (JSON form) |
|---|---|
| `system-info/device-id` | `{"elem":[{"name":"system-info"},{"name":"device-id"}]}` |
| `interfaces/interface[name=radio0]/radio-link/tx-power` | `{"elem":[{"name":"interfaces"},{"name":"interface","key":{"name":"radio0"}},{"name":"radio-link"},{"name":"tx-power"}]}` |
| `ptp/clock` | `{"origin":"sim-sync","elem":[{"name":"ptp"},{"name":"clock"}]}` |

---

## 4. Values and encodings

`Capabilities` advertises `JSON`, `JSON_IETF` and `PROTO`; anything else (`BYTES`, `ASCII`) is
`INVALID_ARGUMENT`.

| Encoding | `Get` / `Subscribe` | `Set` |
|---|---|---|
| `JSON`, `JSON_IETF` | `TypedValue.json_val` / `JsonIetfVal` holding the JSON encoding of the leaf value (`"locked"`, `24`, `-72.5`, `true`) | a JSON scalar is accepted; a JSON object or array is `INVALID_ARGUMENT` |
| `PROTO` | the concrete field for the leaf's Go type: `string_val`, `int_val`, `uint_val`, `bool_val`, `double_val` | the same fields are accepted |

`Set` also accepts `decimal_val` (rendered with its precision), a one-element `leaflist_val`,
`bytes_val` and `ascii_val`. A `Set` operation without a value is `INVALID_ARGUMENT`.

---

## 5. RPCs

### 5.1. Capabilities

```
grpcurl -plaintext localhost:9339 gnmi.gNMI/Capabilities
```

The response lists the four models and the three encodings. `use_models` in a request is ignored.

### 5.2. Get

One `Notification` per requested path: `prefix` is the requested path, `update` holds one entry
per leaf below it with a **relative** path, and `timestamp` is the read time in nanoseconds.

| `GetRequest.type` | Leaves returned |
|---|---|
| `CONFIG` | configuration only (`config:"false"` leaves are omitted) |
| `STATE` | state only |
| `OPERATIONAL` | state only — the model has no separate operational annotation |
| `ALL` (default) | configuration and state |

```bash
# gnmic (not installed by this repository, see §8)
gnmic -a localhost:9339 --insecure get --path 'ptp/clock'
gnmic -a localhost:9339 --insecure --encoding json_ietf get --path 'system-info/device-id'
gnmic -a localhost:9339 --insecure --type state get --path 'interfaces/interface[name=radio0]/radio-link'
```

### 5.3. Set

`prefix` is prepended to every operation path. `delete` paths may address a leaf, a list entry or
a container subtree; `replace` and `update` must address a leaf. All operations are applied in one
edit, then `ConfigChanged` is published on the EventBus. The running datastore is written directly
— there is no candidate step, so `startup.json` is not updated (compare
[NETCONF.md](NETCONF.md#73-commit), where a commit persists).

```bash
gnmic -a localhost:9339 --insecure set \
  --update-path system-info/location --update-val field-site-7
gnmic -a localhost:9339 --insecure set \
  --replace-path 'interfaces/interface[name=radio0]/radio-link/tx-power' --replace-val 21.5
gnmic -a localhost:9339 --insecure set \
  --delete-path 'vlans/vlan[id=200]'
```

The response repeats each operation with its path and `op` (`DELETE`, `REPLACE` or `UPDATE`). A
request with no operations is a no-op: an empty response and no event.

### 5.4. Subscribe

| Mode | Behaviour |
|---|---|
| `ONCE` | one snapshot per subscription path, then `sync_response`, then the stream ends |
| `STREAM` / `ON_CHANGE` | an optional initial snapshot (unless `updates_only`), then `sync_response`, then one notification per matching event until the client goes away |
| `STREAM` / `SAMPLE`, `TARGET_DEFINED`, `POLL` | `UNIMPLEMENTED` |

The first request of the stream creates the subscription. When `subscribe.subscription` is empty,
the `prefix` itself is subscribed; an empty prefix subscribes to the whole device.

`ON_CHANGE` translates an EventBus event into the model path it changed, then re-reads that
subtree from the running datastore. A subscription is notified when its path and the affected path
lie on one another, so a subscription to `interfaces/interface[name=radio0]` sees an update about
`…/radio-link`. Path comparison is segment-aware: `[name=eth0]` never matches `[name=eth01]`.

| Event | Affected model path |
|---|---|
| `ConfigChanged` (resource `device`) | root: every subscription re-reads its own path |
| `AlarmRaised`/`AlarmCleared` `radioLinkDown`, `radioLinkDegraded` (resource `<link>`) | `interfaces/interface[name=<link>]/radio-link` |
| `StateTransition` or `AlarmRaised` `syncHoldover` (resource `ptp/clock`) | `ptp/clock` |
| `StateTransition` (resource `l2/stp/<port>`) | `stp/state/ports/port[name=<port>]` |
| `AlarmRaised` `l2Storm` (resource `l2/storm/<port>`) | `interfaces/interface[name=<port>]` |
| anything else | ignored |

```bash
# one snapshot, then exit
gnmic -a localhost:9339 --insecure subscribe --mode once --path 'ptp'

# streaming: watch the radio link while a failure is injected
gnmic -a localhost:9339 --insecure subscribe --path 'interfaces/interface[name=radio0]/radio-link'
curl -X POST http://localhost:8080/api/simulate/radio-failure -d '{"link":"radio0"}'
```

#### Delivery guarantees

Events are delivered best-effort from the in-process EventBus: a subscriber whose buffer is full
drops events and logs the drop ([eventbus.md](../eventbus.md)). gNMI adds no replay, no
aggregation and no heartbeat, so a client that needs an authoritative value uses `Get`.

---

## 6. Errors

Router and data-tree failures are mapped onto gRPC status codes. Every status carries the original
message in its `desc`.

| Condition | gRPC code |
|---|---|
| unknown path, `data-missing` | `NOT_FOUND` |
| malformed path, composite key, unknown origin, bad value, `unknown-element`, failed validation | `INVALID_ARGUMENT` |
| write to a read-only (`config:"false"`) leaf, `access-denied` | `PERMISSION_DENIED` |
| `data-exists` | `ALREADY_EXISTS` |
| unsupported operation (SAMPLE/POLL/`union_replace`) | `UNIMPLEMENTED` |
| cancelled or expired context | `CANCELLED` / `DEADLINE_EXCEEDED` |
| anything else, including a store failure | `INTERNAL` |

---

## 7. Wiring

```go
server := gnmi.New(r, st, bus, gnmi.Options{Addr: "127.0.0.1:0", Port: 9339, Clock: clock.RealClock{}})
if err := server.Listen(); err != nil { /* report the bind failure */ }
go server.Serve(ctx)   // returns on ctx cancellation or Close, with a 2 s graceful stop
```

Like the other planes the server takes the router, the store and the EventBus; `bus` may be nil,
which leaves `Capabilities`, `Get` and `Set` working and answers `STREAM` with `UNIMPLEMENTED`.
The clock stamps read notifications and can be replaced in tests.

Tests: `internal/gnmi/*_test.go` drive the real service over a loopback connection with the
generated client, and `test/integration/gnmi_test.go` does the same against the compiled binary in
a container (build tag `integration`).

---

## 8. Interoperability notes

`gnmic`, `grpcurl`, `pygnmi` and other real clients are **not installed** in this repository's
development environment, so the examples above are written from the gNMI specification and not
replayed here. Everything the examples exercise is covered by the Go-client tests in
`internal/gnmi`, which speak the same gRPC service:

```go
conn, _ := grpc.NewClient("127.0.0.1:9339", grpc.WithTransportCredentials(insecure.NewCredentials()))
client := gnmi.NewGNMIClient(conn)

response, err := client.Get(ctx, &gnmi.GetRequest{
    Path:     []*gnmi.Path{{Elem: []*gnmi.PathElem{{Name: "ptp"}, {Name: "clock"}, {Name: "state"}}}},
    Type:     gnmi.GetRequest_STATE,
    Encoding: gnmi.Encoding_JSON_IETF,
})
// response.Notification[0].Update[0].Val.GetJsonIetfVal() == []byte(`"locked"`)
```

Common client pitfalls with this implementation:

- A client that defaults to `PROTO` gets protobuf scalars for leaves, not JSON. Both work; the
  encoding is per value, so a `Get` never fails because one leaf cannot be represented in the
  requested encoding.
- A client that treats an empty `Get` response as "path not found" must not: a valid path with no
  data answers with no notification, while an unknown path is `NOT_FOUND`.
- `Set` to `startup.json` is impossible over gNMI: writes go to the running datastore only. Use
  NETCONF `commit` if the change must survive a restart.
