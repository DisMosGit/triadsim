# RESTCONF.md

**Project:** TriadSim — Lightweight Telecom Equipment Simulator  
**Protocol:** RESTCONF (RFC 8040)  
**Transport:** HTTP/1.1 over TCP (no TLS)  
**Default Port:** 8080  
**Base Path:** `/restconf`  
**Data Encoding:** `application/yang-data+json`, `application/yang-data+xml`

---

## 1. Introduction

RESTCONF is an HTTP-based protocol that provides a programmatic interface for accessing data defined in YANG, using the datastore concepts defined in the Network Configuration Protocol (NETCONF). It maps the NETCONF datastore model (running, candidate, startup) onto HTTP resources, allowing clients to perform CRUD operations using standard HTTP methods.

TriadSim implements RESTCONF as one of its three northbound management interfaces, alongside SNMP and NETCONF. The protocol is defined in RFC 8040 (Standards Track, January 2017). Extensions for YANG Patch are defined in RFC 8072, and support for HTTP/2 transport is described in RFC 8700.

### 1.1. Implementation status

The implementation lives in `internal/restconf` (routing, media types, HTTP status mapping, the `ietf-restconf` error document), `internal/restconf/ops` (GET/PUT/PATCH/POST/DELETE against the store plus the JSON and XML codecs) and `internal/datatree` (the shared read/edit engine). It is a deliberately small subset of RFC 8040.

**Implemented**

- `GET`, `HEAD`, `PUT`, `PATCH`, `POST` and `DELETE` on `/restconf/data` — a resource, a list collection, a list entry or the whole datastore. Any other method gets `405 method-not-allowed` with an `Allow` header.
- The two YANG data media types `application/yang-data+json` and `application/yang-data+xml`, for both request bodies and responses, selected by `Content-Type` and `Accept`. JSON is the default.
- `?datastore=running|candidate|startup`, default `running`. A write is accepted on `running` and `candidate`; `startup` is read-only.
- `?content=all|config|nonconfig`, default `all`.
- The `ietf-restconf` error document (JSON) with `error-type`, `error-tag`, `error-path` and `error-message`.
- `POST /api/simulate/l2-storm`, a simulator-specific endpoint (not part of RFC 8040) that injects a broadcast storm on one L2 port.

**Not implemented**

- No authentication, no authorization and no TLS. The server is plain `net.Listen("tcp", …)` behind `net/http`; every request is anonymous.
- No YANG Patch (RFC 8072). `PATCH` accepts only a plain `application/yang-data+json` / `application/yang-data+xml` merge body.
- No `depth`, `fields`, `filter`, `with-defaults` or `replay` query parameters. Only `datastore` and `content` are read; any other query parameter is ignored.
- No `/restconf/operations` and no `/restconf/streams`. Both — including every sub-path — answer `501 operation-not-supported` for any method.
- No `ietf-yang-library` (and therefore no `application/yang` schema retrieval).
- No `application/yang-patch+json` or `application/yang-patch+xml` content type; such a request is `415 unsupported-media-type`.
- No `/.well-known/host-meta` root-resource discovery.
- No `OPTIONS`; it is answered with `405 method-not-allowed`.
- HTTP/1.1 only. There is no HTTP/2, TLS or `h2c` support.

Because of the two missing resource families, RESTCONF in TriadSim is used for configuration and monitoring only:

- **Configuration** — creating, replacing, modifying and deleting data resources in the running and candidate datastores.
- **Monitoring** — retrieving operational state, configuration data and statistics.

RPC invocation and event streams are served by NETCONF, not by this RESTCONF server.

---

## 2. Protocol Overview

### 2.1. Architectural Model

RESTCONF follows a client-server model over HTTP:

| Role | Entity | Description |
|---|---|---|
| **RESTCONF client** | NMS, `curl`, custom tooling | Sends HTTP requests and reads responses |
| **RESTCONF server** | TriadSim simulator | Serves YANG-defined resources |

The protocol is stateless at the HTTP layer. Each request carries all necessary information: method, URI, headers and optional body. The server responds with an HTTP status code and, when applicable, a response body.

### 2.2. Relationship to NETCONF

RESTCONF is designed as a companion protocol to NETCONF. Both operate on the same YANG data models and datastore concepts. The key differences are:

| Aspect | NETCONF | RESTCONF |
|---|---|---|
| Transport | SSH | HTTP over TCP (no TLS) |
| Encoding | XML | JSON and XML |
| Protocol style | RPC-based | REST-based |
| Configuration model | Candidate + commit | Direct edit (or candidate via explicit datastore) |
| Notifications | NETCONF notifications | Not implemented in TriadSim (no SSE endpoint) |

RESTCONF does not replace NETCONF; it provides an alternative HTTP-based access path to the same underlying data. A server may support both simultaneously, and TriadSim does. Because the two planes share one store, a NETCONF `commit` is authoritative over the candidate datastore — see §5.6.

