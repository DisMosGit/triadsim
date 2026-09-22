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
├── doc.go      # package overview and scope
├── manager.go  # Manager: Run/Tick, source selection refresh, offset/jitter
├── ptp.go      # PTP state machine (see PTP.md)
├── synce.go    # SyncE source selection
└── esmc.go     # QL/SSM mapping and the simplified ESMC message
```

The SyncE objects live in `internal/model/sync.go` as `SyncEState`,
`SyncEInterface` and `ESMC` with `path`/`xml`/`json` tags; `internal/router`
maps them to RESTCONF/NETCONF paths and to the vendor OIDs, and the domain is
wired into `start` next to the L2 domain.

### 5.2. SyncE Model

```go
type SyncEState struct {
    Enabled            bool             `path:"enabled"`
    SelectedSource     string           `path:"selected-source" config:"false"`
    SelectedQL         QL               `path:"selected-ql" config:"false"`
    SelectedExtendedQL string           `path:"selected-extended-ql" config:"false"`
    Interfaces         []SyncEInterface `path:"interfaces/interface"`
    ESMC               ESMC             `path:"esmc"`
}

type SyncEInterface struct {
    Name          string `path:"name" key:"true"`
    SSMEnabled    bool   `path:"ssm-enabled"`
    QL            QL     `path:"ql"`          // "QL-PRC", "QL-SSU-A", ...
    ExtendedQL    string `path:"extended-ql"` // "QL-PRTC", "QL-ePRTC", "QL-eEEC", or empty
    Priority      uint8  `path:"priority"`
    PTPPreference bool   `path:"ptp-preference"`
}

type ESMC struct {
    Enabled       bool   `path:"enabled"`
    TxInterval    uint32 `path:"tx-interval"`
    ExtendedCodes bool   `path:"extended-codes"`
}
```

The interface list is closed (seeded with `eth0` and `eth1`), like the STP port
list: SyncE is a feature of the device's Ethernet ports. The three
`selected-*` leaves are read-only state.

### 5.3. QL State Machine

TriadSim maintains a QL state per SyncE-capable interface and re-runs the source
selection on every tick of the injected clock (5 s by default). A source
changes in response to:

- **Configuration** via NETCONF or RESTCONF (`synce/interfaces/interface[name=…]/ql`,
  `…/priority`, `…/ssm-enabled`, `synce/enabled`);
- **Source loss** through `sync.Manager.SyncLoss`, which the RESTCONF
  `/api/simulate/sync-loss` endpoint calls;
- **PTP state changes** once the cross-domain wiring of Phase 6.5 subscribes to
  the radio alarms.

The selection order is: the best quality level (QL-PRC, QL-SSU-A, QL-SSU-B,
QL-SEC), then the lowest configured `priority`, then the interface the PTP
receiver prefers (`ptp-preference`), and finally the interface name, so the
choice is deterministic. `QL-DNU` is never selected, an interface with
`ssm-enabled` false is skipped, and when no candidate exists the selection is
empty. The result is written to `synce/selected-source`,
`synce/selected-ql` and `synce/selected-extended-ql`.

### 5.4. Event Types

SyncE does not define its own bus types: the domain publishes the generic
`StateTransition`, `AlarmRaised` and `AlarmCleared` events documented in
`docs/eventbus.md`. A QL change is visible as a change of the selection state
leaves; trap and metric producers subscribe to the bus events.

### 5.5. Integration with PTP State Machine

Both parts of the domain are managed by one `sync.Manager`, so the SyncE
selection and the PTP clock are read and written consistently:

- PTP `locked` keeps the QL selection as configured.
- When PTP enters `holdover-in-spec`, the SyncE selection is left untouched (the
  last known quality is maintained).
- When the holdover expires (`holdover-out-of-spec`), the domain raises the PTP
  holdover alarm; a SyncE QL degradation on top of it is planned for the
  cross-domain work of Phase 6.
- A restored source locks the clock again and clears the alarm.

### 5.6. ESMC/SSM Mapping

`internal/sync/esmc.go` maps the model quality levels to their codes and vice
versa (`SSMCode`, `QLFromSSM`, `ExtendedSSMCode`, `Rank`) and models one
simplified `Message{QL, ExtendedQL}` with a `Validate()`. The simulator never
encodes or decodes an ESMC PDU on the wire: the codes are what the vendor SNMP
objects (`simSyncSyncEQL`, `simSyncSyncEExtendedQL`) report.

---

## 6. Management Interfaces

### 6.1. SNMP

TriadSim exposes SyncE QL information through vendor-specific OIDs in the enterprise subtree.

| OID | Object | Type | Access | Description |
|---|---|---|---|---|
| `1.3.6.1.4.1.99999.2.1.5.0` | `simSyncSyncEQL` | Integer | read-only | QL of the selected source, as an SSM code |
| `1.3.6.1.4.1.99999.2.1.6.0` | `simSyncSyncEExtendedQL` | Integer | read-only | Extended QL (eSSM code) of the selected source, 0 when unset |
| `1.3.6.1.4.1.99999.2.1.7.1.1.<ifIndex>` | `simSyncSyncEIfQL` | Integer | read-only | QL for the interface, as an SSM code |
| `1.3.6.1.4.1.99999.2.1.7.1.2.<ifIndex>` | `simSyncSyncEIfSSMEnabled` | Integer | read-only | SSM enabled flag as TruthValue |

The interface table has no separate index column: the instance sub-identifier is
the interface index (radio0=1, eth0=2, eth1=3), the same instance the interface
and bridge MIB tables use. The SSM codes are documented in Appendix A; the
extended codes are `QL-PRTC`=0x20, `QL-ePRTC`=0x21 and `QL-eEEC`=0x22.

### 6.2. NETCONF

SyncE configuration is available under the `sim-sync` YANG module. The
`selected-*` leaves are read-only (`config:"false"`) and are not returned by
`get-config`.

```xml
<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target><candidate/></target>
    <config>
      <synce xmlns="urn:sim:sync">
        <enabled>true</enabled>
        <interfaces>
          <interface>
            <name>eth0</name>
            <ssm-enabled>true</ssm-enabled>
            <ql>QL-PRC</ql>
            <priority>10</priority>
            <ptp-preference>true</ptp-preference>
          </interface>
        </interfaces>
        <esmc>
          <enabled>true</enabled>
          <tx-interval>1</tx-interval>
        </esmc>
      </synce>
    </config>
  </edit-config>
