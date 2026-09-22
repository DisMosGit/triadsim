# PTP.md

**Project:** TriadSim — Lightweight Telecom Equipment Simulator  
**Protocol:** Precision Time Protocol, IEEE 1588-2019 (PTP v2.1)  
**Related:** SyncE (ITU-T G.8261/G.8264), ESMC/SSM (ITU-T G.781)  
**Default PTP Domain:** 24 (G.8275.1 telecom profile)  
**Transport:** UDP over IPv4/IPv6 (multicast/unicast)  
**Default PTP Port:** 319/320 (event/general messages)

---

## 1. Introduction

The Precision Time Protocol (PTP), defined in IEEE 1588, is a protocol used to synchronize clocks throughout a packet-switched network. It achieves sub-microsecond accuracy, making it suitable for telecom, industrial, and financial applications. TriadSim implements PTP as part of its synchronization domain, alongside SyncE, to simulate the timing behavior of telecom equipment.

TriadSim does not implement the full IEEE 1588 protocol stack. Instead, it models the **state machine** of a PTP clock, including the transitions between freerun, master, slave, and holdover states. Real PTP message exchanges (Sync, Delay_Req, Announce) are simulated at the management plane level; the simulator exposes PTP state, offset, and quality metrics through SNMP, NETCONF, and RESTCONF. This approach allows end-to-end testing of management systems without requiring a full PTP stack.

The synchronization model is based on the ITU-T G.8275.1 telecom profile for phase/time synchronization with full timing support from the network.

---

## 2. Protocol Versions and Standards

### 2.1. IEEE 1588 Versions

| Standard | Protocol Version | Year | Notes |
|---|---|---|---|
| **IEEE 1588-2002** | PTP v1 | 2002 | Original version |
| **IEEE 1588-2008** | PTP v2 | 2008 | Widely deployed, basis for most telecom profiles |
| **IEEE 1588-2019** | PTP v2.1 | 2019 | Current version; backward compatible with v2.0 |

PTP v2.1 adds enhancements such as authentication TLVs, statistics reporting, and improvements to the Best Master Clock Algorithm (BMCA). TriadSim models PTP v2.1 behavior where applicable but does not implement the wire protocol.

### 2.2. ITU-T Telecom Profiles

| Recommendation | Title | Scope |
|---|---|---|
| **ITU-T G.8275.1** | PTP telecom profile for phase/time synchronization with full timing support (FTS) | All network elements support PTP (Boundary Clock or Transparent Clock) |
| **ITU-T G.8275.2** | PTP telecom profile for phase/time synchronization with partial timing support (PTS) | Not all network elements are PTP-aware |
| **ITU-T G.8265.1** | PTP telecom profile for frequency synchronization | Frequency-only distribution |

TriadSim targets G.8275.1 behavior. The default PTP domain is 24, as specified for FTS deployments.

### 2.3. Related Standards

| Standard | Title | Relation |
|---|---|---|
| **ITU-T G.8261** | Timing and synchronization aspects in packet networks | Defines SyncE and PTP coexistence |
| **ITU-T G.8264** | Distribution of timing information through packet networks | Defines ESMC (Ethernet Synchronization Messaging Channel) |
| **ITU-T G.781** | Synchronization layer functions | Defines SSM quality levels (QL) and clock selection |
| **RFC 8173** | PTPv2 Management Information Base (MIB) | SNMP MIB for PTP management |

---

## 3. Synchronization Model

### 3.1. Clock Types

| Clock Type | Abbreviation | Description |
|---|---|---|
| **Grandmaster** | GM / T-GM | Primary time source in a PTP domain |
| **Ordinary Clock** | OC | Single PTP port; either master or slave |
| **Boundary Clock** | BC / T-BC | Multiple PTP ports; can be slave on one and master on others |
| **Transparent Clock** | TC | Forwards PTP messages with correction for residence time |

TriadSim simulates a **T-BC (Telecom Boundary Clock)** with configurable behavior. The simulator can act as a T-GM (master) or a T-BC (slave/holdover) depending on configuration.

### 3.2. PTP Port States

Each PTP port in a clock is in one of the following states, as defined in IEEE 1588:

