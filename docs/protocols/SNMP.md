# SNMP.md

**Project:** TriadSim — Lightweight Telecom Equipment Simulator  
**Protocol:** Simple Network Management Protocol, Version 2c (SNMPv2c)  
**Transport:** UDP  
**Default Agent Port:** 1161  
**Default Trap Receiver Port:** 1162  
**Default Community String:** `public`

---

## 1. Introduction

The Simple Network Management Protocol (SNMP) is an Internet Standard protocol for managing devices on IP networks. It provides a framework for monitoring and controlling network elements through a virtual information store called the Management Information Base (MIB) . TriadSim implements SNMPv2c as one of its three northbound management interfaces, alongside NETCONF and RESTCONF.

SNMPv2c is the "community-based" variant of SNMPv2. It is not an Internet Standards Track protocol in itself, but its protocol operations are defined in RFC 3416, which is an Internet Standard (STD 62) . The "c" in SNMPv2c stands for "community," reflecting its use of community strings for authentication, inherited from the original SNMPv1 framework .

TriadSim uses SNMPv2c exclusively for the following operations:

- **Monitoring** — retrieving read-only operational data (RSSI, fade margin, capacity, PTP state, interface counters, MAC table).
- **Configuration** — setting writable parameters (TX power, ATPC target, VLAN membership, PTP domain).
- **Traps** — asynchronous notifications for alarms and state changes.

SNMPv3 is out of scope for the current project iteration.

---

## 2. Protocol Overview

### 2.1. Communication Model

SNMP follows a manager–agent model:

| Role | Entity | Description |
|---|---|---|
| **Manager** | NMS, CLI tools, monitoring systems | Sends requests, receives traps |
| **Agent** | TriadSim simulator | Responds to requests, generates traps |

The manager sends requests to the agent on UDP port 1161. The agent responds to the source address. Traps are sent asynchronously from the agent to one or more configured trap receivers on UDP port 1162 .

### 2.2. Message Structure

An SNMPv2c message consists of:

```
Message ::= SEQUENCE {
    version   INTEGER,    -- version-1(0) for SNMPv2c
    community OCTET STRING, -- community name
    data      PDU         -- protocol data unit
}
```

The version field for SNMPv2c is encoded as `1` (the same integer value as SNMPv1), which is a known ambiguity in the SNMPv2c specification. The community field carries the community string used for authentication .

### 2.3. Supported PDUs

TriadSim supports the following SNMPv2c PDUs, as defined in RFC 3416 :

| PDU | Direction | Description |
|---|---|---|
| **GetRequest** | Manager → Agent | Retrieve the value of one or more object instances |
| **GetNextRequest** | Manager → Agent | Retrieve the lexicographically next object instance |
| **GetBulkRequest** | Manager → Agent | Retrieve multiple values efficiently (SNMPv2c only) |
| **SetRequest** | Manager → Agent | Modify the value of one or more object instances |
| **GetResponse** | Agent → Manager | Reply to GetRequest, GetNextRequest, GetBulkRequest, or SetRequest |
| **SNMPv2-Trap** | Agent → Manager | Unconfirmed notification of an event or condition |

`InformRequest` (confirmed notification) is not supported in the current implementation.

---

## 3. OID Tree

### 3.1. Root Hierarchy

Object Identifiers (OIDs) are structured as a hierarchical tree. The path from the root to the MIB-II subtree is:

```
iso(1)
└── org(3)
    └── dod(6)
        └── internet(1)
            └── mgmt(2)
                └── mib-2(1)
```

This yields the base OID `1.3.6.1.2.1` for all MIB-II objects .

### 3.2. Relevant Subtrees

| Subtree | Base OID | Contents |
|---|---|---|
| **system** | `1.3.6.1.2.1.1` | Device identity, uptime, contact, location |
| **interfaces** | `1.3.6.1.2.1.2` | Interface table (`ifTable`), counters, descriptions |
| **snmp** | `1.3.6.1.2.1.11` | SNMP statistics, trap configuration |
| **dot1dTpFdb** | `1.3.6.1.2.1.17.4.3` | Bridge forwarding database (MAC table) |
| **enterprises** | `1.3.6.1.4.1` | Vendor-specific OIDs |