</rpc>
```

### 6.3. RESTCONF

```bash
# Get the selected source and its quality level.
curl http://localhost:8080/restconf/data/sim-sync:synce/selected-source
# {"sim-sync:selected-source":"eth0"}
curl http://localhost:8080/restconf/data/sim-sync:synce/selected-ql
# {"sim-sync:selected-ql":"QL-PRC"}

# Get the whole SyncE subtree, including the read-only selection.
curl 'http://localhost:8080/restconf/data/sim-sync:synce?content=all'

# Configure a SyncE interface.
curl -X PATCH http://localhost:8080/restconf/data/sim-sync:synce/interfaces/interface=eth0 \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-sync:interface":{"ssm-enabled":true,"ql":"QL-PRC","priority":10}}'

# Lose the synchronization source: PTP enters holdover.
curl -X POST http://localhost:8080/api/simulate/sync-loss \
  -H 'Content-Type: application/json' -d '{"port":"eth0"}'
```

An invalid quality level answers `422 invalid-value`; the selection itself is
read-only and answers `403 access-denied`.

### 6.4. CLI

The cobra commands (`dump --synce`, `synce ql set`) arrive in Phase 6.6. Until
then SyncE is driven through RESTCONF or NETCONF.

---

## 7. Metrics

The Prometheus metrics below are **planned for Phase 6.4** and are not exposed
yet. In Phase 5 a QL change is visible through the `synce/selected-*` state
leaves and, indirectly, through the PTP state transitions on the EventBus.

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
| **ESMC frame generation** | Not generated; QL changes come from configuration or the simulation endpoint |
| **SSM TLV encoding/decoding** | Not performed on the wire; only the SSM/eSSM code mapping is modeled |
| **Slow protocol (OSSP) framing** | Not implemented |
| **QL-enabled selection algorithm** | Simplified; quality level, priority and PTP preference are configurable and the selection is deterministic |
| **Extended SSM TLV** | Modeled as a separate string leaf; not encoded in ESMC |
| **SyncE-to-PTP QL propagation** | Both parts share one `sync.Manager`; the cross-domain linkage to radio alarms arrives in Phase 6 |
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