---

## 3. Media Types

### 3.1. YANG Data Media Types

RESTCONF defines two application-specific media types for serializing YANG data:

| Media Type | Encoding | RFC Reference |
|---|---|---|
| `application/yang-data+xml` | XML | RFC 8040, Section 11.3.1 |
| `application/yang-data+json` | JSON | RFC 8040, Section 11.3.2 |

Both are accepted for request bodies and produced for responses (`MediaTypeJSON` / `MediaTypeXML` in `internal/restconf/server.go`). JSON is the default in both directions:

- `Content-Type` absent, or `application/yang-data+json` (with optional parameters such as `; charset=utf-8`) → JSON request body.
- `Accept` absent, `*/*`, or containing `application/yang-data+json` → JSON response.
- `Accept` containing `application/yang-data+xml` (and none of the JSON cases above) → XML response.
- Any other `Accept` value → `406 not-acceptable`.
- Any other `Content-Type` value on a write → `415 unsupported-media-type`.

### 3.2. YANG Patch Media Types

YANG Patch (RFC 8072) defines two additional media types for ordered edit operations:

| Media Type | Encoding |
|---|---|
| `application/yang-patch+xml` | XML |
| `application/yang-patch+json` | JSON |

These are not implemented. A `PATCH` request declaring either type is rejected with `415 unsupported-media-type` (`error-tag: operation-not-supported`). `PATCH` on `/restconf/data` is a plain merge using `application/yang-data+json` or `application/yang-data+xml`.

### 3.3. Other Media Types

| Media Type | RFC 8040 usage | Status in TriadSim |
|---|---|---|
| `application/yang` | YANG schema retrieval | Not implemented |
| `application/xrd+xml` | Root resource discovery | Not implemented |
| `text/event-stream` | Server-Sent Events (SSE) | Not implemented |

### 3.4. JSON Body and Response Shape

A JSON request body must be **a single-member JSON object**: the member name is the resource and its value carries the data. The member name may carry the module prefix (`sim-l2-switching:vlan`); the prefix is stripped before the name is matched. The value may be:

- an **object** — a container or one list entry,
- an **array** — a list of entries (each entry must be an object), or
- a **scalar** — a leaf value (string, number, boolean or `null`). A scalar body such as `{"sim-l2-switching:name":"DATA-NEW"}` is accepted when the target is a leaf.

A body with zero or several top-level members, a non-object root, an empty body or trailing data after the document is rejected (`invalid-value` or `malformed-message`).

Responses follow RFC 8040 §6.1: the top-level member is module-qualified, nested member names are not. A container is a JSON object, a list is a JSON array (an empty list is `[]`), and a leaf is a JSON scalar. Numbers keep their YANG type (for example `"id":100`, not `"100"`); `null` is not emitted for absent leaves — absent leaves are simply omitted. The response root is the **addressed node**, so `GET /restconf/data/sim-l2-switching:stp/state` returns a `sim-l2-switching:state` member, not `sim-l2-switching:stp`.

### 3.5. XML Body and Response Shape

An XML request body has the resource as its single root element; its child elements are the document's children. Element names are matched by **local name**, so the namespace is not significant on input (the codec ignores it). `PATCH` and `PUT` bodies therefore work with or without `xmlns`.

Responses declare namespaces from the module mapping:

- The addressed node is the document element and declares its module namespace, e.g. `<state xmlns="urn:sim:l2-switching">`.
- A nested node declares its namespace only when it changes, e.g. `<radio-link xmlns="urn:sim:radio-link">` inside `<interface xmlns="urn:sim:device">`.
- A list produces one element per entry, with no wrapper element of its own.
- A read of the whole datastore has several top-level nodes, so it is wrapped in the `ietf-restconf` `<data>` element:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<data xmlns="urn:ietf:params:xml:ns:yang:ietf-restconf">
  ...
