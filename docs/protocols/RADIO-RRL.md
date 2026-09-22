# RADIO-RRL.md

**Project:** TriadSim — Lightweight Telecom Equipment Simulator  
**Technology:** Radio Relay Link (RRL) / Point-to-Point Microwave  
**Frequency Bands:** 6–86 GHz (simulated: 6, 11, 18, 23, 38 GHz)  
**Duplexing:** FDD  
**Simulated Modulation:** QPSK to 4096-QAM with ACM  
**Standards Baseline:** ETSI EN 302 217, ITU-R P.530, ITU-R F.1101, RFC 8561

---

## 1. Introduction

The Radio Relay Link (RRL) is the wireless transmission segment of TriadSim’s simulated telecom equipment. It models a point-to-point microwave/millimeter-wave link carrying packet traffic between two simulated nodes. The RRL implementation covers the radio-layer parameters that a network management system would monitor and configure: link budget, received signal level (RSSI/RSL), transmit power control, adaptive coding and modulation (ACM), modulation profiles, fade margin, and radio-specific alarms.

TriadSim does not simulate RF propagation or generate actual modulated waveforms. Instead, it maintains a state model of the radio link that responds to management commands (SNMP SetRequest, NETCONF edit-config, RESTCONF PATCH) and environmental events (rain fade injection, multipath fade injection). This allows integration testing of NMS applications without RF hardware.

The RRL model is based on the following standards:

- **ETSI EN 302 217** series — Harmonised standard for point-to-point fixed radio systems; defines equipment classes, RSL thresholds, ATPC, and modulation formats.
- **ITU-R P.530** — Propagation data and prediction methods for terrestrial line-of-sight systems; defines fade margin calculation and outage probability.
- **ITU-R F.1101** — Characteristics of digital fixed wireless systems below about 17 GHz; defines ATPC and ACM operational principles.
- **RFC 8561** — YANG data model for microwave radio link; defines the management object structure.
- **ITU-T G.826** — Error performance parameters and objectives; defines severely errored seconds (SES) and unavailable time criteria used for radio alarm severity.

---

## 2. Link Budget

### 2.1. General Equation

The link budget determines whether the received signal level (RSL) at the far-end receiver exceeds the receiver threshold with sufficient margin. For a clear line-of-sight (LOS) radio link, the power budget equation is:

\[
P_{RX} = P_{TX} + G_{TX} + G_{RX} - L_{FS} - L_{feed} - L_{atm} - L_{misc}
\]

| Term | Description | Typical Range (Simulated) |
|---|---|---|
| \(P_{TX}\) | Transmit power at antenna port | −10 to +30 dBm |
| \(G_{TX}\) | Transmit antenna gain | 30–45 dBi |
| \(G_{RX}\) | Receive antenna gain | 30–45 dBi |
| \(L_{FS}\) | Free-space path loss | 120–160 dB (link length dependent) |
| \(L_{feed}\) | Feed/connector loss | 1–5 dB |
| \(L_{atm}\) | Atmospheric absorption | 0.1–2 dB |
| \(L_{misc}\) | Miscellaneous losses (pointing, radome) | 0.5–3 dB |

The free-space path loss is calculated as:

\[
L_{FS} = 92.45 + 20\log_{10}(f_{GHz}) + 20\log_{10}(d_{km})
\]

where \(f_{GHz}\) is the frequency in GHz and \(d_{km}\) is the path length in kilometers.

TriadSim exposes the following link budget parameters via the management interfaces:

| Parameter | Model Path | Units | Configurable |
|---|---|---|---|
| `link-length` | `radio-link/link-budget/link-length` | km | Yes |
| `frequency` | `radio-link/link-budget/frequency` | GHz | Yes |
| `tx-power` | `radio-link/tx-power` | dBm | Yes |
| `tx-antenna-gain` | `radio-link/link-budget/tx-antenna-gain` | dBi | Yes |
| `rx-antenna-gain` | `radio-link/link-budget/rx-antenna-gain` | dBi | Yes |
| `feed-loss` | `radio-link/link-budget/feed-loss` | dB | Yes |
| `calculated-rsl` | `radio-link/link-budget/calculated-rsl` | dBm | No (derived) |

### 2.2. RSL Thresholds and Equipment Classes

ETSI EN 302 217-2 specifies receiver signal level (RSL) thresholds for different bit error rate (BER) objectives. For availability purposes, the reference BER is typically \(10^{-6}\); for quality purposes, it is \(10^{-8}\) for systems with radio interface capacity (RIC) ≤ 100 Mbit/s, or \(10^{-10}\) for systems with RIC > 100 Mbit/s.

The RSL threshold depends on the spectral efficiency class. Higher classes (complex modulation) have higher RSL/BER thresholds and are more sensitive to nonlinear distortion. The minimum upper RSL limit (the maximum received power before nonlinear distortion degrades BER) ranges from approximately −40 to −20 dBm, depending on frequency band and class.

TriadSim models the following ETSI equipment classes:

