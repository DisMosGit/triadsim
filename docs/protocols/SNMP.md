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

| OID | Object | Type | Access | Description |
|---|---|---|---|---|
| `1.3.6.1.4.1.99999.1.1.1` | `simRadioRssi` | Float | read-only | Current RSSI in dBm |
| `1.3.6.1.4.1.99999.1.1.2` | `simRadioFadeMargin` | Float | read-only | Fade margin in dB |
| `1.3.6.1.4.1.99999.1.1.3` | `simRadioCapacity` | Unsigned32 | read-only | Current capacity in Mbps |
| `1.3.6.1.4.1.99999.1.1.4` | `simRadioTxPower` | Float | read-write | Configured TX power in dBm |
| `1.3.6.1.4.1.99999.2.1.1` | `simSyncPtpState` | Integer | read-only | PTP state (1=freerun, 2=master, 3=holdover) |
| `1.3.6.1.4.1.99999.2.1.2` | `simSyncPtpOffset` | Float | read-only | PTP offset in nanoseconds |
| `1.3.6.1.4.1.99999.3.1.1` | `simL2VlanCount` | Unsigned32 | read-only | Number of configured VLANs |

---

## 4. MIB Modules

TriadSim does not parse MIB files at runtime. Instead, it uses Go structs with OID metadata. However, the following standard MIB modules are referenced for OID correctness and client interoperability:

| MIB Module | RFC | Purpose |
|---|---|---|
| **SNMPv2-SMI** | RFC 2578 | Structure of Management Information, OID definitions |
| **SNMPv2-TC** | RFC 2579 | Textual conventions (`DisplayString`, `TimeStamp`, `MacAddress`) |
| **SNMPv2-MIB** | RFC 3418 | `sysUpTime`, `sysDescr`, `snmpTrapOID` |
| **IF-MIB** | RFC 2863 | `ifDescr`, `ifType`, `ifOperStatus`, interface counters |
| **BRIDGE-MIB** | RFC 4188 | `dot1dTpFdbTable` (MAC forwarding database) |

### 4.1. Textual Conventions

The following textual conventions from RFC 2579 are used by TriadSim MIB objects :

- **`DisplayString`** — OCTET STRING (SIZE (0..255)), NVT ASCII. Used for `ifDescr`, `sysDescr`, `sysContact`.
- **`PhysAddress`** — OCTET STRING. Used for MAC addresses in `dot1dTpFdbAddress`.
- **`TimeStamp`** — TimeTicks. Used for `sysUpTime` and trap timestamps.

### 4.2. Standard OID Reference

| Object | OID | Type | Access |
|---|---|---|---|
| `sysDescr` | `1.3.6.1.2.1.1.1` | DisplayString | read-only |
| `sysObjectID` | `1.3.6.1.2.1.1.2` | OBJECT IDENTIFIER | read-only |
| `sysUpTime` | `1.3.6.1.2.1.1.3` | TimeTicks | read-only |
| `sysContact` | `1.3.6.1.2.1.1.4` | DisplayString | read-write |
| `sysName` | `1.3.6.1.2.1.1.5` | DisplayString | read-write |
| `sysLocation` | `1.3.6.1.2.1.1.6` | DisplayString | read-write |
| `ifDescr` | `1.3.6.1.2.1.2.2.1.2` | DisplayString | read-only |
| `ifType` | `1.3.6.1.2.1.2.2.1.3` | INTEGER | read-only |
| `ifOperStatus` | `1.3.6.1.2.1.2.2.1.8` | INTEGER | read-only |
| `ifInOctets` | `1.3.6.1.2.1.2.2.1.10` | Counter32 | read-only |
| `ifOutOctets` | `1.3.6.1.2.1.2.2.1.16` | Counter32 | read-only |
| `snmpTrapOID` | `1.3.6.1.2.1.11.4.1` | OBJECT IDENTIFIER | — (trap varbind) |

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

---

