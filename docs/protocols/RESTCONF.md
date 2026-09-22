# RESTCONF.md

**Project:** TriadSim — Lightweight Telecom Equipment Simulator  
**Protocol:** RESTCONF (RFC 8040)  
**Transport:** HTTP/HTTPS over TCP  
**Default Port:** 8080  
**Base Path:** `/restconf`  
**Data Encoding:** `application/yang-data+json`, `application/yang-data+xml`

---

## 1. Introduction

RESTCONF is an HTTP-based protocol that provides a programmatic interface for accessing data defined in YANG, using the datastore concepts defined in the Network Configuration Protocol (NETCONF). It maps the NETCONF datastore model (running, candidate, startup) onto HTTP resources, allowing clients to perform CRUD operations using standard HTTP methods.

TriadSim implements RESTCONF as one of its three northbound management interfaces, alongside SNMP and NETCONF. The protocol is defined in RFC 8040 (Standards Track, January 2017). Extensions for YANG Patch are defined in RFC 8072, and support for HTTP/2 transport is described in RFC 8700.

RESTCONF in TriadSim is used for:

- **Configuration** — creating, replacing, modifying, and deleting data resources in the running and candidate datastores.
- **Monitoring** — retrieving operational state, configuration data, and statistics.
- **RPC invocation** — executing YANG `rpc` and `action` statements via POST.
- **Event streams** — subscribing to server-sent event notifications.

---

## 2. Protocol Overview

### 2.1. Architectural Model

RESTCONF follows a client-server model over HTTP:

| Role | Entity | Description |
|---|---|---|
| **RESTCONF client** | NMS, `curl`, custom tooling | Sends HTTP requests, receives responses and event streams |
| **RESTCONF server** | TriadSim simulator | Serves YANG-defined resources, processes RPCs |

The protocol is stateless at the HTTP layer. Each request carries all necessary information: method, URI, headers, and optional body. The server responds with an HTTP status code and, when applicable, a response body.

### 2.2. Relationship to NETCONF

RESTCONF is designed as a companion protocol to NETCONF. Both operate on the same YANG data models and datastore concepts. The key differences are:

| Aspect | NETCONF | RESTCONF |
|---|---|---|
| Transport | SSH | HTTP/HTTPS |
| Encoding | XML | JSON and XML |
| Protocol style | RPC-based | REST-based |
| Configuration model | Candidate + commit | Direct edit (or candidate via explicit datastore) |
| Notifications | NETCONF notifications | Server-Sent Events (SSE) |

RESTCONF does not replace NETCONF; it provides an alternative HTTP-based access path to the same underlying data. A server may support both simultaneously.

---

## 3. Media Types

### 3.1. YANG Data Media Types

RESTCONF defines two application-specific media types for serializing YANG data:

| Media Type | Encoding | RFC Reference |
|---|---|---|
| `application/yang-data+xml` | XML | RFC 8040, Section 11.3.1 |
| `application/yang-data+json` | JSON | RFC 8040, Section 11.3.2 |

The `application/yang-data+json` media type is the default for TriadSim RESTCONF responses when the client sends `Accept: application/yang-data+json`.

### 3.2. YANG Patch Media Types

YANG Patch (RFC 8072) defines two additional media types for ordered edit operations:

| Media Type | Encoding |
|---|---|
| `application/yang-patch+xml` | XML |
| `application/yang-patch+json` | JSON |

These are used with the PATCH method to apply a sequence of edits to a target resource. The YANG Patch capability is advertised via the URN `urn:ietf:params:restconf:capability:yang-patch:1.0`.

### 3.3. Other Media Types

| Media Type | Usage |
|---|---|
| `application/yang` | YANG schema retrieval (GET on `/restconf/data/ietf-yang-library:yang-library`) |
| `application/xrd+xml` | Root resource discovery (`/.well-known/host-meta`) |
| `text/event-stream` | Server-Sent Events (SSE) for notifications |

---

## 4. Resource Paths

### 4.1. Root Resource Discovery