</data>
```

`GET /restconf/data` is the only case that emits the `<data xmlns="urn:ietf:params:xml:ns:yang:ietf-restconf">` wrapper.

---

## 4. Resource Paths

### 4.1. Root Resource Discovery

RFC 8040 discovers the RESTCONF API root by retrieving `/.well-known/host-meta` and parsing the `<Link>` element with the `restconf` relation:

```http
GET /.well-known/host-meta HTTP/1.1
Host: localhost:8080
Accept: application/xrd+xml
```

TriadSim does **not** implement this endpoint. The base path is fixed at `/restconf` (`restconf.BasePath` in `internal/restconf/server.go`), so clients configure it directly and start with `/restconf/data`.

### 4.2. Path Structure

RESTCONF resource paths follow the YANG data tree hierarchy. The general form is:

```
{+restconf}/data/{+yang-data-path}
```

| Path Component | Description | Example |
|---|---|---|
| `{+restconf}` | RESTCONF root | `/restconf` |
| `data` | Data resource root | `/restconf/data` |
| `{+module-name}:{+node-name}` | Module-qualified data node | `/restconf/data/sim-device:system-info` |
| `{+list-key}` | List key predicate | `/restconf/data/sim-l2-switching:vlans/vlan=100` |
| `{+container}` | Container node | `/restconf/data/sim-l2-switching:stp/state` |

Rules the parser (`internal/restconf/path.go`) enforces:

- **List entries** are addressed with `key=value`, e.g. `/restconf/data/sim-l2-switching:vlans/vlan=100`. The key name is declared by the model (here `vlan` is the list name and `id` is the key, so a URL says `vlan=100`). Internally the canonical router path is `vlans/vlan[id=100]`.
- **A list collection** is addressed without a key predicate, e.g. `/restconf/data/sim-l2-switching:vlans/vlan`. That is the resource `POST` creates in and `DELETE` clears.
- **A list addressed without its key in the middle of a path** is a `400 malformed-message` (`list … is missing its key predicate`).
- **A key predicate on a non-list node** is a `400 malformed-message` (`… is not a list`).
- **The module prefix is optional on every segment**, but when present it must name the module that owns that node (`400 malformed-message` otherwise). URLs the server generates in a `Location` header always module-qualify the top node.
- **Unknown node names** are `400 unknown-element`.
- **Trailing slashes** are tolerated.

### 4.3. Modules and Top-Level Nodes

Modules are defined in `internal/router/modules.go`. `ModuleFor` selects the module from the first path segment that matches one of its node names; a path no module claims belongs to `sim-device`.

| Module (prefix) | XML namespace | Top-level data nodes |
|---|---|---|
| `sim-device` | `urn:sim:device` | `system-info`, `interfaces` |
| `sim-l2-switching` | `urn:sim:l2-switching` | `vlans`, `mac-table`, `stp`, `lldp` |
| `sim-radio-link` | `urn:sim:radio-link` | none at the data-tree root; owns `radio-link` and `modulation-profile`, both below `interfaces/interface[<key>]` |
| `sim-sync` | `urn:sim:sync` | none reachable: the data model does not yet carry `ptp` or `synce` |

`sim-sync` is declared in the module table (its node names `ptp` and `synce` are mapped for paths such as `ptp/clock/state`), but `internal/model.Device` has no synchronization subtree, so no `sim-sync` resource can be addressed over RESTCONF today.

### 4.4. TriadSim Resource Paths

The reachable data tree (from `internal/model` and `internal/model/seed.go`):

| Resource | Path | Description |
|---|---|---|
| Whole datastore | `/restconf/data` | Every top-level node |
| System info | `/restconf/data/sim-device:system-info` | Device identity, uptime |
| Interfaces | `/restconf/data/sim-device:interfaces` | Interface container |
| Interface | `/restconf/data/sim-device:interfaces/interface=radio0` | One interface, keyed by `name` |
| Radio link | `/restconf/data/sim-device:interfaces/interface=radio0/radio-link` | RRL configuration and state |
| Modulation profile | `/restconf/data/sim-device:interfaces/interface=radio0/radio-link/modulation-profile=5` | One ACM profile, keyed by `id` |
| VLANs | `/restconf/data/sim-l2-switching:vlans` | VLAN container |
| VLAN list | `/restconf/data/sim-l2-switching:vlans/vlan` | VLAN collection (POST target) |
| VLAN | `/restconf/data/sim-l2-switching:vlans/vlan=100` | One VLAN, keyed by `id` |
| MAC table | `/restconf/data/sim-l2-switching:mac-table` | Bridge forwarding database |
| MAC entry | `/restconf/data/sim-l2-switching:mac-table/entry=02:00:00:00:00:02` | One entry, keyed by `mac-address` |
| STP state | `/restconf/data/sim-l2-switching:stp/state` | STP/RSTP bridge and port states |
| STP port | `/restconf/data/sim-l2-switching:stp/state/ports/port=eth0` | One STP port, keyed by `port` |
| LLDP | `/restconf/data/sim-l2-switching:lldp` | LLDP configuration |
| LLDP neighbour | `/restconf/data/sim-l2-switching:lldp/neighbors/neighbor=eth0` | One neighbour, keyed by `port` |

List keys in the model:

| Canonical path | Key |
|---|---|
| `interfaces/interface` | `name` |
| `vlans/vlan` | `id` |
| `vlans/vlan[...]/ports/port` | `port` |
| `mac-table/entry` | `mac-address` |
| `stp/state/ports/port` | `port` |
| `lldp/neighbors/neighbor` | `port` |
| `…/radio-link/modulation-profile` | `id` |

---

## 5. HTTP Methods

RESTCONF maps CRUD operations to standard HTTP methods. The following table summarizes the RESTCONF methods defined in RFC 8040, Section 4, with what TriadSim does:

| Method | RESTCONF Operation | NETCONF Equivalent | Implemented |
|---|---|---|---|
| **GET** | Retrieve data | `get-config`, `get` | Yes |
| **HEAD** | Retrieve headers only | — | Yes (GET without a body) |
| **POST** | Create resource | `edit-config` (create) | Yes, on a list collection |
| **PUT** | Create or replace resource | `edit-config` (create/replace) | Yes |
| **PATCH** | Merge resource | `edit-config` (merge) | Yes, plain merge only |
| **DELETE** | Delete resource | `edit-config` (delete) | Yes |
| **OPTIONS** | Retrieve allowed methods | — | No; answered `405` with `Allow` |

A method that is not allowed for the target is answered `405 method-not-allowed` (`error-tag: operation-not-supported`) with an `Allow` header. The values used are:

| Situation | `Allow` |
|---|---|
| Unsupported method on `/restconf/data` | `GET, HEAD, PUT, PATCH, POST, DELETE` |
| Any write to `?datastore=startup` | `GET, HEAD` |
| `PUT` on a list collection | `GET, HEAD, PATCH, POST, DELETE` |
| `POST` on a resource that is not a list collection | `GET, HEAD, PUT, PATCH, DELETE` |
| Wrong method on `/api/simulate/l2-storm` | `POST` |

### 5.1. GET and HEAD

`GET` reads a resource, a list collection, a list entry or the whole datastore. `HEAD` runs the same read — it validates `Accept`, resolves the resource and sets `Content-Type` and status — but writes no body.

The response root is the addressed node (§3.4). Missing data is a `404 data-missing`: a list entry the datastore does not hold, a leaf that is absent, or a list collection with no entries. A container is returned even when empty. A read of a list through its container returns an empty JSON array when the list has no entries.

`?content=` selects which leaves are read:

- `all` (default) — configuration and state;
- `config` — leaves not tagged `config:"false"` only;
- `nonconfig` — state leaves only.

With `content=config`, a state leaf is not part of the resource at all: addressing it directly returns `404`.

**Example — retrieve system info:**

```http
GET /restconf/data/sim-device:system-info HTTP/1.1
Host: localhost:8080
Accept: application/yang-data+json
```

**Response:**

```http
HTTP/1.1 200 OK
Content-Type: application/yang-data+json