| Port State | Value | Description |
|---|---|---|
| **INITIALIZING** | 1 | Port is initializing |
| **FAULTY** | 3 | Port is in a fault state |
| **DISABLED** | 4 | Port is disabled |
| **LISTENING** | 5 | Port is listening for Announce messages |
| **MASTER** | 6 | Port is behaving as a master port |
| **PASSIVE** | 7 | Port does not place messages on the communication path (except Pdelay) |
| **UNCALIBRATED** | 8 | Port is synchronizing but not yet calibrated |
| **SLAVE** | 9 | Port is synchronizing to the selected master port |

### 3.3. Clock-Level States (ITU-T G.8275.1)

ITU-T G.8275.1 defines clock-level states for T-GM and T-BC that provide a high-level indication of the operational status of the entire clock.

| Clock State | Condition | Mapping to Port States |
|---|---|---|
| **Free-Run** | Clock has never been synchronized to a time source | No PTP ports in MASTER, PRE-MASTER, PASSIVE, UNCALIBRATED, or SLAVE states |
| **Acquiring** | Clock is in the process of synchronizing | A PTP port is in UNCALIBRATED state |
| **Locked** | Clock is synchronized and within acceptable accuracy | A PTP port is in SLAVE state |
| **Holdover-In-Specification** | No longer synchronized; using stored information to maintain performance within specification | No PTP ports in INITIALIZING, LISTENING, UNCALIBRATED, or SLAVE states; performance within specification |
| **Holdover-Out-Of-Specification** | No longer synchronized; unable to maintain performance within specification | No PTP ports in INITIALIZING, LISTENING, UNCALIBRATED, or SLAVE states; performance not within specification |

---

## 4. State Machine

TriadSim implements a simplified PTP state machine that models the transitions between clock-level states. The state machine is event-driven and publishes state transitions to the EventBus.

### 4.1. State Diagram

```
                    ┌─────────────────────────────────────────────┐
                    │                                             │
                    │                  Free-Run                   │
                    │  (no PTP ports in MASTER/SLAVE/PASSIVE)     │
                    │                                             │
                    └──────────────────┬──────────────────────────┘
                                       │
                                       │ source detected
                                       ▼
                    ┌─────────────────────────────────────────────┐
                    │                                             │
                    │                 Acquiring                   │
                    │      (port in UNCALIBRATED state)           │
                    │                                             │
                    └──────────────────┬──────────────────────────┘
                                       │
                                       │ calibration complete
                                       ▼
                    ┌─────────────────────────────────────────────┐
                    │                                             │
                    │                  Locked                     │
                    │       (port in SLAVE state)                 │
                    │                                             │
                    └──────────────────┬──────────────────────────┘
                                       │
                                       │ source lost
                                       ▼
                    ┌─────────────────────────────────────────────┐
                    │                                             │
                    │         Holdover-In-Specification           │
                    │   (no SLAVE port; performance within spec)  │
                    │                                             │
                    └──────────────────┬──────────────────────────┘
                                       │
                                       │ holdover timer expires
                                       ▼
                    ┌─────────────────────────────────────────────┐
                    │                                             │
                    │        Holdover-Out-Of-Specification        │
                    │  (no SLAVE port; performance out of spec)   │
                    │                                             │
                    └──────────────────┬──────────────────────────┘
                                       │
                                       │ source restored
                                       ▼
                    ┌─────────────────────────────────────────────┐
                    │                                             │
                    │                  Locked                     │
                    │                                             │
                    └─────────────────────────────────────────────┘
```

### 4.2. Transition Events

TriadSim implements the transitions below in `internal/sync/ptp.go`. An event
whose (from, event) pair is not in the table is a no-op: the clock keeps its
state and no event is published.