| Class | Typical Modulation | Spectral Efficiency | Simulated RSL Threshold (BER 10⁻⁶) |
|---|---|---|---|
| 1 | 2PSK / 2FSK | Low | −90 dBm |
| 2 | 4QAM / 4FSK | Low | −87 dBm |
| 3 | 8PSK | Medium | −84 dBm |
| 4L | 16QAM | Medium | −80 dBm |
| 4H | 32QAM | Medium-High | −77 dBm |
| 5L | 64QAM | High | −74 dBm |
| 5H | 128QAM | High | −71 dBm |
| 6L | 256QAM | Very High | −68 dBm |
| 6H | 512QAM | Very High | −65 dBm |
| 7 | 1024QAM | Extreme | −62 dBm |
| 8 | 2048QAM / 4096QAM | Extreme | −59 dBm |

The higher modes (class 7 and 8, and in some cases class 6H) have very limited fade margin and may not be suitable as reference modes for availability objectives. In TriadSim, these modes are available for configuration but are flagged as `high-risk-reference-mode` in the YANG model.

---

## 3. RSSI / RSL

### 3.1. Definition

The Received Signal Strength Indicator (RSSI) — equivalently, the Received Signal Level (RSL) — is the power level of the received signal at the radio receiver input. In TriadSim, RSSI is the primary monitored metric for link health.

| Attribute | Value |
|---|---|
| **Management name** | `rssi` (JSON/RESTCONF), `simRadioRssi` (SNMP), `actual-received-level` (RFC 8561) |
| **SNMP OID** | `1.3.6.1.4.1.99999.1.1.1.0` |
| **RESTCONF path** | `/restconf/data/sim-radio-link:radio-link/rssi` |
| **YANG leaf** | `actual-received-level` (RFC 8561, decimal64, units dBm) |
| **Range** | −99 to −20 dBm (RFC 8561 range: `-99..-20`) |
| **Resolution** | 0.1 dB |

### 3.2. RSSI Alarm Threshold

RFC 8561 defines a `received-level-alarm-threshold` leaf with a default value of −99 dBm. An alarm is generated when the received power level falls below the specified threshold.

TriadSim implements this as a configurable threshold:

| Parameter | Default | Range | Description |
|---|---|---|---|
| `rssi-alarm-threshold` | −85 dBm | −99 to −50 dBm | Alarm trigger when RSSI < threshold |
| `rssi-clear-threshold` | −82 dBm | −95 to −45 dBm | Alarm clear when RSSI > threshold (hysteresis) |

### 3.3. RSSI Monitoring

The RSSI value is updated by the simulator's radio state model at a configurable interval (default: 1 second). The model applies:

- **Clear-sky RSL**: calculated from the link budget.
- **Fade injection**: when a fade event is triggered (rain, multipath), the RSSI is reduced by the fade depth.
- **ATPC adjustment**: when ATPC is active, the transmitter power is adjusted to compensate for fade, partially offsetting RSSI reduction.

Example RESTCONF retrieval:

```http
GET /restconf/data/sim-radio-link:radio-link/rssi HTTP/1.1
Host: localhost:8080
Accept: application/yang-data+json
```

```json
{
  "sim-radio-link:rssi": -72.5
}
```

Example SNMP GetRequest:

```
GetRequest:
  OID: 1.3.6.1.4.1.99999.1.1.1.0

GetResponse:
  Value: -72.5
```

---

## 4. ATPC — Automatic Transmit Power Control

### 4.1. Overview

Automatic Transmit Power Control (ATPC) is a dynamic mechanism that adjusts the transmitter output power based on the received signal level at the far end. When propagation conditions are favorable, the transmitter operates at reduced power, reducing interference to other systems. When fading occurs, the transmitter increases power to partially offset the fade.

ITU-R F.1101 describes ATPC as a technology widely used in digital fixed wireless systems. In a typical implementation, the RSL is monitored at the receiver and sent back to the transmitter. When the RSL falls below its expected level, the transmitter increases power.

### 4.2. ATPC Operation in TriadSim

TriadSim models ATPC as a control loop with the following parameters:

| Parameter | Model Path | Units | Default | Description |
|---|---|---|---|---|
| `atpc-enabled` | `radio-link/atpc/enabled` | Boolean | `false` | Enable/disable ATPC |
| `atpc-target-rsl` | `radio-link/atpc/target-rsl` | dBm | −45.0 | Target RSL at far end |
| `atpc-min-power` | `radio-link/atpc/min-power` | dBm | −10.0 | Minimum transmit power |
| `atpc-max-power` | `radio-link/atpc/max-power` | dBm | +30.0 | Maximum transmit power |
| `atpc-range` | `radio-link/atpc/range` | dB | 20.0 | ATPC dynamic range |
| `atpc-current-power` | `radio-link/atpc/current-power` | dBm | — | Current transmit power (read-only) |

The ATPC control law is:

\[
P_{TX,current} = \min\left(P_{TX,max}, \max\left(P_{TX,min}, P_{TX,current} + \Delta\right)\right)
\]

where \(\Delta\) is the power adjustment step, computed as:

\[
\Delta = \text{clamp}\left((RSL_{target} - RSL_{measured}) \cdot k_p, -\Delta_{max}, +\Delta_{max}\right)
\]

