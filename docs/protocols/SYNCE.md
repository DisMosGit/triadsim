# SYNCE.md

**Project:** TriadSim — Lightweight Telecom Equipment Simulator  
**Technology:** Synchronous Ethernet (SyncE)  
**Related Standards:** ITU-T G.8261, G.8262, G.8264, G.781  
**Associated Channel:** Ethernet Synchronization Messaging Channel (ESMC)  
**Associated Messages:** Synchronization Status Messages (SSM)  
**Quality Level Encoding:** 4-bit SSM code (Option I), extended SSM (eSSM)  
**Default Synchronization Mode:** QL-enabled  

---

## 1. Introduction

Synchronous Ethernet (SyncE) is a physical-layer frequency synchronization technology standardized by the ITU-T. Unlike Precision Time Protocol (PTP), which distributes phase and time-of-day information at the packet layer, SyncE distributes frequency by locking the Ethernet PHY transmit clock of each network element to a reference clock derived from an upstream synchronization source. The technology is specified in ITU-T G.8261 (timing and synchronization aspects in packet networks) and ITU-T G.8262 (timing characteristics of synchronous Ethernet equipment slave clocks) [3†L4-L8].

SyncE is a foundational element of telecom synchronization architectures. It provides a stable frequency reference that PTP can use to improve phase and time accuracy. The two technologies are complementary: SyncE provides frequency, while PTP provides phase and time. ITU-T G.8275.1, the PTP telecom profile for full timing support, assumes that SyncE is available to provide the underlying frequency layer [8†L36-L38].

TriadSim models SyncE at the management plane. It does not implement the Ethernet PHY clock recovery mechanism or generate ESMC frames on the wire. Instead, it maintains a Synchronization Status Message (SSM) quality-level state per SyncE-capable interface, propagates QL changes through the EventBus, and exposes QL information through SNMP, NETCONF, and RESTCONF. This approach allows end-to-end testing of management systems that monitor and control synchronization without requiring a full SyncE stack.

---

## 2. Protocol Overview

### 2.1. Synchronization Model

SyncE operates on a hierarchical synchronization model:

| Role | Entity | Description |
|---|---|---|
| **Primary Reference Clock (PRC)** | ITU-T G.811 | Ultimate source of frequency traceability |
| **Synchronization Supply Unit (SSU)** | ITU-T G.812 | Intermediate clock that filters and distributes timing |
| **Synchronous Equipment Clock (SEC / EEC)** | ITU-T G.813 / G.8262 | Clock embedded in a network element |
| **SyncE-capable interface** | TriadSim model | Ethernet port that can be a source or sink of frequency |

The network element selects the best available synchronization source based on a combination of QL information and user-assigned priority, as defined in ITU-T G.781 [7†L42-L45].

### 2.2. ESMC

The Ethernet Synchronization Messaging Channel (ESMC) is the transport mechanism for SSM information over Ethernet. It is defined in ITU-T G.8264. ESMC uses the IEEE 802.3 Organization-Specific Slow Protocol (OSSP) to carry Synchronization Status Messages between adjacent Ethernet nodes [7†L10-L13].

ESMC defines two types of Protocol Data Units (PDUs):

| PDU Type | Purpose | Trigger |
|---|---|---|
| **Event ESMC PDU** | Immediate notification of a QL change | Change of SSM code or SD_CL_QL value |
| **Information ESMC PDU** | Periodic refresh of current QL | Configured heartbeat interval |

An ESMC PDU contains a Quality Level Type-Length-Value (QL TLV) field. The QL TLV carries a 4-bit SSM code that identifies the quality level of the transmitting clock source [9†L4-L6]. The ESMC PDU is always sent with the QL TLV as the first TLV in the Data and Padding field [9†L22-L24].

### 2.3. SSM and QL

The Synchronization Status Message (SSM) is the mechanism by which clock quality is communicated. The SSM carries a Quality Level (QL) value that indicates the traceability and accuracy of the timing source. The QL values are defined in ITU-T G.781, and their encoding for transmission over SyncE is defined in ITU-T G.8264 [5†L4-L15].

The SSM channel is a 4-bit field in the ESMC QL TLV. For extended quality levels (eSSM), an optional extended TLV is defined in G.8264 Amendment 2, allowing discrimination of very accurate clocks such as ePRTC and eEEC [0†L19-L22].

---

## 3. Quality Levels

### 3.1. Option I Synchronization Network

ITU-T G.781 defines quality levels for different synchronization network options. Option I is the most commonly deployed for SDH-based networks and is the primary reference for TriadSim. Five SSM codes are defined for Option I [5†L31-L32]:

| QL Code | Name | SSM Binary | Description | Priority |
|---|---|---|---|---|---|
| 0010 | QL-PRC | 0010 | Primary Reference Clock (ITU-T G.811) | Highest |
| 0100 | QL-SSU-A | 0100 | Synchronization Supply Unit, Type I or V (ITU-T G.812) | High |
| 1000 | QL-SSU-B | 1000 | Synchronization Supply Unit, Type VI (ITU-T G.812) | Medium |
| 1011 | QL-SEC | 1011 | Synchronous Equipment Clock (ITU-T G.813 or G.8262, Option I) | Low |
| 1111 | QL-DNU | 1111 | Do Not Use | Lowest |

**Note:** In ITU-T G.8264, QL-SEC corresponds to QL-EEC1 for Option I networks [6†L4-L6].

### 3.2. Extended Quality Levels (eSSM)

ITU-T G.8264 Amendment 2 introduced the extended SSM (eSSM) mechanism to support very accurate clocks that cannot be adequately represented by the 4-bit SSM code. The eSSM uses an optional extended TLV in the ESMC PDU. The following extended QL values are defined [0†L19-L22]:

| eSSM Code | Name | Description |
|---|---|---|
| 0x20 | QL-PRTC | Primary Reference Time Clock (ITU-T G.8272) |
| 0x21 | QL-ePRTC | Enhanced Primary Reference Time Clock (ITU-T G.8272.1) |
| 0x22 | QL-eEEC | Enhanced Ethernet Equipment Clock (ITU-T G.8262.1) |

When processing SSM information, the SSM code should be processed first, followed by the eSSM code if present [4†L10-L12].

### 3.3. Option II and Option III Networks

ITU-T G.781 also defines quality levels for Option II (SONET) and Option III networks. These include QL-PRS, QL-STU, QL-ST2, QL-TNC, QL-ST3E, QL-eSEC, QL-ST3, QL-SMC, QL-ST4, and QL-PROV [6†L12-L28]. TriadSim models Option I quality levels for SyncE and uses the Option I QL set for its SNMP and RESTCONF objects.

---

## 4. Relationship with PTP

### 4.1. Complementary Roles

SyncE and PTP serve distinct but complementary synchronization functions:

| Aspect | SyncE | PTP |
|---|---|---|
| **Synchronization type** | Frequency | Phase and time-of-day |
| **OSI layer** | Physical layer (Layer 1) | Packet layer (Layer 2/3/4) |
| **Transport** | Ethernet PHY clock | UDP/IP multicast or unicast |
| **Accuracy** | Frequency accuracy per ITU-T G.8262 | Sub-microsecond phase/time |
| **Standard** | ITU-T G.8261, G.8262, G.8264 | IEEE 1588-2019 |
| **Telecom profile** | G.8261 (SyncE) | G.8275.1 (FTS), G.8275.2 (PTS) |

SyncE provides the frequency layer that PTP uses to improve its phase accuracy. In a G.8275.1 deployment, every network element is a Boundary Clock or Transparent Clock and is also SyncE-capable. The SyncE frequency reference reduces the phase error that PTP must correct, enabling sub-microsecond time accuracy [2†L11-L15].

### 4.2. QL Propagation

In a combined SyncE and PTP deployment, QL information can flow through multiple channels:

| Source | QL Propagation Path |
|---|---|
| SyncE interface | ESMC → SSM → QL state |
| GPS/GNSS | Fixed QL maintained by platform software |
| PTP clock | PTP communicates its QL to the frequency synchronization PI software through platform APIs |

The frequency synchronization selection process (PI) uses QL information from all sources to select the best available frequency reference [7†L32-L35].

### 4.3. SyncE Preference for PTP Receiver Interface

When SyncE and PTP sources have equal QL and user priority, the system should prefer the interface on which the PTP receiver is selected. This ensures that the frequency and phase/time references are traceable to the same source. If the PTP source fails or its quality degrades, SyncE switches to the new PTP source [11†L22-L26].

TriadSim models this behavior by allowing the user to configure a preference flag on SyncE interfaces that are co-located with a PTP receiver.

---

## 5. Simulator Implementation

### 5.1. Go Package Structure

```
internal/sync/
├── state.go          # Clock state definitions and transitions
├── state_machine.go  # StateMachine implementation
├── holdover.go       # Holdover timer and drift simulation
├── bmca.go           # BMCA attribute modeling
├── synce.go          # SyncE/ESMC QL state
└── ptp.go            # PTP model integration
```