{"sim-device:system-info":{"device-id":"sim-001","name":"triadsim-01","description":"TriadSim simulated telecom device","contact":"noc@example.net","location":"lab","uptime":0}}
```

### 5.2. PUT

Creates or completely replaces the target resource (`replace` semantics applied by the shared data-tree editor). If the resource does not exist it is created and the response is `201 Created` with a `Location` header; if it exists it is replaced and the response is `200 OK`. Neither response carries a body.

- The body must carry exactly one document. For a list entry, the body may omit the key leaf — it is taken from the request path — but a key that contradicts the path is `400 invalid-value`.
- `PUT` is not supported on a list collection: it is answered `405` with `Allow: GET, HEAD, PATCH, POST, DELETE`.
- A `PUT` that addresses a leaf tagged `config:"false"` is `403 access-denied`.
- A `PUT` whose proposed datastore snapshot fails model validation is `422 invalid-value`, and the datastore is left untouched.

### 5.3. PATCH

Merges the body into the target resource. The response is `204 No Content`.

- The media type is a plain `application/yang-data+json` or `application/yang-data+xml` body; YANG Patch is not accepted (`415`).
- It can target a container, a leaf or a list entry.
- On a list collection, every entry the body carries is merged.
- A scalar body is accepted only when the target is a leaf; otherwise it is `400 invalid-value`.

### 5.4. POST

Creates the list entries the body carries. The response is `201 Created` with a `Location` header pointing at the created resource; there is no body. For a single created entry the `Location` is the entry URL (`…/vlans/vlan=300`); when the body carries several entries it is the collection URL.

- `POST` is only supported on a list collection. On any other resource it is `405` with `Allow: GET, HEAD, PUT, PATCH, DELETE`.
- Creating an entry that already exists is `409 data-exists`.
- A body that is empty, malformed or holds no entry is `400`.

### 5.5. DELETE

Deletes the target resource and answers `204 No Content`.

- On a list entry, that entry is removed. Read-only state leaves below it are not writable and are left in place; a `DELETE` that names only a read-only leaf that exists is `403 access-denied`, and one that names a leaf that does not exist is `404 data-missing`.
- On a list collection, every entry is removed. When the collection is empty the request is `404 data-missing`.
- Deleting a missing entry is `404 data-missing`.

### 5.6. Writes, Datastores and Commit

Every write goes to the datastore selected by `?datastore=`, default `running`:

- `?datastore=running` (or no parameter) writes the active datastore.
- `?datastore=candidate` writes the work-in-progress datastore. The seeded device lives in `running` and `candidate`; the test suite writes candidate and confirms `running` is untouched.
- `?datastore=startup` is read-only for every method: it answers `405` with `Allow: GET, HEAD`.

RESTCONF writes are **not** mirrored between datastores and are not committed. The store's `Commit` is candidate-authoritative: it validates `candidate`, makes `running` a copy of it and persists `startup` from it. The practical consequences are:

- A change written to `running` over RESTCONF can be overwritten by a later NETCONF `commit`, because commit replaces `running` with `candidate`.
- A change written to `candidate` over RESTCONF is not visible in `running` until a NETCONF `commit` applies it.
- A successful write publishes a `ConfigChanged` event on the shared bus; the request itself never commits.

---

## 6. Query Parameters

RFC 8040 defines several query parameters. TriadSim reads exactly two of them (`internal/restconf/path.go`):

| Parameter | Values | Default | Behaviour |
|---|---|---|---|
| `datastore` | `running`, `candidate`, `startup` | `running` | Selects the datastore read or written |
| `content` | `all`, `config`, `nonconfig` | `all` | Selects configuration, state or both |

Notes:

- `content` is spelled **`nonconfig`**, without a hyphen (the value of `datatree.ContentNonConfig`).
- An unknown value for either parameter is `400 malformed-message` (`unknown datastore "…"` / `unknown content "…"`).
- The RFC 8040 parameters `depth`, `fields`, `filter`, `with-defaults` and `replay` are **not implemented**. They are neither validated nor acted on: an unknown query parameter is ignored and the full resource is returned. The capability URNs below are consequently not advertised.

**Example — configuration only:**

```http
GET /restconf/data/sim-device:interfaces/interface=radio0/radio-link?content=config HTTP/1.1
Host: localhost:8080
Accept: application/yang-data+json
```

This omits state leaves such as `rssi`, `fade-margin`, `capacity` and `calculated-rsl`, and returns configuration such as `tx-power`. Addressing a state leaf such as `…/radio-link/rssi` with `?content=config` is `404`.

**Example — read the candidate datastore:**

```http
GET /restconf/data/sim-l2-switching:vlans/vlan=200?datastore=candidate HTTP/1.1
Host: localhost:8080
```

---

## 7. Error Handling

### 7.1. Error Response Format

When an HTTP status code in the 4xx or 5xx range is returned, the response body is the `ietf-restconf` error document defined in RFC 8040, Section 7.1 (`internal/restconf/errors.go`). The document is **always JSON**, even when the request asked for XML: the codec sets `Content-Type: application/yang-data+json` for every error and has exactly one error entry per response.

```json
{
  "ietf-restconf:errors": {
    "error": [
      {
        "error-type": "protocol",
        "error-tag": "data-missing",
        "error-path": "vlans/vlan[id=999]",
        "error-message": "data missing: vlans/vlan[id=999]"
      }
    ]
  }
}
```

### 7.2. Error Fields

The implementation emits four fields; there is no `error-app-tag` and no `error-info`:

| Field | JSON key | Description |
|---|---|---|
| Type | `error-type` | Error category. Values produced are `protocol` (the server's own errors and data-tree protocol errors), `application` (a device/store failure) and `rpc` (a malformed or empty request body, which the codecs report with the RFC 6241 `rpc` type) |
| Tag | `error-tag` | RFC 6241 error tag, e.g. `malformed-message`, `unknown-element`, `invalid-value`, `data-missing`, `access-denied`, `data-exists`, `operation-not-supported`, `operation-failed`, `too-big` |
| Path | `error-path` | Canonical router path of the offending node, e.g. `vlans/vlan[id=999]` (note: the internal `[key=value]` form, not the URL form). Omitted when empty |
| Message | `error-message` | Human-readable description. Omitted when empty |

### 7.3. HTTP Status Code Mapping

The following table is the exact mapping in `internal/restconf/errors.go`:

| HTTP status | `error-type` | `error-tag` | Condition |
|---|---|---|---|
| `400 Bad Request` | `protocol` | `malformed-message` | Malformed or empty body reported by the server's own reader; unknown `?datastore=`/`?content=` value; missing key predicate, key on a non-list, or a module prefix that does not own the node |
| `400 Bad Request` | `rpc` | `malformed-message` | Empty or unparsable JSON/XML body reported by the codec |
| `400 Bad Request` | `protocol` | `unknown-element` | Path or body names a node that is not in the model |
| `400 Bad Request` | `protocol` | `missing-element` | A required element (for example a list key) is absent from the body |
| `400 Bad Request` | `protocol` | `invalid-value` | Body is not a single-member object, a key contradicts the path, a scalar body targets a non-leaf, or a leaf value cannot be parsed |
| `403 Forbidden` | `protocol` | `access-denied` | Write to a leaf tagged `config:"false"` |
| `404 Not Found` | `protocol` | `data-missing` | Missing leaf or list entry; `DELETE` of an empty list collection |
| `404 Not Found` | `protocol` | `invalid-value` | Path is outside `/restconf/data` (`resource not found: …`) |
| `405 Method Not Allowed` | `protocol` | `operation-not-supported` | Method is not allowed for the target; `Allow` header is set |
| `406 Not Acceptable` | `protocol` | `operation-not-supported` | `Accept` is not a YANG data media type |
| `409 Conflict` | `protocol` | `data-exists` | Creating an entry that already exists |
| `413 Request Entity Too Large` | `protocol` | `too-big` | The request body exceeded the 1 MiB cap (`maxBodyBytes = 1 << 20`), or a data-tree operation reports a size limit |
| `415 Unsupported Media Type` | `protocol` | `operation-not-supported` | `Content-Type` is not a YANG data media type |
| `422 Unprocessable Entity` | `protocol` | `invalid-value` | The proposed datastore snapshot fails model validation |
| `500 Internal Server Error` | `application` | `operation-failed` | Store or router failure; also the default for an unclassified error |
| `501 Not Implemented` | `protocol` | `operation-not-supported` | `/restconf/operations`, `/restconf/streams`, the storm endpoint without a simulator, or any other unimplemented operation |

**Note on the body limit.** Both the data-resource reader and the storm endpoint wrap the body in `http.MaxBytesReader(w, r.Body, 1<<20)` (1 MiB). A body above the limit fails while it is being read and is answered with `413 too-big` (`request body exceeds the 1048576 byte limit`); a different read failure is `400 malformed-message`.

### 7.4. Common Error Scenarios

**Missing resource (404):**

```http
GET /restconf/data/sim-l2-switching:vlans/vlan=999 HTTP/1.1
Host: localhost:8080
```

```http
HTTP/1.1 404 Not Found
Content-Type: application/yang-data+json