\(k_p\) is a proportional gain (default: 1.0), and \(\Delta_{max}\) is the maximum step per update (default: 1 dB).

### 4.3. ATPC and Licensing

ETSI EN 302 217-1 distinguishes between two ATPC scenarios:

1. **ATPC not imposed by licensing**: The user may apply ATPC reduction for general interference improvement. The spectrum mask is not relevant in the ATPC range.
2. **ATPC imposed as pre-condition of coordination/licensing**: Interference impact is evaluated with power reduced by the ATPC range. The spectrum mask must be respected within the assumed ATPC range, which typically remains within the "initial" sub-range where the spectrum mask is still fulfilled.

TriadSim models both scenarios via a configuration flag:

| Parameter | Values | Description |
|---|---|---|
| `atpc-licensing-mode` | `none`, `imposed`, `voluntary` | Licensing condition |

### 4.4. RTPC — Remote Transmit Power Control

RTPC is the manual equivalent of ATPC: the NMS explicitly sets the transmit power attenuation. TriadSim supports RTPC as a fallback when ATPC is disabled.

| Parameter | Model Path | Units | Description |
|---|---|---|---|
| `rtpc-attenuation` | `radio-link/rtpc/attenuation` | dB | Manual attenuation setting |
| `rtpc-enabled` | `radio-link/rtpc/enabled` | Boolean | RTPC active flag |

When both ATPC and RTPC are enabled, ATPC takes precedence; RTPC attenuation is applied as a baseline offset.

---

## 5. ACM — Adaptive Coding and Modulation

### 5.1. Overview

Adaptive Coding and Modulation (ACM) is a technique that dynamically adjusts the modulation order and coding rate based on channel conditions. As propagation conditions degrade (e.g., due to rain fade), the system switches to a more robust modulation scheme with lower spectral efficiency but higher receiver sensitivity. When conditions improve, it switches back to a higher-order modulation to increase capacity.

ITU-R F.1101 identifies ACM as an effective countermeasure to adverse propagation conditions and a means of reducing interference. ACM enables graceful performance degradation rather than complete link failure, as occurs with fixed modulation schemes.

### 5.2. ACM Profiles

An ACM profile is identified by a combination of channel separation and coding-modulation values. ETSI EN 302 217-2 defines modulation formats from 2 states to 2048 states (amplitude and/or phase and/or frequency modulated). The standard defines reference modes that are intermediate to the full modulation set (e.g., 8PSK, 32QAM, 64QAM).

TriadSim implements a configurable ACM profile table. Each entry defines a modulation/coding mode with its associated receiver threshold and capacity:

| Profile ID | Modulation | Coding Rate | Spectral Efficiency (bit/s/Hz) | RSL Threshold (dBm) | Capacity (Mbit/s @ 28 MHz) |
|---|---|---|---|---|---|
| `acm-1` | QPSK | 1/2 | 1.0 | −88.0 | 28 |
| `acm-2` | QPSK | 3/4 | 1.5 | −86.0 | 42 |
| `acm-3` | 8PSK | 2/3 | 2.0 | −83.0 | 56 |
| `acm-4` | 16QAM | 3/4 | 3.0 | −79.0 | 84 |
| `acm-5` | 32QAM | 4/5 | 4.0 | −76.0 | 112 |
| `acm-6` | 64QAM | 4/5 | 5.0 | −73.0 | 140 |
| `acm-7` | 128QAM | 5/6 | 6.0 | −70.0 | 168 |
| `acm-8` | 256QAM | 5/6 | 7.0 | −67.0 | 196 |
| `acm-9` | 512QAM | 7/8 | 8.0 | −64.0 | 224 |
| `acm-10` | 1024QAM | 7/8 | 9.0 | −61.0 | 252 |
| `acm-11` | 2048QAM | 8/9 | 10.0 | −58.0 | 280 |
| `acm-12` | 4096QAM | 9/10 | 11.0 | −55.0 | 308 |

### 5.3. ACM Control Loop

TriadSim implements ACM as a state machine driven by RSSI and SNR measurements:

```
     ┌──────────────────────────────────────────────────────┐
     │                    ACM State Machine                 │
     │                                                      │
     │   IDLE ──► DOWNGRADE ──► STABLE ──► UPGRADE ──► IDLE │
     │                                                      │
     └──────────────────────────────────────────────────────┘
```

| State | Condition | Action |
|---|---|---|
| **IDLE** | RSSI within current profile's operating range | No change |
| **DOWNGRADE** | RSSI < current profile threshold + hysteresis_down | Switch to next lower profile |
| **STABLE** | RSSI > current profile threshold + hysteresis_up | Hold current profile |
| **UPGRADE** | RSSI > next higher profile threshold + hysteresis_up | Switch to next higher profile |

Default hysteresis values:

| Parameter | Default | Description |
|---|---|---|
| `hysteresis-down` | 2.0 dB | Additional margin before downgrade |
| `hysteresis-up` | 3.0 dB | Additional margin before upgrade |
| `upgrade-hold-time` | 30 s | Minimum time in STABLE before upgrade |

### 5.4. ACM Configuration

