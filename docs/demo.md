# Demo: the cross-domain scenario

This is the canonical walkthrough of `.docs/desicion.md` §3.6: **one request** fails the radio
link, and the three domains plus the management planes react to it.

```
POST /api/simulate/radio-failure {"link":"radio0"}
  │
  ├─ radio   RSSI and fade margin drop, link-state → down → AlarmRaised(radioLinkDown, critical)
  │
  ├─ sync    the raised alarm is a lost reference → PTP locked → holdover-in-spec → StateTransition
  │
  ├─ SNMP    trap 1.3.6.1.4.1.99999.0.1 simRadioLinkDown (and .0.3 simSyncHoldover)
  ├─ NETCONF <notification> to every subscribed session
  └─ metrics simulator_alarms_total{type="radio",severity="critical"} +1
```

## 1. Start the simulator

```bash
go run ./cmd/simulator start --config configs/default.yaml
```

The startup log line reports the bound addresses:

```json
{"level":"INFO","msg":"simulator starting","snmp_port":1161,"snmp_trap_host":"127.0.0.1","snmp_trap_port":1162,
 "netconf_port":1830,"restconf_port":8080,"metrics_port":9090,...}
```

## 2. Start a trap receiver

In a second shell, on the configured trap port:

```bash
snmptrapd -f -Lo -p 1162
```

Any receiver works — the simulator only sends, and trap delivery is fire-and-forget.

## 3. Subscribe to notifications (optional)

In a third shell, subscribe to the NETCONF event stream:

```bash
ssh -p 1830 -s admin@localhost netconf
```

```xml
<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <create-subscription><stream>sim-events</stream></create-subscription>
</rpc>
]]>]]>
```

## 4. Fail the radio link

```bash
curl -X POST http://localhost:8080/api/simulate/radio-failure -d '{"link":"radio0"}'
# {"link":"radio0","state":"down","status":"ok"}
```

or, equivalently, through the CLI:

```bash
go run ./cmd/simulator alarm inject --type radioLinkDown --link radio0
```

## 5. Observe every effect

**The link state** (`link-state` is the read-only vendor object `1.3.6.1.4.1.99999.1.1.8`):

```bash
go run ./cmd/simulator dump --format json --content nonconfig | grep -A2 link-state
# "link-state": "down"
```

**The PTP clock** entered holdover:

```bash
curl -s http://localhost:8080/restconf/data/sim-sync:ptp/clock/state
# {"sim-sync:state":"holdover-in-spec"}
```

**The trap receiver** printed `simRadioLinkDown` for `radio0`, followed by `simSyncHoldover`:

```
SNMPv2-Trap-PDU, community public
  sysUpTime.0      1.3.6.1.2.1.1.3.0
  snmpTrapOID.0    1.3.6.1.4.1.99999.0.1   simRadioLinkDown
  simRadioRssi.0   1.3.6.1.4.1.99999.1.1.1.0
  ifDescr.1        1.3.6.1.2.1.2.2.1.2.1   "radio0"
```

**The NETCONF session** received a `<notification>` carrying the raised alarm (`<alarm>`,
`<resource>`, `<severity>`), and a second one for the PTP transition (`<from>`, `<to>`).

**The metrics** counted it:

```bash
curl -s http://localhost:9090/metrics | grep -E 'simulator_alarms_total|simulator_ptp_state'
# simulator_alarms_total{severity="critical",type="radio"} 1
# simulator_ptp_state_transitions_total{from="locked",to="holdover-in-spec"} 1
```

## 6. Restore the link

```bash
curl -X POST http://localhost:8080/api/simulate/radio-restore -d '{"link":"radio0"}'
# {"link":"radio0","state":"up","status":"ok"}
```

The radio domain clears the alarm, the sync domain locks the clock again, and the cleared alarm and
the restored PTP state produce `simRadioLinkUp` (`.0.2`) and `simSyncRestored` (`.0.4`) traps plus
`simulator_alarms_total{severity="cleared",type="radio"} 1`.

## 7. Vary the fade

`POST /api/simulate/radio-failure` takes an optional `fade-db`; a moderate fade lands the link in
the **degraded** band instead of down and raises `radioLinkDegraded` (major):

```bash
curl -X POST http://localhost:8080/api/simulate/radio-failure -d '{"link":"radio0","fade-db":30}'
go run ./cmd/simulator dump --format json --content nonconfig | grep link-state
# "link-state": "degraded"
```

The link recovers by itself once the received level and the fade margin pass their clear thresholds
(−82 dBm and 8 dB; see [protocols/RADIO-RRL.md](protocols/RADIO-RRL.md)), which the ATPC loop makes
it reach after the fade ends.

## Automated forms

The same scenario is covered twice in the test suite:

- `internal/cli/crossdomain_test.go` runs it in-process with ephemeral ports — `go test ./...`
  proves it without Docker;
- `test/integration/crossdomain_test.go` runs the compiled binary in a container and drives it with
  real clients — `go test -tags=integration ./...`.

`scripts/demo.sh` reproduces the whole scenario in one command: it builds the binary, starts it on
a temporary configuration, fails the radio link, asserts the PTP holdover and the
`simulator_alarms_total{severity="critical",type="radio"}` counter, restores the link and asserts
the cleared counter. `snmptrapd` and `ssh` are used when they are installed; `DEMO_NETCONF=1` also
opens a NETCONF subscription and prints the notifications it receives. The ports can be overridden
with `DEMO_SNMP_PORT`, `DEMO_TRAP_PORT`, `DEMO_NETCONF_PORT`, `DEMO_RESTCONF_PORT` and
`DEMO_METRICS_PORT`. See [cli.md](cli.md) for the commands and [metrics.md](metrics.md) for the
counters.