{"ietf-restconf:errors":{"error":[{"error-type":"protocol","error-tag":"data-missing","error-path":"vlans/vlan[id=999]","error-message":"data missing: vlans/vlan[id=999]"}]}}
```

**Read-only leaf (403):**

```http
PUT /restconf/data/sim-device:interfaces/interface=radio0/radio-link/rssi HTTP/1.1
Host: localhost:8080
Content-Type: application/yang-data+json

{"sim-device:rssi":-50}
```

```http
HTTP/1.1 403 Forbidden
Content-Type: application/yang-data+json

{"ietf-restconf:errors":{"error":[{"error-type":"protocol","error-tag":"access-denied","error-path":"interfaces/interface[name=radio0]/radio-link/rssi","error-message":"access denied: interfaces/interface[name=radio0]/radio-link/rssi is read-only"}]}}
```

**Model validation (422):**

A VLAN without a name is rejected by `VLAN.Validate`:

```http
PUT /restconf/data/sim-l2-switching:vlans/vlan=400 HTTP/1.1
Host: localhost:8080
Content-Type: application/yang-data+json

{"sim-l2-switching:vlan":[{"id":400}]}
```

```http
HTTP/1.1 422 Unprocessable Entity
Content-Type: application/yang-data+json

{"ietf-restconf:errors":{"error":[{"error-type":"protocol","error-tag":"invalid-value","error-message":"router: validate: vlans[1]: vlan 400: name must not be empty"}]}}
```

The rejected configuration is not written: a subsequent `GET` of the resource is `404`.

**Unsupported media type (415):**

```http
PUT /restconf/data/sim-device:system-info HTTP/1.1
Host: localhost:8080
Content-Type: application/json