| Parameter | Model Path | Values | Description |
|---|---|---|---|
| `acm-enabled` | `radio-link/acm/enabled` | Boolean | Enable ACM |
| `acm-min-profile` | `radio-link/acm/min-profile` | `acm-1` … `acm-12` | Minimum profile (floor) |
| `acm-max-profile` | `radio-link/acm/max-profile` | `acm-1` … `acm-12` | Maximum profile (ceiling) |
| `acm-current-profile` | `radio-link/acm/current-profile` | (read-only) | Active profile |
| `acm-capacity` | `radio-link/acm/current-capacity` | (read-only) | Current capacity in Mbit/s |
| `acm-mode` | `radio-link/acm/mode` | `adaptive`, `fixed` | Operating mode |

When `acm-mode` is `fixed`, the profile specified by `acm-current-profile` is used regardless of channel conditions. This is equivalent to the `single` coding-modulation mode in RFC 8561.

> **Implemented model (Phase 1.1).** `internal/model/radio.go` addresses ACM profiles by index:
> `acm/min-profile` and `acm/max-profile` are `uint8` values `1..12` corresponding to the
> `acm-1`…`acm-12` rows of the table above, and the display name lives in `ModProfile.Name`.
> `rssi`, `fade-margin` and `capacity` are leaves of `radio-link` (as in §9.2), not members of a
> `radio-link/performance` container.

### 5.5. Relationship to RFC 8561

RFC 8561 defines the YANG structure for ACM configuration:

- `container adaptive` — for ACM mode, with `selected-min-acm` and `selected-max-acm` leaves that bound the adaptive range.
- `leaf actual-tx-cm` — reports the actual coding/modulation in the transmitting direction.
- `leaf actual-snir` — reports the actual signal-to-noise-plus-interference ratio.

TriadSim maps its internal ACM state to these RFC 8561 objects for NETCONF/RESTCONF compatibility.

---

## 6. Modulation Profiles

### 6.1. Modulation Schemes

TriadSim models the following modulation schemes, which are commonly used in point-to-point microwave radio systems:

| Scheme | States | Bits/Symbol | Typical Use | Required C/N (BER 10⁻⁶) |
|---|---|---|---|---|
| QPSK | 4 | 2 | Robust, long-haul, low capacity | 10.5 dB |
| 8PSK | 8 | 3 | Medium robustness | 14.0 dB |
| 16QAM | 16 | 4 | Medium capacity | 17.5 dB |
| 32QAM | 32 | 5 | Medium-high capacity | 20.5 dB |
| 64QAM | 64 | 6 | High capacity | 23.5 dB |
| 128QAM | 128 | 7 | High capacity | 26.5 dB |
| 256QAM | 256 | 8 | Very high capacity | 29.5 dB |
| 512QAM | 512 | 9 | Very high capacity | 32.5 dB |
| 1024QAM | 1024 | 10 | Extreme capacity | 35.5 dB |
| 2048QAM | 2048 | 11 | Extreme capacity | 38.5 dB |
| 4096QAM | 4096 | 12 | Extreme capacity | 41.5 dB |

Multi-level QAM from 16-QAM to 256-QAM is generally adopted for fixed wireless systems. Progress in semiconductor devices now enables 1024-QAM and work up to 4096-QAM. However, higher-order modulation requires an even higher carrier-to-noise ratio (CNR), and the use of 1024-QAM increases the data rate only by 1.25× compared with 256-QAM, so 1024-QAM and above are not widely used and are foreseen to be limited when adaptive modulation is concerned.

### 6.2. Forward Error Correction (FEC)

TriadSim models FEC as a coding gain applied to the receiver threshold. The following FEC schemes are supported:

| FEC Scheme | Coding Gain (typical) | Latency | Complexity |
|---|---|---|---|
| None | 0 dB | 0 | — |
| Reed-Solomon (RS) | 3–5 dB | Low | Low |
| Trellis Coded Modulation (TCM) | 4–6 dB | Low | Medium |
| Low-Density Parity-Check (LDPC) | 8–12 dB | Medium | High |

LDPC codes, based on iterative decoding, are the most powerful FEC scheme commonly available and are increasingly adopted in modern fixed wireless systems.

### 6.3. XPIC

Cross-Polarization Interference Canceller (XPIC) allows two radio channels to operate on the same frequency with orthogonal polarizations, doubling capacity without additional spectrum. XPIC generates a replica of the interference from the orthogonal polarization and subtracts it from the received signal.

TriadSim models XPIC as a capacity multiplier and a C/N degradation factor:

| Parameter | Default | Description |
|---|---|---|
| `xpic-enabled` | `false` | XPIC active |
| `xpic-cn-degradation` | 1.5 dB | C/N penalty due to cross-polar interference |
| `xpic-improvement-factor` | 20 dB | Interference cancellation improvement |

When XPIC is enabled, the effective capacity is doubled, but the required C/N is increased by `xpic-cn-degradation`.

---

## 7. Fade Margin

### 7.1. Definition

Fade margin is the difference between the nominal received signal level (RSL) in clear-sky conditions and the receiver threshold:

\[
FM = RSL_{nominal} - RSL_{threshold}
\]