| Event | From | To | Trigger |
|---|---|---|---|
| `SourceDetected` | Free-Run | Acquiring | Valid PTP source discovered |
| `CalibrationComplete` | Acquiring | Locked | Offset within acceptable threshold |
| `SourceLost` | Locked | Holdover-In-Spec | Announce timeout; no SLAVE port |
| `SourceLost` | Acquiring | Free-Run | The source disappeared before the clock ever locked |
| `HoldoverTimerExpired` | Holdover-In-Spec | Holdover-Out-Of-Spec | `clock.Clock.AfterFunc` timer |
| `SourceRestored` | Holdover-In-Spec, Holdover-Out-Of-Spec | Locked | Valid PTP source re-acquired |
| `ManualReset` | any other state | Free-Run | `sync.Manager.Handle` with `EventReset` |

The state machine is fed through `sync.Manager.Handle(ctx, Event)`; the
`SourceLost` path that the RESTCONF simulation endpoint uses is
`sync.Manager.SyncLoss(ctx, port)`. A master clock that is still free-running
locks itself on the first tick (source detected → calibrate).

### 4.3. Holdover Timer

The holdover timer is scheduled on the injected `clock.Clock`
(`time.AfterFunc` in production, `FakeClock` in tests) for the duration of
`ptp/clock/holdover-timeout`, which the seed sets to 300 seconds. The timer only
wakes the domain's `Run` loop, which applies the transition with the domain lock
held, so a late expiry can never resurrect a holdover that a source restore
already ended.

During holdover the simulator keeps the offset the clock held when the source
was lost and adds drift of `100` ns per second (`DefaultDriftPPB`), i.e. one ppb
is one nanosecond per second; the reported jitter is that accumulated drift. A
locked clock instead reports noise within ±`DefaultJitterNS` (25 ns) of zero, and
a free-running clock wanders within ±`DefaultFreerunOffsetNS` (1000 ns).

---

## 5. Best Master Clock Algorithm (BMCA)

### 5.1. Overview

The BMCA is the mechanism by which PTP clocks elect the best master in a domain. Each clock periodically transmits `Announce` messages containing attributes that are used to compare clocks.

### 5.2. Comparison Attributes

The BMCA compares clocks using the following attributes in order of precedence:

| Priority | Attribute | Description |
|---|---|---|
| 1 | **priority1** | User-configurable priority (0–255, lower is better) |
| 2 | **clockClass** | Traceability and suitability as a time source |
| 3 | **clockAccuracy** | Precision of the clock |
| 4 | **offsetScaledLogVariance** | Stability of the oscillator |
| 5 | **priority2** | Secondary user-configurable priority |
| 6 | **clockIdentity** | Unique identifier; used to resolve ties |

### 5.3. TriadSim BMCA Simulation

TriadSim does not implement the full BMCA. Instead, the simulator allows the user to:

- **Configure** the local clock's `priority1`, `clockClass`, and `clockAccuracy` via RESTCONF or NETCONF.
- **Simulate** the effect of a master selection by triggering a `SourceDetected` or `SourceLost` event.
- **Observe** the resulting state transitions and the `parentDS` (parent data set) via SNMP OIDs from RFC 8173.

---

## 6. PTP Messages

TriadSim models the following PTP message types for simulation purposes. Actual message generation is not performed; the simulator reacts to management commands as if these messages had been received or transmitted.

| Message Type | Class | Description |
|---|---|---|
| **Sync** | Event | Master → Slave; contains origin timestamp T1 |
| **Follow_Up** | General | Master → Slave; contains precise T1 for two-step clocks |
| **Delay_Req** | Event | Slave → Master; requests delay measurement |
| **Delay_Resp** | General | Master → Slave; contains receive timestamp T4 |
| **Announce** | General | Master → all; contains BMCA attributes |
| **Signaling** | General | Unicast negotiation and management |
| **Management** | General | PTP management messages |

### 6.1. Message Rates

For G.8275.1 profile, the default message rates are:

| Message | Rate |
|---|---|
| Sync | 16 packets per second |
| Announce | 8 packets per second |
| Delay_Req | 16 packets per second |

TriadSim uses these rates as default configuration values.

---

## 7. SyncE and ESMC/SSM

### 7.1. SyncE Overview

Synchronous Ethernet (SyncE) provides frequency synchronization by locking the Ethernet PHY clock to a reference clock. It is defined in ITU-T G.8261 and G.8264. SyncE complements PTP: SyncE distributes frequency, while PTP distributes phase and time.