{"sim-device:system-info":{"name":"x"}}
```

```http
HTTP/1.1 415 Unsupported Media Type
Content-Type: application/yang-data+json

{"ietf-restconf:errors":{"error":[{"error-type":"protocol","error-tag":"operation-not-supported","error-message":"Content-Type \"application/json\" is not supported"}]}}
```

**Unsupported Accept (406):**

```http
GET /restconf/data/sim-device:system-info HTTP/1.1
Host: localhost:8080
Accept: text/plain
```

```http
HTTP/1.1 406 Not Acceptable
Content-Type: application/yang-data+json

{"ietf-restconf:errors":{"error":[{"error-type":"protocol","error-tag":"operation-not-supported","error-message":"Accept \"text/plain\" is not supported"}]}}
```

---

## 8. Capabilities

RFC 8040 servers advertise optional capabilities via the `ietf-restconf-monitoring` module's `capability` leaf-list, and the capability URNs are registered by IANA. Examples include `urn:ietf:params:restconf:capability:depth:1.0`, `…:fields:1.0`, `…:filter:1.0`, `…:with-defaults:1.0` and `…:yang-patch:1.0`.

TriadSim does **not** implement capability advertisement. There is no `ietf-restconf-monitoring` resource and no `ietf-yang-library`. A `GET` of such a path is answered `400 unknown-element` because the node is not in the model; no capability URN is served. The only request features beyond the base are the two query parameters in §6, and they are not advertised.

---

## 9. Simulator Implementation

### 9.1. HTTP Server

TriadSim uses Go's standard `net/http` package with the `chi` router for path matching (`internal/restconf/server.go`). The server owns one plain TCP listener (`net.Listen("tcp", …)`), so it speaks HTTP/1.1 with no TLS. There is no authentication. The configured request-header timeout is 5 s and shutdown on context cancellation is bounded to 2 s.

Routes:

| Route | Handler |
|---|---|
| `/restconf/data`, `/restconf/data/*` | Data resource (GET/HEAD/PUT/PATCH/POST/DELETE) |
| `/restconf/operations`, `/restconf/operations/*` | `501 operation-not-supported` |
| `/restconf/streams`, `/restconf/streams/*` | `501 operation-not-supported` |
| `/api/simulate/l2-storm` | Simulation endpoint (`POST`) |

The server is stateless at the HTTP layer; state resides in the `internal/store` package (running/candidate/startup datastores).

### 9.2. Path-to-Model Resolution

The `internal/router` package maps RESTCONF paths to Go model fields using the `path` tag on model structs and derives a type-driven schema tree (`Children`) independent of any datastore. The mapping is bidirectional:

- **GET**: URL path → canonical router path → datatree read → serialize to JSON/XML.
- **PUT/PATCH/POST/DELETE**: URL path + body → `datatree.Apply` on a proposed snapshot → whole-snapshot model validation → diff write to the store.

The shared `internal/datatree` engine gives every write the same shape: the edit is applied to an in-memory copy of the datastore, the copy is validated as a whole, and only then is the difference written. A rejected edit therefore leaves the datastore untouched.

### 9.3. Datastore Integration

RESTCONF operations target the running datastore by default. The candidate and startup datastores are addressed with `?datastore=candidate` and `?datastore=startup` (a TriadSim extension for demonstration purposes; standard RESTCONF does not define a datastore query parameter). Writes are accepted on running and candidate, and rejected on startup. See §5.6 for the commit relationship with NETCONF.

### 9.4. Content Negotiation

The server supports both JSON and XML encoding:

- `Accept: application/yang-data+xml` → XML response.
- `Accept: application/yang-data+json`, `Accept: */*`, or no `Accept` header → JSON response.
- Any other `Accept` → `406 not-acceptable`.
- Request bodies: `Content-Type: application/yang-data+xml` → XML; `application/yang-data+json` or missing → JSON; any other → `415 unsupported-media-type`.

Errors are always JSON regardless of `Accept`.

### 9.5. RPC Operations and Event Streams

Not implemented. RFC 8040 operation resources under `/restconf/operations` and event streams under `/restconf/streams` are mounted only to return:

```http
HTTP/1.1 501 Not Implemented
Content-Type: application/yang-data+json