where \(RSL_{threshold}\) is the receiver sensitivity for the target BER (typically \(10^{-6}\) for availability purposes). A larger fade margin provides greater resilience to fading.

ITU-R P.530 provides methods for calculating the fade margin required to achieve a given availability objective. The required fade margin depends on the link length, frequency, path inclination, and climatic conditions. For a typical microwave link, the fade margin required for 99.99% availability may be in the range of 20–30 dB, depending on the path.

### 7.2. Fade Types

TriadSim models two fade types:

| Fade Type | Frequency Range | Dominant Below 10 GHz | Dominant Above 17 GHz |
|---|---|---|---|
| **Multipath / flat fade** | All | ✓ (dominant) | — |
| **Rain fade** | > 10 GHz | — | ✓ (dominant) |
| **Frequency-selective fade** | All | ✓ | — |

ITU-R F.1101 states that rain attenuation predominates at frequencies above about 17 GHz, while multipath distortion predominates at frequencies below about 10 GHz. In the 10–17 GHz range, both objectives should be considered.

### 7.3. Fade Margin Calculation (TriadSim Model)

TriadSim computes the fade margin as:

\[
FM = P_{TX} + G_{TX} + G_{RX} - L_{FS} - L_{feed} - L_{atm} - RSL_{threshold}
\]

The simulator exposes the calculated fade margin as a read-only metric:

| Parameter | Model Path | Units | Description |
|---|---|---|---|
| `fade-margin` | `radio-link/performance/fade-margin` | dB | Current fade margin |
| `nominal-rsl` | `radio-link/performance/nominal-rsl` | dBm | Clear-sky RSL |
| `rsl-threshold` | `radio-link/performance/rsl-threshold` | dBm | Receiver threshold (BER 10⁻⁶) |
| `availability` | `radio-link/performance/availability` | % | Estimated link availability |

### 7.4. Availability Estimation

TriadSim uses a simplified ITU-R P.530 model to estimate link availability:

\[
\text{Unavailability} = p_w \cdot \left( \frac{10^{-q_a \cdot FM / 20}}{1} \right)
\]

where \(p_w\) is the percentage of time in the worst month, \(q_a\) is a fade depth distribution parameter, and \(FM\) is the fade margin. The full ITU-R P.530 model incorporates path length, frequency, and terrain factors.

For simulation purposes, TriadSim allows the user to inject fade events with a specified depth and duration:

```bash
# Inject a 15 dB rain fade for 60 seconds
curl -X POST http://localhost:8080/api/simulate/fade \
  -d '{"type":"rain","depth_db":15,"duration_s":60}'
```

### 7.5. Fade Margin and ACM

When ACM is enabled, the effective fade margin is determined by the current ACM profile. Higher-order profiles have lower fade margins (because they require higher C/N), while lower-order profiles provide greater fade margin. A typical fade margin at the highest ACM mode is approximately 10–20 dB.

TriadSim calculates the ACM-adjusted fade margin as:

\[
FM_{ACM} = RSL_{nominal} - RSL_{threshold}(profile_{current})
\]

The simulator also exposes the maximum fade margin available with the most robust profile:

| Parameter | Description |
|---|---|
| `fade-margin-current` | Fade margin at current ACM profile |
| `fade-margin-max` | Fade margin at minimum (most robust) ACM profile |
| `fade-margin-min` | Fade margin at maximum (least robust) ACM profile |

---

## 8. Alarms

### 8.1. Alarm Overview

TriadSim generates radio-link alarms based on the following monitored conditions:

- Loss of signal (LOS)
- Loss of frame (LOF)
- Alarm indication signal (AIS) received from far end
- BER threshold exceeded
- RSL below alarm threshold
- Radio link degradation (ACM downgrade)
- Transmitter power out of range
- ATPC at limit

### 8.2. Alarm Severity Levels

TriadSim uses the ITU-T X.733 perceived severity model, with four levels:

| Severity | Code | Service Affecting | Description |
|---|---|---|---|
| **Critical** | 1 | Yes | Complete loss of service; immediate action required |
| **Major** | 2 | Yes | Significant service degradation; urgent action required |
| **Minor** | 3 | Potentially SA | Partial degradation; corrective action recommended |
| **Warning** | 4 | NSA | Non-service-affecting condition; diagnostic action |

ITU-T defines these as service affecting (SA) or non-service affecting (NSA) conditions. Critical and major alarms are typically SA; minor and warning are NSA or potentially SA and require corrective or diagnostic action.

### 8.3. Radio-Specific Alarm Definitions