### 7.2. ESMC and SSM

The Ethernet Synchronization Messaging Channel (ESMC) carries Synchronization Status Messages (SSM) that indicate the quality level (QL) of the timing source. ESMC is defined in ITU-T G.8264. The SSM quality levels are defined in ITU-T G.781.

### 7.3. Quality Levels (QL)

TriadSim models the following SSM quality levels:

| QL Code | Name | Description | Priority |
|---|---|---|---|
| 0010 | **PRC** | Primary Reference Clock | Highest |
| 0100 | **SSU-A** | Synchronization Supply Unit A | High |
| 1000 | **SSU-B** | Synchronization Supply Unit B | Medium |
| 1011 | **SEC** | Synchronous Equipment Clock | Low |
| 1111 | **DNU** | Do Not Use | Lowest |

Extended QL options include `ePRTC`, `eEEC`, `PRTC`, `eEEC`, and `DNU`.

### 7.4. TriadSim SyncE Model

TriadSim simulates SyncE by:

- Maintaining a **QL state** for each SyncE-capable interface.
- Re-running the source selection on every tick of the injected clock.
- Writing the selected source and its QL to the read-only state leaves, so they
  are visible over RESTCONF and SNMP.
- Allowing manual source loss for testing (`POST /api/simulate/sync-loss`); a
  manual QL injection endpoint is not implemented.
- Reacting to the radio domain's `radioLinkDown`/clear alarms: the manager drains
  its bus subscription in `Run`, so a failed radio link drives the clock into
  holdover and a restored one locks it again (Phase 6.5).

---

## 8. Simulator Implementation

### 8.1. Go Package Structure

```
internal/sync/
├── doc.go      # package overview and scope
├── manager.go  # Manager: Deps, New, Run, Tick, state IO, holdover timer, offset/jitter
├── ptp.go      # State, Event, transition table, Handle, SyncLoss
├── synce.go    # SyncE source selection
└── esmc.go     # QL/SSM mapping and the simplified ESMC message
```

The manager is wired into `start` next to the L2 domain: it reads configuration
with `router.Snapshot(running)` and writes operational state with
`router.SetState` (running + candidate), exactly like `internal/l2`.

### 8.2. Manager API

```go
type Deps struct {
    Router       *router.Router
    Bus          *event.Bus
    Clock        clock.Clock
    TickInterval time.Duration
}

func New(deps Deps) (*Manager, error)
func (m *Manager) Run(ctx context.Context)                              // periodic work + expiry
func (m *Manager) Tick(ctx context.Context) error                       // selection + offset/jitter
func (m *Manager) Handle(ctx context.Context, e Event) (State, error)   // state machine
func (m *Manager) SyncLoss(ctx context.Context, source string) (State, error)
```

`State` is one of `freerun`, `acquiring`, `locked`, `holdover-in-spec` or
`holdover-out-of-spec`, the G.8275.1 clock-level states the model also accepts.
`Handle` is the only way to drive the machine; `SyncLoss` is the
management-plane entry point and validates that a named source is a SyncE
interface of the device.

### 8.3. Configuration Model

```go
type PTPClock struct {
    Mode            string  `path:"mode"`             // "master", "slave", "boundary"
    Domain          uint8   `path:"domain"`           // seeded to 24
    Priority1       uint8   `path:"priority1"`
    Priority2       uint8   `path:"priority2"`
    ClockClass      uint8   `path:"clock-class"`
    ClockAccuracy   uint8   `path:"clock-accuracy"`
    HoldoverTimeout uint32  `path:"holdover-timeout"` // seconds, seeded to 300
    State           string  `path:"state" config:"false"`
    Offset          float64 `path:"offset" config:"false"` // nanoseconds
    Jitter          float64 `path:"jitter" config:"false"` // nanoseconds
}
```

The router path is `ptp/clock/...`, the module is `sim-sync`
(`urn:sim:sync`).

### 8.4. Event Types

The domain publishes the bus-wide event types from `internal/event`; the
payload is the generic `Event` (`type`, `resource`, `severity`, `message`,
`timestamp`). There are no PTP-specific bus types.