### 3.3. Vendor-Specific OIDs

TriadSim uses the enterprise OID space for domain-specific metrics that have no standard MIB representation. The assigned enterprise number is `99999` (placeholder for development).

The radio objects below are **implemented in Phase 1.8**. They are scalar objects (`.0`) because the MVP has one radio link; `internal/router/oid.go` builds them from the first interface whose `type` is `radio`.

| OID | Object | Type | Access | Description |
|---|---|---|---|---|
| `1.3.6.1.4.1.99999.1.1.1.0` | `simRadioRssi` | OpaqueDouble | read-only | Current RSSI in dBm |
| `1.3.6.1.4.1.99999.1.1.2.0` | `simRadioFadeMargin` | OpaqueDouble | read-only | Fade margin in dB |
| `1.3.6.1.4.1.99999.1.1.3.0` | `simRadioCapacity` | Gauge32 | read-only | Current capacity in Mbps |
| `1.3.6.1.4.1.99999.1.1.4.0` | `simRadioTxPower` | OpaqueDouble | read-write | Configured TX power in dBm |
| `1.3.6.1.4.1.99999.1.1.5.0` | `simRadioAtpcEnabled` | Integer TruthValue | read-write | ATPC enabled (1) or disabled (2) |
| `1.3.6.1.4.1.99999.1.1.6.0` | `simRadioAcmProfile` | Integer | read-only | Active ACM profile index (1-12) |
| `1.3.6.1.4.1.99999.1.1.7.0` | `simRadioAcmCapacity` | Gauge32 | read-only | Capacity of the active profile in Mbps |
| `1.3.6.1.4.1.99999.1.1.8.0` | `simRadioLinkState` | Integer | read-only | Radio link state: `up(1)`, `degraded(2)`, `down(3)` — Phase 6.2 |