| Alarm | Condition | Severity | SNMP Trap OID | NETCONF Notification |
|---|---|---|---|---|
| `radioLinkDown` | RSL below `rssi-alarm-threshold` for > `los-persistence` | Critical | `1.3.6.1.4.1.99999.0.1` | `radio-link-down` |
| `radioLinkDegraded` | ACM downgrade below `acm-min-profile` | Major | `1.3.6.1.4.1.99999.0.7` | `radio-link-degraded` |
| `radioLinkUp` | RSL above `rssi-clear-threshold` for > `clear-persistence` | Info (clear) | `1.3.6.1.4.1.99999.0.2` | `radio-link-up` |
| `txPowerHigh` | Transmit power > `atpc-max-power` − 1 dB | Minor | `1.3.6.1.4.1.99999.0.8` | `tx-power-high` |
| `txPowerLow` | Transmit power < `atpc-min-power` + 1 dB | Minor | `1.3.6.1.4.1.99999.0.9` | `tx-power-low` |
| `berThresholdExceeded` | BER > `ber-alarm-threshold` (configurable: 10⁻⁹ … 10⁻³) | Major | `1.3.6.1.4.1.99999.0.10` | `ber-exceeded` |
| `fadeMarginLow` | Fade margin < `fade-margin-min-threshold` (default: 6 dB) | Warning | `1.3.6.1.4.1.99999.0.11` | `fade-margin-low` |

RFC 8561 defines a `ber-alarm-threshold` leaf with enumeration values from 1e-9 to 1e-2. TriadSim supports the full range.

### 8.4. LOS and LOF Alarms

Loss of Signal (LOS) is a physical-layer condition indicating that the received signal level has dropped below the receiver sensitivity. Loss of Frame (LOF) indicates that the frame synchronization has been lost. Both are considered "red alarms" and are typically critical.

In TriadSim:

- **LOS** is asserted when `RSL < rssi-alarm-threshold` for a configurable persistence time (default: 100 ms).
- **LOF** is asserted when the simulated frame synchronization state indicates loss (triggered by a separate `frame-sync-loss` event).

Both alarms are cleared when the condition is no longer met for the clear persistence time (default: 1 second).

### 8.5. AIS — Alarm Indication Signal

AIS is a signal transmitted by a downstream network element to indicate an upstream failure. In a radio link, AIS may be received from the far-end radio if the far-end has detected a failure.

TriadSim models AIS as a received alarm condition:

| Parameter | Default | Description |
|---|---|---|
| `ais-detection-enabled` | `true` | Enable AIS detection |
| `ais-source` | `far-end` | Source of AIS indication |

When AIS is detected, the simulator asserts a `remote-alarm` indication in the radio status and generates a `radioLinkDegraded` alarm if the AIS persists beyond the configured threshold.

### 8.6. Alarm Configuration

```yaml
radio:
  alarms:
    los-persistence-ms: 100
    clear-persistence-ms: 1000
    rssi-alarm-threshold: -85.0
    rssi-clear-threshold: -82.0
    ber-alarm-threshold: 1e-6
    fade-margin-min-threshold: 6.0
    acm-degrade-alarm: true
    atpc-limit-alarm: true
    ais-detection: true
```

### 8.7. Alarm Clearing

Alarms are cleared when the alarm condition is no longer present for the clear persistence time. TriadSim generates a clear notification for each alarm type:

- SNMP: `simRadioLinkUp` trap
- NETCONF: `radio-link-up` notification
- RESTCONF: Event stream `radio-link-up` event

---

## 9. Management Interface Mapping

### 9.1. SNMP OID Mapping

| Object | OID | Type | Access |
|---|---|---|---|
| `simRadioRssi` | `1.3.6.1.4.1.99999.1.1.1` | Float | read-only |
| `simRadioFadeMargin` | `1.3.6.1.4.1.99999.1.1.2` | Float | read-only |
| `simRadioCapacity` | `1.3.6.1.4.1.99999.1.1.3` | Unsigned32 | read-only |
| `simRadioTxPower` | `1.3.6.1.4.1.99999.1.1.4` | Float | read-write |
| `simRadioAtpcEnabled` | `1.3.6.1.4.1.99999.1.1.5` | Integer | read-write |
| `simRadioAcmProfile` | `1.3.6.1.4.1.99999.1.1.6` | Integer | read-only |
| `simRadioModulation` | `1.3.6.1.4.1.99999.1.1.7` | Integer | read-only |
| `simRadioLinkState` | `1.3.6.1.4.1.99999.1.1.8` | Integer | read-only |

All of the objects above are registered in `internal/router/oid.go` (Phase 1.8 and 6.2). Floats are
carried as RFC 5342 `OpaqueDouble`; `simRadioLinkState` reports `up(1)`, `degraded(2)` or `down(3)`.
The objects are scalars (`.0`) because the MVP has one radio link, and they are built from the first
interface whose type is `radio`.

### 9.2. RESTCONF Paths

| Resource | Path |
|---|---|
| Radio link | `/restconf/data/sim-radio-link:radio-link` |
| RSSI | `/restconf/data/sim-radio-link:radio-link/rssi` |
| TX power | `/restconf/data/sim-radio-link:radio-link/tx-power` |
| ATPC config | `/restconf/data/sim-radio-link:radio-link/atpc` |
| ACM config | `/restconf/data/sim-radio-link:radio-link/acm` |
| Fade margin | `/restconf/data/sim-radio-link:radio-link/performance/fade-margin` |
| Radio alarms | `/restconf/data/sim-radio-link:radio-link/alarms` |
| Reset stats RPC | `/restconf/operations/sim-radio:reset-stats` |
| Inject alarm RPC | `/restconf/operations/sim-radio:inject-alarm` |