{"ietf-restconf:errors":{"error":[{"error-type":"protocol","error-tag":"operation-not-supported","error-message":"/restconf/operations/sim-l2:clear-mac-table is not implemented"}]}}
```

The message names the requested path. RPCs are available on the NETCONF plane and over SNMP, not here.

### 9.6. Simulation Endpoint

In addition to the standard data resource, the server exposes one simulator-specific endpoint that is not part of RFC 8040:

```
POST /api/simulate/l2-storm
```

It injects a broadcast storm on one interface by driving the L2 domain in process. The request body is a plain JSON object with two fields:

| Field | Type | Meaning |
|---|---|---|
| `port` | string | Ingress interface name, e.g. `eth0` (required) |
| `packets` | integer | Number of frames to inject |

A successful request is `202 Accepted` with `Content-Type: application/yang-data+json` and a small status object (not a YANG data document):

```json
{"packets":1500,"port":"eth0","status":"ok"}
```

Errors: a malformed body is `400 malformed-message`; a missing `port` is `400 invalid-value`; a `Storm` failure (for example an unknown port) is `422 invalid-value`; a method other than `POST` is `405` with `Allow: POST`; and if the server was built without a storm simulator the endpoint is `501 operation-not-supported`. The production server is started with the L2 manager as the storm simulator, so the endpoint is live.

---

## 10. CLI Examples

All examples run against a locally started simulator. The RESTCONF port comes from the configuration file (`restconf.port`, default 8080); there is no `--restconf-port` flag.

### 10.1. Starting the Server

```bash
go run ./cmd/simulator start --config configs/default.yaml
```

`configs/default.yaml` sets `restconf.port: 8080`, so the RESTCONF server listens on `:8080`. Change that key to use another port. The same command starts SNMP, NETCONF and the metrics endpoint.

### 10.2. List the VLANs

```bash
curl -s http://localhost:8080/restconf/data/sim-l2-switching:vlans \
  -H 'Accept: application/yang-data+json' | jq
```

```json
{
  "sim-l2-switching:vlans": {
    "vlan": [
      {
        "id": 100,
        "name": "DATA",
        "description": "default data vlan",
        "ports": {"port": [{"port": "eth0", "mode": "access", "pvid": 100, "tagged": false, "qinq": false}]}
      }
    ]
  }
}
```

### 10.3. Read One VLAN

```bash
curl -s http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=100 \
  -H 'Accept: application/yang-data+json' | jq