Before accessing any RESTCONF resource, the client must discover the RESTCONF API root. This is done by retrieving `/.well-known/host-meta` and parsing the `<Link>` element with the `restconf` relation.

**Request:**
```http
GET /.well-known/host-meta HTTP/1.1
Host: localhost:8080
Accept: application/xrd+xml
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/xrd+xml
Content-Length: nnn

<XRD xmlns='http://docs.oasis-open.org/ns/xri/xrd-1.0'>
  <Link rel='restconf' href='/restconf'/>
</XRD>
```

The client then uses `/restconf` as the initial path component for all subsequent RESTCONF requests.

### 4.2. Path Structure

RESTCONF resource paths follow the YANG data tree hierarchy. The general form is:

```
{+restconf}/data/{+yang-data-path}
```

| Path Component | Description | Example |
|---|---|---|
| `{+restconf}` | RESTCONF root (discovered) | `/restconf` |
| `data` | Data resource root | `/restconf/data` |
| `{+module-name}:{+node-name}` | Module-qualified data node | `/restconf/data/sim-device:system-info` |
| `{+list-key}` | List key predicate | `/restconf/data/sim-l2-switching:vlans/vlan=100` |
| `{+container}` | Container node | `/restconf/data/sim-sync:ptp/clock` |
| `operations` | RPC operation root | `/restconf/operations/sim-radio:reset-stats` |
| `yang-library` | YANG module library | `/restconf/data/ietf-yang-library:yang-library` |

### 4.3. TriadSim Resource Paths

| Resource | Path | Description |
|---|---|---|
| System info | `/restconf/data/sim-device:system-info` | Device identity, uptime |
| Radio link | `/restconf/data/sim-radio-link:radio-link` | Radio link configuration and state |
| Radio link by name | `/restconf/data/sim-radio-link:radio-link[name=radio0]` | Specific radio link |
| VLANs | `/restconf/data/sim-l2-switching:vlans` | VLAN list |
| VLAN by ID | `/restconf/data/sim-l2-switching:vlans/vlan=100` | Specific VLAN |
| MAC table | `/restconf/data/sim-l2-switching:mac-table` | Bridge forwarding database |
| STP state | `/restconf/data/sim-l2-switching:stp/state` | STP/RSTP port states |
| PTP clock | `/restconf/data/sim-sync:ptp/clock` | PTP clock configuration and state |
| PTP state | `/restconf/data/sim-sync:ptp/clock/state` | Current PTP state |
| SyncE state | `/restconf/data/sim-sync:synce/state` | SyncE synchronization state |
| YANG library | `/restconf/data/ietf-yang-library:yang-library` | List of supported YANG modules |
| Operations | `/restconf/operations` | Available RPC operations |

---

## 5. HTTP Methods

RESTCONF maps CRUD operations to standard HTTP methods. The following table summarizes the RESTCONF methods defined in RFC 8040, Section 4:

| Method | RESTCONF Operation | NETCONF Equivalent | Idempotent | Safe |
|---|---|---|---|---|
| **GET** | Retrieve data | `get-config`, `get` | Yes | Yes |
| **HEAD** | Retrieve headers only | — | Yes | Yes |
| **POST** | Create resource / invoke RPC | `edit-config` (create), `rpc` | No | No |
| **PUT** | Create or replace resource | `edit-config` (create/replace) | Yes | No |
| **PATCH** | Merge or patch resource | `edit-config` (merge) | No | No |
| **DELETE** | Delete resource | `edit-config` (delete) | Yes | No |
| **OPTIONS** | Retrieve allowed methods | — | Yes | Yes |

### 5.1. GET

Retrieves the representation of a target resource. The response body contains the resource data in the requested media type.

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

{
  "sim-device:system-info": {
    "device-id": "sim-001",
    "uptime": 123
  }
}
```

### 5.2. POST

Two distinct uses:

1. **Create a data resource** — when the target is a collection (list or leaf-list), POST creates a new entry.
2. **Invoke an RPC operation** — when the target is an operation resource (`/restconf/operations/...`), POST executes the RPC.

**Example — invoke RPC:**
```http
POST /restconf/operations/sim-radio:reset-stats HTTP/1.1
Host: localhost:8080
Content-Type: application/yang-data+json