### 9.3. NETCONF YANG Module

TriadSim provides a `sim-radio-link` YANG module (embedded, not parsed at runtime) with the following structure:

```yang
module sim-radio-link {
  namespace "urn:sim:radio-link";
  prefix srl;

  container radio-link {
    leaf name { type string; }
    leaf tx-power { type decimal64; units "dBm"; }
    leaf rssi { type decimal64; units "dBm"; config false; }
    leaf fade-margin { type decimal64; units "dB"; config false; }

    container atpc {
      leaf enabled { type boolean; }
      leaf target-rsl { type decimal64; units "dBm"; }
      leaf current-power { type decimal64; units "dBm"; config false; }
    }

    container acm {
      leaf enabled { type boolean; }
      leaf min-profile { type string; }
      leaf max-profile { type string; }
      leaf current-profile { type string; config false; }
      leaf current-capacity { type uint32; units "Mbit/s"; config false; }
    }

    container performance {
      leaf fade-margin { type decimal64; units "dB"; config false; }
      leaf nominal-rsl { type decimal64; units "dBm"; config false; }
      leaf rsl-threshold { type decimal64; units "dBm"; config false; }
      leaf availability { type decimal64; units "percent"; config false; }
    }

    container alarms {
      leaf los-persistence-ms { type uint32; }
      leaf clear-persistence-ms { type uint32; }
      leaf rssi-alarm-threshold { type decimal64; units "dBm"; }
      leaf ber-alarm-threshold { type decimal64; }
      leaf fade-margin-min-threshold { type decimal64; units "dB"; }
    }
  }
}
```

---

## 10. CLI Examples

### 10.1. Retrieve RSSI and Fade Margin

```bash
curl -s http://localhost:8080/restconf/data/sim-radio-link:radio-link \
  -H 'Accept: application/yang-data+json' | jq
```

```json
{
  "sim-radio-link:radio-link": {
    "name": "radio0",
    "tx-power": 20.0,
    "rssi": -72.5,
    "fade-margin": 12.5,
    "atpc": {
      "enabled": true,
      "target-rsl": -45.0,
      "current-power": 18.5
    },
    "acm": {
      "enabled": true,
      "min-profile": "acm-1",
      "max-profile": "acm-8",
      "current-profile": "acm-5",
      "current-capacity": 112
    }
  }
}
```

### 10.2. Configure ATPC

```bash
curl -X PATCH http://localhost:8080/restconf/data/sim-radio-link:radio-link/atpc \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-radio-link:enabled":true,"sim-radio-link:target-rsl":-45.0}'
```

### 10.3. Configure ACM

```bash
curl -X PATCH http://localhost:8080/restconf/data/sim-radio-link:radio-link/acm \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-radio-link:enabled":true,"sim-radio-link:min-profile":"acm-1","sim-radio-link:max-profile":"acm-8"}'
```

### 10.4. Inject a Fade Event

```bash
curl -X POST http://localhost:8080/api/simulate/fade \
  -d '{"type":"rain","depth_db":15,"duration_s":120}'
```

### 10.5. Inject an Alarm via RPC

```bash
curl -X POST http://localhost:8080/restconf/operations/sim-radio:inject-alarm \
  -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-radio:input":{"link":"radio0","type":"radioLinkDown"}}'
```

### 10.6. SNMP Walk of Radio OIDs

```bash
snmpwalk -v2c -c public localhost:1161 1.3.6.1.4.1.99999.1
```

---

## 11. Metrics

TriadSim exposes the following Prometheus metrics for the radio link:

| Metric | Type | Description |
|---|---|---|
| `simulator_radio_rssi_dbm` | Gauge | Current RSSI in dBm |
| `simulator_radio_tx_power_dbm` | Gauge | Current transmit power in dBm |
| `simulator_radio_fade_margin_db` | Gauge | Current fade margin in dB |
| `simulator_radio_acm_profile` | Gauge | Current ACM profile index (1–12) |
| `simulator_radio_capacity_mbps` | Gauge | Current link capacity in Mbit/s |
| `simulator_radio_alarms_active` | Gauge | Number of active radio alarms |
| `simulator_radio_fade_events_total` | Counter | Total number of fade events injected |
| `simulator_radio_acm_changes_total` | Counter | Total number of ACM profile changes |

---

## 12. Simulator implementation (Phase 6)

`internal/radio` implements the parts of this reference the simulator needs. It is a **state
model**, never a waveform: no RF is generated, no modulated signal is decoded, and no frame is
encoded on the wire.

### 12.1. Link budget

`internal/radio/linkbudget.go` computes, once per tick per radio link:

```
L_FS = 92.45 + 20*log10(f_GHz) + 20*log10(d_km)
RSL  = P_TX + G_TX + G_RX − L_FS − feed-loss − 0.5 − 0.5
RSSI = clamp(RSL − injected-fade, −99, −20)            [dBm]
```