| Event Type | Resource | Severity | Consumers |
|---|---|---|---|
| `StateTransition` | `ptp/clock` | (empty) | SNMP trap sender (`simSyncHoldover`/`simSyncRestored`), NETCONF notification dispatcher, Prometheus |
| `AlarmRaised` | `ptp/clock` | `major` | NETCONF notification dispatcher, Prometheus |
| `AlarmCleared` | `ptp/clock` | `cleared` | Same as `AlarmRaised` |

The events carry the structured fields of `internal/event`: `Domain` = `sync`,
`From`/`To` for a transition and `Alarm` for the holdover alarm. The sync trap is
derived from the transition, so entering `holdover-*` sends `simSyncHoldover` and
returning to `locked` sends `simSyncRestored`.

`AlarmRaised` is published when the holdover expires (the clock leaves
`holdover-in-spec` for `holdover-out-of-spec`); `AlarmCleared` is published when
a clock in `holdover-out-of-spec` reaches `locked` again.

---

## 9. Management Interfaces

### 9.1. SNMP

TriadSim exposes the PTP clock through vendor OIDs under
`1.3.6.1.4.1.99999.2.*`. The clock is a single object, so the scalars end in
`.0`; the SyncE interface table is indexed by the interface index (radio0=1,
eth0=2, eth1=3), the same instance the interface and bridge MIB tables use. The
RFC 8173 `ptpbaseMIB` (`1.3.6.1.2.1.241`) is not implemented.

| OID | Model path | Object | Type | Access |
|---|---|---|---|---|
| `1.3.6.1.4.1.99999.2.1.1.0` | `ptp/clock/state` | `simSyncPtpState` | Integer | read-only |
| `1.3.6.1.4.1.99999.2.1.2.0` | `ptp/clock/offset` | `simSyncPtpOffset` | OpaqueDouble | read-only |
| `1.3.6.1.4.1.99999.2.1.3.0` | `ptp/clock/domain` | `simSyncPtpDomain` | Gauge32 | read-write |
| `1.3.6.1.4.1.99999.2.1.4.0` | `ptp/clock/priority1` | `simSyncPtpPriority1` | Gauge32 | read-write |
| `1.3.6.1.4.1.99999.2.1.5.0` | `synce/selected-ql` | `simSyncSyncEQL` | Integer | read-only |
| `1.3.6.1.4.1.99999.2.1.6.0` | `synce/selected-extended-ql` | `simSyncSyncEExtendedQL` | Integer | read-only |
| `1.3.6.1.4.1.99999.2.1.7.1.1.<ifIndex>` | `synce/interfaces/interface[name=…]/ql` | `simSyncSyncEIfQL` | Integer | read-only |
| `1.3.6.1.4.1.99999.2.1.7.1.2.<ifIndex>` | `synce/interfaces/interface[name=…]/ssm-enabled` | `simSyncSyncEIfSSMEnabled` | Integer | read-only |
| `1.3.6.1.4.1.99999.2.1.8.0` | `ptp/clock/jitter` | `simSyncPtpJitter` | OpaqueDouble | read-only |

`simSyncPtpState` reports `freerun`(1), `acquiring`(2), `locked`(3),
`holdover-in-spec`(4) or `holdover-out-of-spec`(5); the quality levels are the
4-bit Option I SSM codes (`QL-PRC`=2, `QL-SSU-A`=4, `QL-SSU-B`=8, `QL-SEC`=11,
`QL-DNU`=15) and the extended codes are `QL-PRTC`=0x20, `QL-ePRTC`=0x21,
`QL-eEEC`=0x22.

### 9.2. NETCONF

The clock lives under the `sim-sync` module. `ptp/clock/state`, `offset`,
`jitter` and the SyncE selection are read-only (`config:"false"`), so
`get-config` never returns them; use RESTCONF with `?content=all`.

```xml
<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target><candidate/></target>
    <config>
      <ptp xmlns="urn:sim:sync">
        <clock>
          <mode>master</mode>
          <domain>24</domain>
          <priority1>128</priority1>
          <holdover-timeout>300</holdover-timeout>
        </clock>
      </ptp>
    </config>
  </edit-config>
</rpc>
```