{
  "sim-radio:input": {
    "link": "radio0"
  }
}
```

**Response:**
```http
HTTP/1.1 200 OK
Content-Type: application/yang-data+json

{
  "sim-radio:output": {
    "status": "ok"
  }
}
```

### 5.3. PUT

Creates or completely replaces the target resource. If the resource does not exist, it is created; if it exists, it is replaced. A `201 Created` status is returned if the resource was newly created; `200 OK` if replaced.

**Example — create or replace a VLAN:**
```http
PUT /restconf/data/sim-l2-switching:vlans/vlan=100 HTTP/1.1
Host: localhost:8080
Content-Type: application/yang-data+json

{
  "sim-l2-switching:vlan": [
    {
      "id": 100,
      "name": "DATA"
    }
  ]
}
```

### 5.4. PATCH

Two patch types are supported:

1. **Plain patch** (`application/yang-data+json` or `application/yang-data+xml`) — performs a merge operation on the target resource.
2. **YANG Patch** (`application/yang-patch+json` or `application/yang-patch+xml`) — applies an ordered list of edits with precise control (create, delete, insert, merge, move, replace).

**Example — plain patch (merge) on PTP clock:**
```http
PATCH /restconf/data/sim-sync:ptp/clock HTTP/1.1
Host: localhost:8080
Content-Type: application/yang-data+json

{
  "sim-sync:mode": "master",
  "sim-sync:domain": 24
}
```

### 5.5. DELETE

Deletes the target resource. A `204 No Content` status is returned on success.

**Example — delete a VLAN:**
```http
DELETE /restconf/data/sim-l2-switching:vlans/vlan=100 HTTP/1.1
Host: localhost:8080
```

### 5.6. OPTIONS

Returns the HTTP methods allowed for the target resource in the `Allow` header.

**Example:**
```http
OPTIONS /restconf/data/sim-radio-link:radio-link HTTP/1.1
Host: localhost:8080
```

**Response:**
```http
HTTP/1.1 200 OK
Allow: GET, HEAD, POST, PUT, PATCH, DELETE, OPTIONS
```

---

## 6. Query Parameters

RESTCONF supports several query parameters that modify the behavior of GET and other requests. These are appended to the request URI after `?`.

| Parameter | Applies To | Description | Capability URN |
|---|---|---|---|
| `depth` | GET, HEAD | Limit subtree depth of returned data | `urn:ietf:params:restconf:capability:depth:1.0` |
| `fields` | GET, HEAD | Select specific fields to return | `urn:ietf:params:restconf:capability:fields:1.0` |
| `filter` | GET, HEAD | Filter data by value or XPath | `urn:ietf:params:restconf:capability:filter:1.0` |
| `with-defaults` | GET, HEAD | Control default value reporting | `urn:ietf:params:restconf:capability:with-defaults:1.0` |
| `content` | GET, HEAD | Select `config`, `nonconfig`, or `all` | — |
| `replay` | SSE | Replay stored notifications | `urn:ietf:params:restconf:capability:replay:1.0` |

The complete list of RESTCONF capability URNs is maintained by IANA.

**Example — retrieve only the RSSI field:**
```http
GET /restconf/data/sim-radio-link:radio-link?fields=rssi HTTP/1.1
Host: localhost:8080
Accept: application/yang-data+json
```

**Example — limit depth to 1:**
```http
GET /restconf/data/sim-sync:ptp?depth=1 HTTP/1.1
Host: localhost:8080
```

---

## 7. Error Handling

### 7.1. Error Response Format

When an HTTP status code in the 4xx or 5xx range is returned, the response body SHOULD contain structured error information. RESTCONF uses the `ietf-restconf` YANG module's `error` structure, defined in RFC 8040, Section 7.1.

**JSON error response:**
```json
{
  "ietf-restconf:errors": {
    "error": [
      {
        "error-type": "application",
        "error-tag": "invalid-value",
        "error-app-tag": "tx-power-out-of-range",
        "error-path": "/sim-radio-link:radio-link/tx-power",
        "error-message": "TX power 999.0 is outside the valid range [-10, 30]",
        "error-info": {
          "bad-element": "tx-power"
        }
      }
    ]
  }
}
```

### 7.2. Error Fields

| Field | Description |
|---|---|
| `error-type` | Error category: `transport`, `rpc`, `protocol`, `application` |
| `error-tag` | Protocol-level error identifier (e.g., `invalid-value`, `operation-failed`) |
| `error-app-tag` | Application-specific error identifier |
| `error-path` | Path to the resource that caused the error |
| `error-message` | Human-readable description |
| `error-info` | Additional protocol-specific error information |

### 7.3. HTTP Status Code Mapping

The following table maps common RESTCONF error tags to HTTP status codes:

| error-tag | HTTP Status Code | Condition |
|---|---|---|
| `invalid-value` | 400, 404, 406 | Invalid value, missing resource, or unsupported encoding |
| `malformed-message` | 400 | Request body is not well-formed |
| `unknown-attribute` | 400 | Unknown attribute in request |
| `bad-element` | 400 | Invalid XML/JSON element |
| `too-big` | 400 | Request or response exceeds size limit |
| `operation-not-supported` | 501 | Operation not implemented by server |
| `resource-denied` | 403 | Access to resource denied |
| `operation-failed` | 500 | Internal server error |
| `in-use` | 409 | Resource is in use |

When an error occurs and the status code is in the 4xx range (except 403 Forbidden), the server SHOULD include the error structure in the response body.

### 7.4. Common Error Scenarios

**Invalid value (400):**
```http
HTTP/1.1 400 Bad Request
Content-Type: application/yang-data+json