### 5.2. SyncE Model

```go
type SyncEInterface struct {
    Name         string `path:"name" json:"name"`
    Port         int    `path:"port" json:"port"`
    QL           int    `path:"ql" json:"ql"`           // SSM code (e.g., 0x2 = PRC)
    ExtendedQL   int    `path:"extended-ql" json:"extended-ql"` // eSSM code
    SSMEnabled   bool   `path:"ssm-enabled" json:"ssm-enabled"`
    Priority     uint8  `path:"priority" json:"priority"`
    PTPPreference bool  `path:"ptp-preference" json:"ptp-preference"`
}
```

### 5.3. QL State Machine

TriadSim maintains a QL state per SyncE-capable interface. The QL state changes in response to:

- **Manual injection** via RESTCONF (`POST /api/simulate/synce-ql`)
- **PTP state changes** (e.g., PTP transitioning to holdover may trigger a QL degradation)
- **Configuration changes** via NETCONF or RESTCONF
- **EventBus events** from other simulation modules

### 5.4. Event Types

| Event Type | Payload | Consumers |
|---|---|---|
| `SyncEQLChanged` | `{port, oldQL, newQL, extendedQL}` | SNMP trap sender, NETCONF notification, RESTCONF subscribers, Prometheus |
| `SyncEInterfaceDown` | `{port}` | SNMP trap sender |
| `SyncEInterfaceUp` | `{port}` | SNMP trap sender |
| `SyncEPTPPreferenceChanged` | `{port, preferred}` | Prometheus, CLI logger |

### 5.5. Integration with PTP State Machine

The SyncE QL state is linked to the PTP state machine:

- When PTP transitions to **Holdover-In-Specification**, the SyncE QL is maintained at the last known value.
- When PTP transitions to **Holdover-Out-Of-Specification**, the SyncE QL is degraded to QL-SEC (or QL-DNU if the holdover timer exceeds the configured limit).
- When PTP restores to **Locked**, the SyncE QL is restored to the value corresponding to the PTP source quality.

This linkage is configurable and can be disabled for independent testing.

---

## 6. Management Interfaces

### 6.1. SNMP

TriadSim exposes SyncE QL information through vendor-specific OIDs in the enterprise subtree.

| OID | Object | Type | Access | Description |
|---|---|---|---|---|
| `1.3.6.1.4.1.99999.2.1.5` | `simSyncSyncEQL` | Integer | read-only | Current SyncE QL for the primary interface |
| `1.3.6.1.4.1.99999.2.1.6` | `simSyncSyncEExtendedQL` | Integer | read-only | Current extended QL (eSSM) |
| `1.3.6.1.4.1.99999.2.1.7` | `simSyncSyncEInterfaceTable` | Table | read-only | Per-interface SyncE state |
| `1.3.6.1.4.1.99999.2.1.7.1.1` | `simSyncSyncEIfIndex` | Integer | read-only | Interface index |
| `1.3.6.1.4.1.99999.2.1.7.1.2` | `simSyncSyncEIfQL` | Integer | read-only | QL for the interface |
| `1.3.6.1.4.1.99999.2.1.7.1.3` | `simSyncSyncEIfSSMEnabled` | TruthValue | read-only | SSM enabled flag |

### 6.2. NETCONF

SyncE configuration is available under the `sim-sync` YANG module.

```xml
<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target><candidate/></target>
    <config>
      <synce xmlns="urn:sim:sync">
        <interface>
          <name>eth0</name>
          <ssm-enabled>true</ssm-enabled>
          <ql>2</ql>
          <priority>10</priority>
          <ptp-preference>true</ptp-preference>
        </interface>
      </synce>
    </config>
  </edit-config>
</rpc>
```

### 6.3. RESTCONF

```bash
# Get SyncE state
curl http://localhost:8080/restconf/data/sim-sync:synce/state
# {"sim-sync:state":{"ql":2,"extended-ql":0,"ssm-enabled":true}}

# Configure SyncE interface
curl -X PATCH http://localhost:8080/restconf/data/sim-sync:synce/interface=eth0 \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-sync:ssm-enabled":true,"sim-sync:ql":2,"sim-sync:priority":10}'

# Inject SyncE QL change
curl -X POST http://localhost:8080/api/simulate/synce-ql \
  -H 'Content-Type: application/json' \
  -d '{"port":"eth0","ql":11}'
```

### 6.4. CLI