`P_TX` is `atpc/current-power` while ATPC is enabled and `tx-power` otherwise. Atmospheric and
miscellaneous losses are fixed constants, and the derived values are clamped to the model's ranges
so a domain state write can never make a later management-plane commit fail. The results are
written to the read-only leaves `rssi`, `fade-margin`, `capacity`, `link-budget/calculated-rsl`,
`atpc/current-power`, `acm/current-profile` and `acm/current-capacity`.

### 12.2. ATPC

`atpcStep` walks the transmit power towards `atpc/target-rsl` by at most 1 dB per tick and clamps
the result to `min-power..max-power`. The loop is open: the far end is not simulated, so the
transmitter reacts to its own received level.

### 12.3. ACM

With `acm/mode` `adaptive`, the highest profile of the `min-profile..max-profile` window whose
`rsl-threshold` the level still meets is selected — the highest capacity the link can carry. A
level below every threshold degrades to the most robust profile of the window. `fixed` mode and a
disabled ACM keep the configured `acm/current-profile`, and an empty profile table leaves the
derived leaves untouched.

### 12.4. Link state and alarms

`internal/radio/alarms.go` maintains one state per link — `link-state` = `up`, `degraded` or `down`,
exposed as `1.3.6.1.4.1.99999.1.1.8` (`simRadioLinkState`) — with raise and clear thresholds so a
level on a threshold does not flap:

| Alarm | Raised when | Cleared when | Severity |
|---|---|---|---|
| `radioLinkDown` | `rssi < −85 dBm` | `rssi ≥ −82 dBm` | critical |
| `radioLinkDegraded` | fade margin `< 6 dB` | fade margin `≥ 8 dB` | major |

A link that is down reports only `radioLinkDown`: entering it clears a raised `radioLinkDegraded`,
and recovering from it clears `radioLinkDown` before raising anything else. Each change publishes
`AlarmRaised`/`AlarmCleared` with `Domain: "radio"`, `Resource` = the link name and
`Alarm: "radioLinkDown"` or `"radioLinkDegraded"`.

### 12.5. Simulation API

```bash
# Fail the link: a 60 dB fade by default.
curl -X POST http://localhost:8080/api/simulate/radio-failure -d '{"link":"radio0"}'
# ... or a partial fade that lands in the degraded band.
curl -X POST http://localhost:8080/api/simulate/radio-failure -d '{"link":"radio0","fade-db":30}'
# Restore it.
curl -X POST http://localhost:8080/api/simulate/radio-restore -d '{"link":"radio0"}'
```

`POST /api/simulate/radio-failure` takes an optional `fade-db` (0 and an absent field mean the
domain's failure depth, 60 dB) and an optional `link` (empty selects the first radio link); the
response carries the resulting `link-state`. `POST /api/simulate/radio-restore` clears the injected
fade. `simulator alarm inject` is a thin client over the same endpoints.

### 12.6. Simplifications

- **No RF and no far end.** RSSI is a budget calculation, not a measurement; ATPC reacts to the
  local level and there is no RTPC, XPIC, FEC, AIS, LOF or BER model.
- **One radio link.** The vendor objects are scalars (`.0`) and are built from the first interface
  whose type is `radio`.
- **Alarms are two.** `radioLinkDown` and `radioLinkDegraded` (§8.3); the other rows of the alarm
  table are not simulated, and the fade, LOS persistence, BER and available-ITU-R-P.530 models are
  not implemented.
- **Traps are three, not eight.** `simRadioLinkDown` (`.0.1`), `simRadioLinkUp` (`.0.2`) and
  `simSyncHoldover`/`simSyncRestored` are documented in
  [SNMP.md](SNMP.md#62-triadsim-trap-oids); `radioLinkDegraded` raises a notification and a metric
  but has no trap OID.
- **The metric set is smaller than §11.** Only `simulator_alarms_total{type="radio",severity}`
  counts radio alarms; the per-object gauges of §11 are not implemented. See
  [../metrics.md](../metrics.md).

---

## 13. References

| Document | Title |
|---|---|
| **ETSI EN 302 217-1** | Point-to-point equipment; Part 1: Overview and system-independent common characteristics |
| **ETSI EN 302 217-2** | Point-to-point equipment; Part 2: Digital systems operating in frequency bands where coordination of frequency use is applied |
| **ETSI EN 302 217-3** | Point-to-point equipment; Part 3: Equipment for generic applications |
| **ITU-R P.530** | Propagation data and prediction methods required for the design of terrestrial line-of-sight systems |
| **ITU-R F.1101** | Characteristics of digital fixed wireless systems below about 17 GHz |
| **ITU-R F.758** | System parameters and considerations for the development of criteria for sharing or compatibility between digital fixed wireless systems |
| **RFC 8561** | A YANG Data Model for Microwave Radio Link |
| **RFC 8173** | Precision Time Protocol Version 2 (PTPv2) Management Information Base (cross-reference for timing aspects) |
| **ITU-T G.826** | End-to-end error performance parameters and objectives for international, constant bit-rate digital paths and connections |
| **ITU-T X.733** | Information technology — Open Systems Interconnection — Systems Management: Alarm reporting function |
| **ITU-T G.8275.1** | Precision time protocol telecom profile for phase/time synchronization with full timing support (for PTP/RRL interaction) |