{
  "ietf-restconf:errors": {
    "error": [{
      "error-type": "application",
      "error-tag": "invalid-value",
      "error-message": "tx-power out of range: 999"
    }]
  }
}
```

**Resource not found (404):**
```http
HTTP/1.1 404 Not Found
Content-Type: application/yang-data+json

{
  "ietf-restconf:errors": {
    "error": [{
      "error-type": "application",
      "error-tag": "invalid-value",
      "error-message": "VLAN 999 not found"
    }]
  }
}
```

**Unsupported media type (415):**
```http
HTTP/1.1 415 Unsupported Media Type
Content-Type: application/yang-data+json

{
  "ietf-restconf:errors": {
    "error": [{
      "error-type": "protocol",
      "error-tag": "operation-not-supported",
      "error-message": "Content-Type application/xml is not supported"
    }]
  }
}
```

---

## 8. Capabilities

RESTCONF servers advertise optional capabilities via the `ietf-restconf-monitoring` module's `capability` leaf-list. TriadSim supports the following:

| Capability URN | Description |
|---|---|
| `urn:ietf:params:restconf:capability:defaults:1.0` | Default value handling |
| `urn:ietf:params:restconf:capability:depth:1.0` | Depth-limited retrieval |
| `urn:ietf:params:restconf:capability:fields:1.0` | Field selection |
| `urn:ietf:params:restconf:capability:filter:1.0` | Data filtering |
| `urn:ietf:params:restconf:capability:with-defaults:1.0` | Default value reporting |
| `urn:ietf:params:restconf:capability:yang-patch:1.0` | YANG Patch support |

**Example — retrieve capabilities:**
```http
GET /restconf/data/ietf-restconf-monitoring:restconf-state/capabilities HTTP/1.1
Host: localhost:8080
Accept: application/yang-data+json
```

**Response:**
```json
{
  "ietf-restconf-monitoring:capabilities": {
    "capability": [
      "urn:ietf:params:restconf:capability:depth:1.0",
      "urn:ietf:params:restconf:capability:fields:1.0",
      "urn:ietf:params:restconf:capability:filter:1.0",
      "urn:ietf:params:restconf:capability:with-defaults:1.0",
      "urn:ietf:params:restconf:capability:yang-patch:1.0"
    ]
  }
}
```

---

## 9. Simulator Implementation

### 9.1. HTTP Server

TriadSim uses Go's standard `net/http` package with the `chi` router for path matching. The server is stateless at the HTTP layer; state resides in the `internal/store` package (running/candidate datastores).

### 9.2. Path-to-Model Resolution

The `internal/router` package maps RESTCONF paths to Go model fields using the `path` tag on model structs. The mapping is bidirectional:

- **GET**: path → model → serialize to JSON/XML
- **PUT/POST/PATCH/DELETE**: path + body → validate → apply to datastore

### 9.3. Datastore Integration

RESTCONF operations target the running datastore by default. The candidate datastore can be accessed via the query parameter `?datastore=candidate` (TriadSim extension for demonstration purposes; standard RESTCONF does not define a datastore query parameter).

### 9.4. Content Negotiation

TriadSim supports both JSON and XML encoding. The server selects the encoding based on the `Accept` header:

- `Accept: application/yang-data+json` → JSON response
- `Accept: application/yang-data+xml` → XML response
- No `Accept` header → JSON is the default

### 9.5. RPC Operations

All YANG `rpc` statements are available as RESTCONF operation resources under `/restconf/operations/`. TriadSim implements the following RPCs:

| RPC | Path | Description |
|---|---|---|
| `sim-radio:reset-stats` | `/restconf/operations/sim-radio:reset-stats` | Reset radio statistics |
| `sim-radio:inject-alarm` | `/restconf/operations/sim-radio:inject-alarm` | Inject a radio alarm |
| `sim-sync:force-holdover` | `/restconf/operations/sim-sync:force-holdover` | Force PTP into holdover |
| `sim-l2:clear-mac-table` | `/restconf/operations/sim-l2:clear-mac-table` | Clear MAC forwarding table |

### 9.6. Event Streams

RESTCONF notifications are delivered via Server-Sent Events (SSE) using the `text/event-stream` media type. TriadSim exposes a single stream:

```
GET /restconf/streams/sim-events
```

Events are JSON-encoded and follow the SSE format:

```
event: notification
data: {"sim-events:event": {"type": "AlarmRaised", "resource": "radio0", "severity": "major"}}