Floats are encoded as [RFC 5342](https://www.rfc-editor.org/rfc/rfc5342) `OpaqueDouble` (`0x79`) because SMIv2 has no native floating-point type; `snmpget`/`snmpwalk` display them as opaque floats.

The synchronization objects below are **implemented in Phase 5**. The clock is a single object (scalars end in `.0`); the SyncE interface table is indexed by the interface index (radio0=1, eth0=2, eth1=3). `simRadioLinkState` (`.1.1.8`) completes the radio subtree in Phase 6.2; there is no other vendor object, and the L2 state is exposed through the standard MIBs above rather than a vendor subtree.

| OID | Object | Type | Access | Description |
|---|---|---|---|---|
| `1.3.6.1.4.1.99999.2.1.1.0` | `simSyncPtpState` | Integer | read-only | Clock state: freerun(1), acquiring(2), locked(3), holdover-in-spec(4), holdover-out-of-spec(5) |
| `1.3.6.1.4.1.99999.2.1.2.0` | `simSyncPtpOffset` | OpaqueDouble | read-only | Clock offset in nanoseconds |
| `1.3.6.1.4.1.99999.2.1.3.0` | `simSyncPtpDomain` | Gauge32 | read-write | PTP domain (0-127) |
| `1.3.6.1.4.1.99999.2.1.4.0` | `simSyncPtpPriority1` | Gauge32 | read-write | BMCA priority1 |
| `1.3.6.1.4.1.99999.2.1.5.0` | `simSyncSyncEQL` | Integer | read-only | QL of the selected SyncE source, as an SSM code |
| `1.3.6.1.4.1.99999.2.1.6.0` | `simSyncSyncEExtendedQL` | Integer | read-only | eSSM code of the selected source, 0 when unset |
| `1.3.6.1.4.1.99999.2.1.7.1.1.<ifIndex>` | `simSyncSyncEIfQL` | Integer | read-only | QL of one SyncE interface, as an SSM code |
| `1.3.6.1.4.1.99999.2.1.7.1.2.<ifIndex>` | `simSyncSyncEIfSSMEnabled` | Integer TruthValue | read-only | SSM enabled (1) or disabled (2) |
| `1.3.6.1.4.1.99999.2.1.8.0` | `simSyncPtpJitter` | OpaqueDouble | read-only | Clock jitter in nanoseconds |

The SSM codes are `QL-PRC`=2, `QL-SSU-A`=4, `QL-SSU-B`=8, `QL-SEC`=11 and `QL-DNU`=15; the extended codes are `QL-PRTC`=0x20, `QL-ePRTC`=0x21 and `QL-eEEC`=0x22. See `docs/protocols/PTP.md` and `docs/protocols/SYNCE.md`.

---

## 4. MIB Modules

TriadSim does not parse MIB files at runtime. Instead, it uses Go structs with OID metadata. However, the following standard MIB modules are referenced for OID correctness and client interoperability:

| MIB Module | RFC | Purpose |
|---|---|---|
| **SNMPv2-SMI** | RFC 2578 | Structure of Management Information, OID definitions |
| **SNMPv2-TC** | RFC 2579 | Textual conventions (`DisplayString`, `TimeStamp`, `MacAddress`) |
| **SNMPv2-MIB** | RFC 3418 | `sysUpTime`, `sysDescr`, `snmpTrapOID` |
| **IF-MIB** | RFC 2863 | `ifDescr`, `ifType`, `ifMtu`, `ifPhysAddress`, `ifAdminStatus`, `ifOperStatus`, the `ifTable` counters and the 64-bit `ifXTable` counters |
| **BRIDGE-MIB** | RFC 4188 | `dot1dBaseBridgeAddress`, `dot1dStpPriority`, `dot1dStpPortTable`, `dot1dTpAgingTime`, `dot1dTpFdbTable` |
| **Q-BRIDGE-MIB** | RFC 4363 | `dot1qVlanStaticName` |

### 4.1. Textual Conventions

The following textual conventions from RFC 2579 are used by TriadSim MIB objects :

- **`DisplayString`** — OCTET STRING (SIZE (0..255)), NVT ASCII. Used for `ifDescr`, `sysDescr`, `sysContact`.
- **`PhysAddress`** — OCTET STRING. Used for MAC addresses in `dot1dTpFdbAddress`.
- **`TimeStamp`** — TimeTicks. Used for `sysUpTime` and trap timestamps.

### 4.2. Standard OID Reference

| Object | OID | Type | Access | Implemented |
|---|---|---|---|---|
| `sysDescr` | `1.3.6.1.2.1.1.1` | DisplayString | read-only | Phase 1.8 (`system-info/description`) |
| `sysObjectID` | `1.3.6.1.2.1.1.2` | OBJECT IDENTIFIER | read-only | Phase 1.8 (constant `1.3.6.1.4.1.99999.1`) |
| `sysUpTime` | `1.3.6.1.2.1.1.3` | TimeTicks | read-only | Phase 1.8 (`system-info/uptime`, ×100) |
| `sysContact` | `1.3.6.1.2.1.1.4` | DisplayString | read-write | Phase 1.8 |
| `sysName` | `1.3.6.1.2.1.1.5` | DisplayString | read-write | Phase 1.8 |
| `sysLocation` | `1.3.6.1.2.1.1.6` | DisplayString | read-write | Phase 1.8 |
| `ifIndex` | `1.3.6.1.2.1.2.2.1.1` | INTEGER | read-only | Phase 1.8 (1-based position) |
| `ifDescr` | `1.3.6.1.2.1.2.2.1.2` | DisplayString | read-only | Phase 1.8 |
| `ifType` | `1.3.6.1.2.1.2.2.1.3` | INTEGER | read-only | Phase 1.8 (radio → `other(1)`, ethernet → `ethernetCsmacd(6)`) |
| `ifOperStatus` | `1.3.6.1.2.1.2.2.1.8` | INTEGER | read-only | Phase 1.8 (`up(1)`/`down(2)`) |
| `ifMtu` | `1.3.6.1.2.1.2.2.1.4` | INTEGER | read-only | Phase 4.11 (`mtu`) |
| `ifPhysAddress` | `1.3.6.1.2.1.2.2.1.6` | PhysAddress | read-only | Phase 4.11 (`mac-address`, six octets) |
| `ifAdminStatus` | `1.3.6.1.2.1.2.2.1.7` | INTEGER | read-write | Phase 4.11 (`enabled`, `up(1)`/`down(2)`) |
| `ifInOctets` | `1.3.6.1.2.1.2.2.1.10` | Counter32 | read-only | Phase 4.11 |
| `ifInUcastPkts` | `1.3.6.1.2.1.2.2.1.11` | Counter32 | read-only | Phase 4.11 |
| `ifInDiscards` | `1.3.6.1.2.1.2.2.1.13` | Counter32 | read-only | Phase 4.11 |
| `ifInErrors` | `1.3.6.1.2.1.2.2.1.14` | Counter32 | read-only | Phase 4.11 |
| `ifOutOctets` | `1.3.6.1.2.1.2.2.1.16` | Counter32 | read-only | Phase 4.11 |
| `ifOutUcastPkts` | `1.3.6.1.2.1.2.2.1.17` | Counter32 | read-only | Phase 4.11 |
| `ifOutDiscards` | `1.3.6.1.2.1.2.2.1.19` | Counter32 | read-only | Phase 4.11 |
| `ifOutErrors` | `1.3.6.1.2.1.2.2.1.20` | Counter32 | read-only | Phase 4.11 |
| `ifName` | `1.3.6.1.2.1.31.1.1.1.1` | DisplayString | read-only | Phase 4.11 |
| `ifHCInOctets` | `1.3.6.1.2.1.31.1.1.1.6` | Counter64 | read-only | Phase 4.11 |
| `ifHCInUcastPkts` | `1.3.6.1.2.1.31.1.1.1.7` | Counter64 | read-only | Phase 4.11 |
| `ifHCOutOctets` | `1.3.6.1.2.1.31.1.1.1.10` | Counter64 | read-only | Phase 4.11 |
| `ifHCOutUcastPkts` | `1.3.6.1.2.1.31.1.1.1.11` | Counter64 | read-only | Phase 4.11 |
| `dot1dBaseBridgeAddress` | `1.3.6.1.2.1.17.1.1` | MacAddress | read-only | Phase 4.11 (`stp/state/bridge-address`) |
| `dot1dStpPriority` | `1.3.6.1.2.1.17.2.2` | INTEGER | read-write | Phase 4.11 (`stp/state/bridge-priority`) |
| `dot1dStpPortPriority` | `1.3.6.1.2.1.17.2.15.1.2` | INTEGER | read-write | Phase 4.11 (`stp/state/ports/port/priority`) |
| `dot1dStpPortState` | `1.3.6.1.2.1.17.2.15.1.3` | INTEGER | read-only | Phase 4.11 (RSTP `discarding` → `blocking(2)`) |
| `dot1dStpPortEnable` | `1.3.6.1.2.1.17.2.15.1.4` | INTEGER | read-only | Phase 4.11 |
| `dot1dStpPortPathCost` | `1.3.6.1.2.1.17.2.15.1.5` | INTEGER | read-write | Phase 4.11 |
| `dot1dTpAgingTime` | `1.3.6.1.2.1.17.4.2` | INTEGER | read-write | Phase 4.11 (`mac-table/aging-time`) |
| `dot1dTpFdbAddress` | `1.3.6.1.2.1.17.4.3.1.1` | MacAddress | read-only | Phase 4.11 (index is the six MAC octets) |
| `dot1dTpFdbPort` | `1.3.6.1.2.1.17.4.3.1.2` | INTEGER | read-only | Phase 4.11 (bridge port number) |
| `dot1dTpFdbStatus` | `1.3.6.1.2.1.17.4.3.1.3` | INTEGER | read-only | Phase 4.11 (`dynamic` → `learned(3)`, `static` → `mgmt(5)`) |
| `dot1qVlanStaticName` | `1.3.6.1.2.1.17.7.1.4.3.1.1` | DisplayString | read-only | Phase 4.11 (index is the VLAN id) |
| `snmpTrapOID.0` | `1.3.6.1.6.3.1.1.4.1.0` | OBJECT IDENTIFIER | — (trap varbind) | Phase 6.3 (implemented) |

Scalar objects are addressed with the `.0` instance (`sysDescr.0`); interface columns append the
1-based interface index (`ifDescr.1`). The bridge tables use their own indexes:
`dot1dStpPortTable` by the bridge port number (the same 1-based interface index),
`dot1dTpFdbTable` by the six MAC octets (`dot1dTpFdbPort.2.0.0.0.0.2` is the seeded entry for
`02:00:00:00:00:02`) and `dot1qVlanStaticName` by the VLAN identifier. The agent walks these
instances in numeric OID order, and the tables are built from the hydrated datastore, so an
entry learned or created at runtime gets an instance without a restart.

A walk of the forwarding database therefore looks like:

```bash
snmpwalk -v2c -c public 127.0.0.1:161 1.3.6.1.2.1.17.4.3.1.2
# SNMPv2-SMI::mib-2.17.4.3.1.2.2.0.0.0.0.2 = INTEGER: 3
```

The `ifDescr` object is particularly important for TriadSim, as it exposes the simulated interface names (`radio0`, `eth0`, `eth1`) to the NMS .

---

## 5. SNMP Operations

### 5.1. GetRequest

Retrieves the value of one or more object instances by OID. The agent responds with a `GetResponse` containing the requested values or an error indication.

```
Manager                          Agent
   │                               │
   │  GetRequest                   │
   │  OID: 1.3.6.1.2.1.1.1        │
   │ ────────────────────────────► │
   │                               │
   │  GetResponse                  │
   │  Value: "TriadSim v0.1"      │
   │ ◄──────────────────────────── │
```

### 5.2. GetNextRequest

Retrieves the lexicographically next object instance. Used for MIB tree traversal and table walking. Repeated `GetNextRequest` calls are the basis of the `snmpwalk` utility .

### 5.3. GetBulkRequest

Introduced in SNMPv2, this PDU allows the manager to request multiple values in a single operation, significantly reducing the number of round trips required for table retrieval . TriadSim supports `GetBulkRequest` with configurable `max-repetitions` and `non-repeaters` parameters.

### 5.4. SetRequest

Modifies the value of one or more writable object instances. TriadSim validates incoming values against the model's `Validate()` method before applying changes. Invalid values result in an error response with the appropriate error status.

Example: Setting `simRadioTxPower` to 20.0 dBm:

```
SetRequest:
  OID: 1.3.6.1.4.1.99999.1.1.4.0
  Value: 20.0

GetResponse:
  error-status: noError(0)
```

**Implementation (Phase 1.8, table objects Phase 4).** A `SetRequest` writes the `running`
datastore (writable-running semantics). The agent resolves every varbind through `internal/router`
using the model path of the binding it just looked up, so an object of a table that grew at
runtime is writable as well; it rejects a read-only
object with `ReadOnly` (`noSuchName`, `wrongType` and `wrongValue` are used for an unknown OID, a
bad value type and a value the model rejects), then builds the whole proposed running
configuration, validates it with the router and only then applies the writes. A multi-varbind
`Set` is therefore all-or-nothing: a value the model rejects leaves the device unchanged.

---

## 6. Traps

> **Implemented in Phase 6.3.** `internal/snmp/trap.go` subscribes to the EventBus, maps an event
> to a trap OID, builds the PDU and sends it to the configured destination. Delivery is
> fire-and-forget, as the unconfirmed nature of an SNMPv2 trap implies.

### 6.1. Trap PDU Format

An SNMPv2-Trap-PDU is generated by the agent to notify a remote receiver that an event has occurred. There is no confirmation associated with trap delivery .

The variable binding list of an SNMPv2-Trap-PDU has the following structure:

| Position | Varbind | Description |
|---|---|---|
| 1 | `sysUpTime.0` | Timestamp of the event, in hundredths of a second since agent initialization |
| 2 | `snmpTrapOID.0` | OID identifying the trap type |
| 3..n | Additional varbinds | Trap-specific data |

The first two varbinds are mandatory for all SNMPv2-Trap-PDUs .

### 6.2. TriadSim Trap OIDs

| Trap OID | Name | Severity | Trigger |
|---|---|---|---|
| `1.3.6.1.4.1.99999.0.1` | `simRadioLinkDown` | Major | `AlarmRaised` with `Alarm=radioLinkDown` — a failed radio link |
| `1.3.6.1.4.1.99999.0.2` | `simRadioLinkUp` | Info | `AlarmCleared` with `Alarm=radioLinkDown` — the link recovered |
| `1.3.6.1.4.1.99999.0.3` | `simSyncHoldover` | Major | `StateTransition` into `holdover-in-spec` or `holdover-out-of-spec` |
| `1.3.6.1.4.1.99999.0.4` | `simSyncRestored` | Info | `StateTransition` from a holdover state back to `locked` |
| `1.3.6.1.4.1.99999.0.5` | `simL2StormDetected` | Warning | `AlarmRaised` with `Alarm=l2Storm` — broadcast storm above the threshold |
| `1.3.6.1.4.1.99999.0.6` | `simConfigChanged` | Info | `ConfigChanged` — a commit changed the running configuration |

`internal/snmp/trap.go` derives the trap from the structured event fields, so no message text is
parsed: an alarm keeps its name across the raised and the cleared event and the event type selects
the trap. `radioLinkDegraded` and a cleared storm raise a notification and a metric but have no trap
OID. An event that maps to no trap is dropped with a debug log.

### 6.3. Trap Example

A `simRadioLinkDown` trap triggered by a simulated radio failure:

```
SNMPv2-Trap-PDU:
  version: 1 (SNMPv2c)
  community: public

  varbind[1]:
    OID: 1.3.6.1.2.1.1.3.0        (sysUpTime.0)
    Value: 1234567                  (12345.67 seconds)

  varbind[2]:
    OID: 1.3.6.1.6.3.1.1.4.1.0      (snmpTrapOID.0)
    Value: 1.3.6.1.4.1.99999.0.1   (simRadioLinkDown)

  varbind[3]:
    OID: 1.3.6.1.4.1.99999.1.1.1.0 (simRadioRssi.0)
    Value: -82.5

  varbind[4]:
    OID: 1.3.6.1.2.1.2.2.1.2.1     (ifDescr.1)
    Value: "radio0"
```

### 6.4. Trap Delivery

Traps are sent to a single destination taken from the YAML config file: a host and a port. The MVP
has no receiver list and no per-receiver community.

```yaml
snmp:
  port: 1161            # agent
  trap-host: 127.0.0.1  # trap receiver address
  trap-port: 1162       # trap receiver port
```

Traps go to `snmp.trap-host:snmp.trap-port` and carry community `public`. Trap delivery is fire-and-forget. If the destination is unreachable, the trap is silently dropped; no retransmission is attempted. This is consistent with the unconfirmed nature of SNMPv2-Trap-PDUs .

---

## 7. Community Strings

### 7.1. Overview

Community strings are the sole authentication mechanism in SNMPv2c. A community string is an OCTET STRING carried in every SNMP message. It acts as a password that defines the relationship between the manager and the agent .

RFC 1157 does not impose an explicit size constraint on community strings; their length is constrained indirectly by the maximum SNMP message size .

### 7.2. TriadSim Community Configuration

TriadSim uses a single community string for all operations:

| Community | Access | Description |
|---|---|---|
| `public` | read-only + read-write | Full access to all MIB objects |

No separate read-only or read-write communities are defined. The community string is fixed to `public` in the MVP and is not configurable through the YAML file. For demonstration purposes, authentication is intentionally absent.

### 7.3. Security Considerations

The SNMPv2c community-based security model provides no confidentiality, no integrity protection, and no replay protection. Any observer on the network path can read the community string and all SNMP payloads in cleartext. This is acceptable for a simulator running in a trusted lab environment but is not suitable for production networks. SNMPv3 with the User-based Security Model (USM) is the standard solution for authenticated and encrypted SNMP; it is out of scope for TriadSim.

---

## 8. Simulator OID Implementation

### 8.1. OID Resolution

TriadSim does not compile or parse MIB files. Instead, the `internal/router` package maps OIDs to Go model fields using the `path` tag on model structs. The mapping is bidirectional:

- **GET/SET**: OID → model path → field value
- **Trap generation**: model path → OID → varbind

### 8.2. OID Registration

`internal/router/oid.go` holds one static table of `objectRule` entries. Each rule names a model
path (or a path suffix for a table column) and the OID base, ASN.1 type and access:

```go
type objectRule struct {
    scope    scope     // scalar, interface column or radio scalar
    suffix   string    // model path, or path relative to the interface / radio-link
    oid      string    // base OID, without the instance
    typ      SNMPType  // Integer, OctetString, Gauge32, TimeTicks, OpaqueDouble, ...
    writable bool
    convert  func(any) any          // model value -> SNMP value (enums, units)
    decode   func(any) (any, error) // SNMP value -> model value (TruthValue)
}
```

The router applies the rules to the model template, so interface names, their 1-based indexes
and the presence of a radio link come from `model.Device` rather than from hardcoded paths. It
then reads each object's current value from the store and returns sorted `Binding`s. The agent's
`internal/snmp/oid.go` indexes those bindings by OID for the request loop and normalises each
value to the exact Go type gosnmp's marshaller expects.

### 8.3. Table Handling

SNMP tables (such as `ifTable` and `dot1dTpFdbTable`) are exposed as OID subtrees with instance identifiers appended. For example, `ifDescr.1` is the description of interface index 1 (`radio0`), `ifDescr.2` is `eth0`, and so on .

`internal/snmp` answers `GetNextRequest` by binary-searching the numerically sorted binding list
for the first OID strictly after the requested one, and `GetBulkRequest` by repeating that walk
(`non-repeaters` first, then up to `max-repetitions` per repeater, capped at 100). Reaching the
end yields `EndOfMibView`, and an exact-miss on `Get` yields `NoSuchObject`, so standard
`snmpwalk` and `snmpbulkwalk` work against the simulator. The MAC table root
(`1.3.6.1.2.1.17.4.3`) is registered in Phase 4.11.

---

## 9. CLI and Tooling

### 9.1. Starting the SNMP Agent

```bash
go run ./cmd/simulator start --config configs/default.yaml
```

The agent listens on UDP port 1161 by default; the port comes from the YAML configuration:

```yaml
snmp:
  port: 2161
```

### 9.2. Testing with snmpwalk

```bash
# Walk the system group (sysDescr, sysObjectID, sysUpTime, sysName, ...)
snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.1

# Walk the interface table: radio0, eth0, eth1
snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.2.2.1.2

# Walk the vendor radio objects (RSSI, fade margin, capacity, ...)
snmpwalk -v2c -c public localhost:1161 1.3.6.1.4.1.99999.1

# Get a single vendor OID (float displayed as an opaque double)
snmpget -v2c -c public localhost:1161 1.3.6.1.4.1.99999.1.1.1.0

# Read-write: set TX power, then read it back
snmpset -v2c -c public localhost:1161 1.3.6.1.4.1.99999.1.1.4.0 D 25.5
snmpget -v2c -c public localhost:1161 1.3.6.1.4.1.99999.1.1.4.0
```

A request with any community other than `public`, and any SNMPv3 message, is dropped without a
reply. The MAC forwarding database (`1.3.6.1.2.1.17.4.3.1.2`) is registered in Phase 4.11.

### 9.3. Receiving Traps

Receiving traps needs a receiver on the configured destination — the default is `127.0.0.1:1162`:

```bash
snmptrapd -f -Lo -p 1162
```

The `-f` flag keeps the daemon in the foreground, `-Lo` logs to stdout, and `-p 1162` binds to the TriadSim trap port. `docs/demo.md` walks through the full scenario.

### 9.4. Triggering Traps

```bash
# Inject a radio link failure
go run ./cmd/simulator alarm inject --type radioLinkDown --link radio0

# Or via RESTCONF simulation endpoint
curl -X POST http://localhost:8080/api/simulate/radio-failure \
  -d '{"link":"radio0"}'
```

---

## 10. References

| Document | Title |
|---|---|
| **RFC 1157** | A Simple Network Management Protocol (SNMP)  |
| **RFC 1901** | Introduction to Community-based SNMPv2 |
| **RFC 2578** | Structure of Management Information Version 2 (SMIv2)  |
| **RFC 2579** | Textual Conventions for SMIv2  |
| **RFC 2580** | Conformance Statements for SMIv2 |
| **RFC 2863** | The Interfaces Group MIB |
| **RFC 3411** | An Architecture for Describing SNMP Management Frameworks |
| **RFC 3416** | Version 2 of the Protocol Operations for SNMP (STD 62)  |
| **RFC 3418** | Management Information Base (MIB) for SNMP (STD 62)  |
| **RFC 4188** | Definitions of Managed Objects for Bridges (BRIDGE-MIB) |
| **RFC 3584** | Coexistence between SNMPv1, SNMPv2, and SNMPv3 |