## 6. Traps

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
| `1.3.6.1.4.1.99999.0.1` | `simRadioLinkDown` | Major | Radio link failure or fade margin below threshold |
| `1.3.6.1.4.1.99999.0.2` | `simRadioLinkUp` | Info | Radio link restored |
| `1.3.6.1.4.1.99999.0.3` | `simSyncHoldover` | Major | PTP transitioned to holdover state |
| `1.3.6.1.4.1.99999.0.4` | `simSyncRestored` | Info | PTP source restored, transitioned to master |
| `1.3.6.1.4.1.99999.0.5` | `simL2StormDetected` | Warning | Broadcast storm threshold exceeded |
| `1.3.6.1.4.1.99999.0.6` | `simConfigChanged` | Info | Running configuration modified |

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
    OID: 1.3.6.1.2.1.11.4.1.0      (snmpTrapOID.0)
    Value: 1.3.6.1.4.1.99999.0.1   (simRadioLinkDown)

  varbind[3]:
    OID: 1.3.6.1.4.1.99999.1.1.1.0 (simRadioRssi.0)
    Value: -82.5

  varbind[4]:
    OID: 1.3.6.1.2.1.2.2.1.2.1     (ifDescr.1)
    Value: "radio0"
```

### 6.4. Trap Delivery

Traps are sent to a single destination taken from the YAML config file: the agent port and the
trap destination port. The MVP has no receiver list and no per-receiver community.

```yaml
snmp:
  port: 1161        # agent
  trap-port: 1162   # trap destination port
```

Traps go to `127.0.0.1:<snmp.trap-port>`. Trap delivery is fire-and-forget. If the destination is unreachable, the trap is silently dropped; no retransmission is attempted. This is consistent with the unconfirmed nature of SNMPv2-Trap-PDUs .

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

Each model that exposes SNMP objects registers its OID tree at startup:

```go
type SNMPRegistration struct {
    OID         string
    ModelPath   string
    Type        ASN1Type
    Access      Access  // read-only, read-write
    Description string
}
```

Example registration for radio link objects:

```go
var radioRegistrations = []SNMPRegistration{
    {
        OID:         "1.3.6.1.4.1.99999.1.1.1",
        ModelPath:   "radio-link/rssi",
        Type:        ASN1Float,
        Access:      ReadOnly,
        Description: "Current RSSI in dBm",
    },
    {
        OID:         "1.3.6.1.4.1.99999.1.1.4",
        ModelPath:   "radio-link/tx-power",
        Type:        ASN1Float,
        Access:      ReadWrite,
        Description: "Configured TX power in dBm",
    },
}
```

### 8.3. Table Handling

SNMP tables (such as `ifTable` and `dot1dTpFdbTable`) are exposed as OID subtrees with instance identifiers appended. For example, `ifDescr.1` is the description of interface index 1 (`radio0`), `ifDescr.2` is `eth0`, and so on .

The `internal/snmp` package implements `GetNextRequest` and `GetBulkRequest` by walking the registered OID tree in lexicographic order. This enables standard `snmpwalk` and `snmpbulkwalk` operations against the simulator.

---

## 9. CLI and Tooling

### 9.1. Starting the SNMP Agent

```bash
go run ./cmd/simulator start --config configs/default.yaml
```

The agent listens on UDP port 1161 by default. The port can be overridden:

```bash
go run ./cmd/simulator start --snmp-port 2161
```

### 9.2. Testing with snmpwalk

```bash
# Walk the system group
snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.1

# Walk the interface table
snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.2.2.1.2

# Walk the MAC forwarding database
snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.17.4.3.1.2

# Get a single vendor OID
snmpget -v2c -c public localhost:1161 1.3.6.1.4.1.99999.1.1.1.0
```

### 9.3. Receiving Traps

```bash
snmptrapd -f -Lo -p 1162
```

The `-f` flag keeps the daemon in the foreground, `-Lo` logs to stdout, and `-p 1162` binds to the TriadSim trap port.

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