```

```json
{
  "sim-l2-switching:vlan": [
    {
      "id": 100,
      "name": "DATA",
      "description": "default data vlan",
      "ports": {"port": [{"port": "eth0", "mode": "access", "pvid": 100, "tagged": false, "qinq": false}]}
    }
  ]
}
```

A single list entry is still a one-element array.

### 10.4. Read STP State (all content)

```bash
curl -s 'http://localhost:8080/restconf/data/sim-l2-switching:stp/state?content=all' \
  -H 'Accept: application/yang-data+json' | jq
```

```json
{
  "sim-l2-switching:state": {
    "enabled": true,
    "protocol": "rstp",
    "bridge-priority": 32768,
    "bridge-address": "02:00:00:00:00:01",
    "root-id": "02:00:00:00:00:01",
    "root-cost": 0,
    "ports": {
      "port": [
        {"port": "eth0", "role": "root", "state": "forwarding", "priority": 128, "path-cost": 20000, "edge-port": false},
        {"port": "eth1", "role": "alternate", "state": "discarding", "priority": 128, "path-cost": 20000, "edge-port": false}
      ]
    }
  }
}
```

### 10.5. Create a VLAN and Read It Back

```bash
curl -i -X PUT http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=200 \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE"}]}'
# HTTP/1.1 201 Created
# Location: /restconf/data/sim-l2-switching:vlans/vlan=200

curl -s http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=200 \
  -H 'Accept: application/yang-data+json' | jq
```

```json
{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE","ports":{"port":[]}}]}
```

Repeating the same `PUT` replaces the entry and returns `200 OK`.

### 10.6. Patch a Leaf

```bash
curl -i -X PATCH http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=100/name \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-l2-switching:name":"DATA-NEW"}'
# HTTP/1.1 204 No Content
```

### 10.7. Create a VLAN with POST

```bash
curl -i -X POST http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-l2-switching:vlan":[{"id":300,"name":"TEST"}]}'
# HTTP/1.1 201 Created
# Location: /restconf/data/sim-l2-switching:vlans/vlan=300
```

Posting the same entry again returns `409 Conflict` (`data-exists`).

### 10.8. Delete a VLAN

```bash
curl -i -X DELETE http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=300
# HTTP/1.1 204 No Content
```

### 10.9. Delete a MAC Entry

```bash
curl -i -X DELETE \
  http://localhost:8080/restconf/data/sim-l2-switching:mac-table/entry=02:00:00:00:00:02
# HTTP/1.1 204 No Content
```

### 10.10. Inject a Broadcast Storm

```bash
curl -i -X POST http://localhost:8080/api/simulate/l2-storm \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"port":"eth0","packets":1500}'
# HTTP/1.1 202 Accepted
# Content-Type: application/yang-data+json
# {"packets":1500,"port":"eth0","status":"ok"}
```

### 10.11. Retrieve XML

```bash
curl -s http://localhost:8080/restconf/data/sim-device:system-info \
  -H 'Accept: application/yang-data+xml'
```

```xml
<?xml version="1.0" encoding="UTF-8"?>
<system-info xmlns="urn:sim:device"><device-id>sim-001</device-id><name>triadsim-01</name><description>TriadSim simulated telecom device</description><contact>noc@example.net</contact><location>lab</location><uptime>0</uptime></system-info>
```

A read of the whole datastore wraps the top-level nodes:

```bash
curl -s http://localhost:8080/restconf/data -H 'Accept: application/yang-data+xml'
# <?xml version="1.0" encoding="UTF-8"?>
# <data xmlns="urn:ietf:params:xml:ns:yang:ietf-restconf"><system-info xmlns="urn:sim:device">…</system-info>…</data>
```

### 10.12. Write the Candidate Datastore

```bash
curl -i -X PUT 'http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=200?datastore=candidate' \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE"}]}'
# HTTP/1.1 201 Created
```

The entry is then visible at `?datastore=candidate` but not in running until NETCONF commits the candidate.

---

## 11. References

| Document | Title |
|---|---|
| **RFC 8040** | RESTCONF Protocol (Standards Track) |
| **RFC 8072** | YANG Patch Media Type |
| **RFC 8700** | RESTCONF over HTTP/2 |
| **RFC 6020** | YANG — A Data Modeling Language for NETCONF |
| **RFC 6241** | Network Configuration Protocol (NETCONF) |
| **RFC 7230** | HTTP/1.1: Message Syntax and Routing |
| **RFC 7231** | HTTP/1.1: Semantics and Content |
| **RFC 7950** | The YANG 1.1 Data Modeling Language |
| **RFC 8527** | RESTCONF Extensions to Support the Network Management Datastore Architecture |
| **IANA** | RESTCONF Capability URNs registry |