```

---

## 10. CLI Examples

### 10.1. Starting the Server

```bash
go run ./cmd/simulator start --config configs/default.yaml
```

The RESTCONF server listens on `:8080` by default. The port can be overridden:

```bash
go run ./cmd/simulator start --restconf-port 9090
```

### 10.2. Retrieve System Info

```bash
curl -s http://localhost:8080/restconf/data/sim-device:system-info \
  -H 'Accept: application/yang-data+json' | jq
```

### 10.3. Create a VLAN

```bash
curl -X PUT http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=100 \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-l2-switching:vlan":[{"id":100,"name":"DATA"}]}'
```

### 10.4. Configure PTP

```bash
curl -X PATCH http://localhost:8080/restconf/data/sim-sync:ptp/clock \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-sync:mode":"master","sim-sync:domain":24}'
```

### 10.5. Retrieve PTP State

```bash
curl -s http://localhost:8080/restconf/data/sim-sync:ptp/clock/state \
  -H 'Accept: application/yang-data+json' | jq
# {"sim-sync:state":"holdover"}
```

### 10.6. Inject an Alarm via RPC

```bash
curl -X POST http://localhost:8080/restconf/operations/sim-radio:inject-alarm \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-radio:input":{"link":"radio0","type":"radioLinkDown"}}'
```

### 10.7. Delete a VLAN

```bash
curl -X DELETE http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=100
```

### 10.8. Subscribe to Events

```bash
curl -N http://localhost:8080/restconf/streams/sim-events \
  -H 'Accept: text/event-stream'
```

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