### 9.3. RESTCONF

```bash
# Get the PTP clock (configuration and state).
curl http://localhost:8080/restconf/data/sim-sync:ptp/clock
# {"sim-sync:ptp":{"clock":{"domain":24,...,"state":"locked","offset":-12.5,"jitter":12.5}}}

# Get just the state leaf.
curl http://localhost:8080/restconf/data/sim-sync:ptp/clock/state
# {"sim-sync:state":"locked"}

# Configure PTP.
curl -X PATCH http://localhost:8080/restconf/data/sim-sync:ptp/clock \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-sync:clock":{"mode":"master","domain":24}}'

# Get the selected SyncE source and its quality level.
curl http://localhost:8080/restconf/data/sim-sync:synce/selected-source
curl http://localhost:8080/restconf/data/sim-sync:synce/selected-ql

# Inject sync loss -> PTP enters holdover.
curl -X POST http://localhost:8080/api/simulate/sync-loss \
  -H 'Content-Type: application/json' -d '{"port":"eth0"}'
# {"port":"eth0","state":"holdover-in-spec","status":"ok"}
```

An unknown port in `sync-loss` answers `422 invalid-value`, a missing body
`400`, a wrong method `405` and a server without the sync domain `501`.

### 9.4. CLI

The cobra commands (`dump`, `alarm inject`, `sync holdover`) arrive in Phase
6.6. Until then the domain is driven through RESTCONF or the test suite.

---

## 10. Metrics

The Prometheus gauges below are **not exposed**; they are the target of the
reference, not the implementation. Phase 6.4 exposes only the counters that the
EventBus can feed:

| Metric | Type | Exposed |
|---|---|---|
| `simulator_ptp_state_transitions_total{from,to}` | Counter | yes — one increment per PTP transition |
| `simulator_alarms_total{type="sync",severity}` | Counter | yes — the holdover alarm and its clear |
| `simulator_ptp_state` | Gauge | no — read the clock state over RESTCONF or SNMP instead |
| `simulator_ptp_offset_ns` | Gauge | no |
| `simulator_ptp_holdover_seconds` | Gauge | no |
| `simulator_synce_ql` | Gauge | no — the selected QL is a model leaf and an SNMP object |

See [../metrics.md](../metrics.md) for the implemented set.

---

## 11. Simplifications and Scope

TriadSim intentionally simplifies PTP and SyncE to focus on management plane testing:

| Aspect | TriadSim Behavior |
|---|---|
| **PTP wire protocol** | Not implemented; state machine is event-driven |
| **BMCA** | Attributes are configurable; election is simulated |
| **Message timestamps** | Not generated; offset is configured or drifted |
| **SyncE PHY** | Not simulated; QL state is maintained at the management level |
| **ESMC frames** | Not generated; QL changes are triggered via API |
| **PTP security (v2.1 auth TLV)** | Not implemented |
| **Unicast/multicast negotiation** | Not implemented |

These simplifications allow the simulator to be lightweight and focused on integration testing with external management systems.

---

## 12. References

| Document | Title |
|---|---|
| **IEEE 1588-2019** | IEEE Standard for a Precision Clock Synchronization Protocol for Networked Measurement and Control Systems |
| **IEEE 1588-2008** | IEEE Standard for a Precision Clock Synchronization Protocol (PTP v2) |
| **ITU-T G.8275.1** | Precision time protocol telecom profile for phase/time synchronization with full timing support from the network |
| **ITU-T G.8275.2** | Precision time protocol telecom profile for phase/time synchronization with partial timing support |
| **ITU-T G.8265.1** | Precision time protocol telecom profile for frequency synchronization |
| **ITU-T G.8261** | Timing and synchronization aspects in packet networks |
| **ITU-T G.8264** | Distribution of timing information through packet networks |
| **ITU-T G.781** | Synchronization layer functions for frequency synchronization based on the physical layer |
| **RFC 8173** | Precision Time Protocol Version 2 (PTPv2) Management Information Base |
| **RFC 9760** | Enterprise Profile for the Precision Time Protocol with Mixed Multicast and Unicast Messages |