```bash
# Show SyncE state
go run ./cmd/simulator dump --synce

# Show per-interface SyncE details
go run ./cmd/simulator dump --synce --interface eth0

# Inject SyncE QL change
go run ./cmd/simulator synce ql set --port eth0 --ql 4

# Force SyncE QL degradation
go run ./cmd/simulator synce ql degrade --port eth0
```

---

## 7. Metrics

TriadSim exposes the following Prometheus metrics for SyncE:

| Metric | Type | Description |
|---|---|---|
| `simulator_synce_ql` | Gauge | Current SyncE QL (integer SSM code) |
| `simulator_synce_extended_ql` | Gauge | Current extended QL (eSSM code) |
| `simulator_synce_ql_changes_total` | Counter | Total number of QL changes |
| `simulator_synce_interface_up` | Gauge | Per-interface SyncE operational state (1=up, 0=down) |
| `simulator_synce_ptp_preference` | Gauge | Whether the interface is preferred for PTP receiver (1=yes, 0=no) |

---

## 8. Simplifications and Scope

TriadSim intentionally simplifies SyncE to focus on management plane testing:

| Aspect | TriadSim Behavior |
|---|---|
| **Ethernet PHY clock recovery** | Not implemented; frequency state is maintained at the management level |
| **ESMC frame generation** | Not generated; QL changes are triggered via API or EventBus |
| **SSM TLV encoding/decoding** | Not performed on the wire; SSM codes are stored as integers |
| **Slow protocol (OSSP) framing** | Not implemented |
| **QL-enabled selection algorithm** | Simplified; QL and priority are configurable, selection is deterministic |
| **Extended SSM TLV** | Modeled as a separate integer field; not encoded in ESMC |
| **SyncE-to-PTP QL propagation** | Modeled via EventBus linkage; not via real ESMC/PTP message exchange |
| **Multi-domain synchronization** | Single domain per interface; no cross-domain QL leakage modeling |

These simplifications allow the simulator to be lightweight and focused on integration testing with external management systems that monitor and control synchronization state.

---

## 9. References

| Document | Title |
|---|---|
| **ITU-T G.781** | Synchronization layer functions for frequency synchronization based on the physical layer |
| **ITU-T G.811** | Timing characteristics of primary reference clocks |
| **ITU-T G.812** | Timing requirements of slave clocks suitable for use as node clocks in synchronization networks |
| **ITU-T G.813** | Timing characteristics of SDH equipment slave clocks (SEC) |
| **ITU-T G.8261** | Timing and synchronization aspects in packet networks |
| **ITU-T G.8262** | Timing characteristics of synchronous Ethernet equipment slave clock (EEC) |
| **ITU-T G.8264** | Distribution of timing information through packet networks |
| **ITU-T G.8272** | Timing characteristics of primary reference time clocks |
| **ITU-T G.8272.1** | Timing characteristics of enhanced primary reference time clocks |
| **ITU-T G.8275.1** | Precision time protocol telecom profile for phase/time synchronization with full timing support |
| **IEEE 1588-2019** | IEEE Standard for a Precision Clock Synchronization Protocol |
| **RFC 8173** | Precision Time Protocol Version 2 (PTPv2) Management Information Base |

---

## Appendix A: SSM Code Quick Reference (Option I)

| Binary | Hex | QL Name | Priority | Extended SSM |
|---|---|---|---|---|
| 0010 | 0x2 | QL-PRC | Highest | — |
| 0100 | 0x4 | QL-SSU-A | High | — |
| 1000 | 0x8 | QL-SSU-B | Medium | — |
| 1011 | 0xB | QL-SEC | Low | — |
| 1111 | 0xF | QL-DNU | Lowest | — |
| — | 0x20 | — | — | QL-PRTC |
| — | 0x21 | — | — | QL-ePRTC |
| — | 0x22 | — | — | QL-eEEC |

---

## Appendix B: ESMC PDU Format (Conceptual)

```
+----------------+----------------+----------------+----------------+
| Subtype (2B)   | ITU-T Subtype  | QL TLV Length  | Reserved       |
| 00-01          | (e.g., 00-01)  |                |                |
+----------------+----------------+----------------+----------------+
| QL TLV Value                                                  |
| (bit 3-0: 4-bit SSM code)                                     |
+----------------+----------------+----------------+----------------+
| Optional Extended TLV (eSSM)                                  |
| (Length + eSSM code)                                          |
+----------------+----------------+----------------+----------------+
```

The QL TLV is always the first TLV in the ESMC PDU. The extended TLV is optional and is used only when eSSM codes are supported by both peers [9†L22-L25].
