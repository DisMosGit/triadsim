# TriadSim

**TriadSim** is a pure-Go, single-binary simulator of a telecom device that combines three
domains on one managed device:

- **Radio link (RRL)** — link budget, RSSI, ATPC, ACM, modulation profiles, fade margin, capacity.
- **L2 switching** — VLAN (802.1Q, QinQ), MAC table, STP/RSTP, LLDP, interface counters, broadcast storms.
- **Synchronization** — PTP (IEEE 1588), SyncE, ESMC/SSM, holdover, quality levels.

All three domains share one managed-object model, one state store and one event bus, and are
exposed through a single management plane: **SNMP v2c**, **NETCONF**, **RESTCONF** and
(optionally) **gNMI**. No CGO, no sidecar processes, no external services — one binary, one
process, no Web UI (that lives in a separate repository).

> **Status: Phase 7 — gNMI, YANG and v0.1.0.** The repository builds, tests and starts; the
> foundations, managed-object models (radio, L2, sync, device), the running/candidate/startup
> store, the router, the SNMP v2c agent **and trap sender** with the Prometheus endpoint, the
> NETCONF subsystem (`get-config`/`edit-config`/`commit`/`discard-changes`, confirmed commit with
> rollback, `create-subscription` notifications), the chi-based RESTCONF server, the L2 domain
> (VLAN and QinQ, MAC forwarding database, simplified STP/RSTP, LLDP, counters, broadcast-storm
> simulation), the sync domain (PTP state machine with holdover, SyncE source selection, ESMC/SSM
> quality levels, simulated offset/jitter), the **radio domain** (link budget, ATPC, ACM,
> `radioLinkDown`/`radioLinkDegraded`) and the **optional gNMI service** (`Capabilities`, `Get`,
> `Set`, `Subscribe`) are in place. The cross-domain scenario runs end to end: one
> `POST /api/simulate/radio-failure` produces the alarm, the PTP holdover, an SNMP trap, a NETCONF
> notification, a gNMI update and the `simulator_alarms_total` counter. The CLI gained
> `alarm inject`, `dump`, `config validate` and `schema` (with the embedded YANG modules); ADRs
> 0002-0004 record the model, monolith and stack decisions. See [ROADMAP.md](ROADMAP.md) and
> [docs/demo.md](docs/demo.md).

## Quick start

Requirements: Go 1.27+ (Docker is optional and only needed for the integration tests,
`go test -tags=integration ./...`).

```bash
go mod download
make build                       # or: go build ./...
make test                        # or: go test ./...

# run (loads configs/default.yaml, logs JSON to stderr, Ctrl-C to stop)
go run ./cmd/simulator start --config configs/default.yaml

# in another shell: the SNMP agent answers on :1161
snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.2.2.1.2   # radio0, eth0, eth1
snmpget  -v2c -c public localhost:1161 1.3.6.1.4.1.99999.1.1.1.0  # RSSI as a float
curl -s localhost:9090/metrics | grep simulator_

# the RESTCONF server answers on :8080 (no auth)
curl -s localhost:8080/restconf/data/sim-l2-switching:vlans
curl -s localhost:8080/restconf/data/sim-l2-switching:stp/state?content=all
snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.17.4.3.1.2   # MAC -> bridge port

# and the NETCONF subsystem on :1830 (any user, no password)
ssh -p 1830 -s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null admin@localhost netconf

# the cross-domain scenario: fail the radio link, then watch the PTP clock follow
snmptrapd -f -Lo -p 1162 &                     # another shell
go run ./cmd/simulator alarm inject --type radioLinkDown --link radio0
curl -s localhost:8080/restconf/data/sim-sync:ptp/clock/state   # {"sim-sync:state":"holdover-in-spec"}
curl -s localhost:9090/metrics | grep simulator_alarms_total
```

The whole scenario is also one command: `./scripts/demo.sh`.

Configuration lives in [`configs/default.yaml`](configs/default.yaml); every field has a
documented default and is described in [docs/config.md](docs/config.md).

## Architecture

One binary, one state store, one event bus; the three domains never import each other and react
through events:

```
                        ┌──────────────────────────────────────────┐
                        │            cmd/simulator                 │
                        │            internal/cli (cobra)          │
                        └───────────────────┬──────────────────────┘
                                            │
   ┌──────────────┬──────────────┬──────────┴─────────┬──────────────┐
   │  internal/   │  internal/   │      internal/     │   internal/  │
   │  snmp :1161  │ netconf :1830│   restconf :8080   │  gnmi :9339  │
   │  traps :1162 │              │                    │  (optional)  │
   └──────┬───────┴──────┬───────┴─────────┬──────────┴──────┬───────┘
          │              │                 │                 │
          └──────────────┴────────┬────────┴─────────────────┘
                                  ▼
                        ┌───────────────────┐
                        │  internal/router  │  path ↔ model, OID ↔ path, RPC dispatch
                        └─────────┬─────────┘
                                  ▼
                        ┌───────────────────┐
                        │  internal/store   │  running / candidate / startup
                        └─────────┬─────────┘
                                  ▼
                        ┌───────────────────┐
                        │  internal/event   │  EventBus on Go channels
                        └─────────┬─────────┘
                                  ▼
        ┌───────────────┬─────────┴─────────┬───────────────┐
        │ internal/radio│      internal/l2  │  internal/sync│
        │ link budget,  │ VLAN, MAC, STP,   │ PTP, SyncE,   │
        │ ATPC, ACM     │ LLDP, counters    │ ESMC/SSM      │
        └───────────────┴───────────────────┴───────────────┘
```

[docs/architecture.md](docs/architecture.md) walks through the packages, the dependency rules and
the startup sequence; [docs/adr/0003-modular-monolith.md](docs/adr/0003-modular-monolith.md)
records why it is one process.

## Ports

| Plane | Port | Notes |
|---|---|---|
| SNMP agent | `1161` | v2c, community `public` |
| SNMP traps | `1162` | traps only, no informs |
| NETCONF | `1830` | SSH subsystem `netconf`, no auth |
| RESTCONF | `8080` | no auth |
| Prometheus `/metrics` | `9090` | |
| gNMI (optional) | `9339` | disabled by default, TLS off |

Ports are deliberately unprivileged so the simulator runs without root: NETCONF listens on `1830`
rather than the privileged IANA `830`, and SNMP on `1161` rather than `161`.

## Demo

The Phase 2 end-to-end check is a NETCONF round trip: connect with `ssh`, `edit-config` into the
candidate, `commit`, then `get-config` returns the committed value. The full transcript is in
[docs/protocols/NETCONF.md](docs/protocols/NETCONF.md#9-walkthrough) and the golden files replay it
in the test suite.

Phase 3 adds the confirmed commit and notifications. In the `ssh` session, subscribe to the event
stream and then watch a commit made from another session arrive as a `<notification>`:

```xml
<rpc message-id="1"><create-subscription><stream>sim-events</stream></create-subscription></rpc>
<!-- now every EventBus event is delivered, for example: -->
<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><eventTime>2024-01-02T03:04:05Z</eventTime><event xmlns="urn:sim:sim-events"><type>ConfigChanged</type><resource>device</resource><message>commit</message></event></notification>
```

A commit can also be provisional: `<commit><confirmed/><confirm-timeout>30</confirm-timeout></commit>`
applies the configuration and reverts it unless a `<commit/>` follows within 30 seconds. See
[docs/protocols/NETCONF.md §7.3](docs/protocols/NETCONF.md#73-commit).

The SNMP walk from Quick start is the Phase 1 check (`ifDescr` returns the three interfaces and
the vendor RSSI OID returns a float). Phase 4 adds the L2 checks: create a VLAN over RESTCONF and
walk the forwarding database over SNMP.

```bash
# create VLAN 200 (running datastore) and read it back
curl -s -X POST -H 'Content-Type: application/yang-data+json' \
  -d '{"sim-l2-switching:vlan":[{"id":200,"name":"VOICE"}]}' \
  http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan
curl -s http://localhost:8080/restconf/data/sim-l2-switching:vlans/vlan=200
# {"sim-l2-switching:vlan":[{"id":200,"name":"VOICE","ports":{"port":[]}}]}

# the forwarding database: the seeded entry maps 02:00:00:00:00:02 to bridge port 3
snmpwalk -v2c -c public localhost:1161 1.3.6.1.2.1.17.4.3.1.2
# SNMPv2-SMI::mib-2.17.4.3.1.2.2.0.0.0.0.2 = INTEGER: 3
```

The cross-domain scenario is the Phase 6 check: one radio failure becomes an alarm, a PTP holdover,
an SNMP trap, a NETCONF notification, a gNMI update and a metric.

```bash
snmptrapd -f -Lo -p 1162 &                                   # trap receiver
curl -X POST http://localhost:8080/api/simulate/radio-failure -d '{"link":"radio0"}'
# {"link":"radio0","state":"down","status":"ok"}

# what the receiver prints (abridged; the Opaque rendering of the vendor floats
# differs slightly between net-snmp versions — the values are the ones the PDU
# carries)
# 2026-09-22 16:10:35 localhost [UDP: [127.0.0.1]:34100->[127.0.0.1]:1162]:
#   DISMAN-EVENT-MIB::sysUpTimeInstance = Timeticks: (11107) 0:01:51.07
#   SNMPv2-MIB::snmpTrapOID.0 = OID: SNMPv2-SMI::enterprises.99999.0.1
#   SNMPv2-SMI::enterprises.99999.1.1.1.0 = Opaque: Float: -98.49   # rssi
#   SNMPv2-SMI::enterprises.99999.1.1.2.0 = Opaque: Float: -10.49   # fade-margin
#   SNMPv2-SMI::mib-2.2.2.1.2.1 = STRING: "radio0"                 # ifDescr.1

curl -s http://localhost:8080/restconf/data/sim-sync:ptp/clock/state   # holdover-in-spec
curl -s http://localhost:9090/metrics | grep 'simulator_alarms_total{severity="critical"'
# simulator_alarms_total{severity="critical",type="radio"} 1

curl -X POST http://localhost:8080/api/simulate/radio-restore -d '{"link":"radio0"}'
# the clock locks again; simRadioLinkUp and simulator_alarms_total{severity="cleared"} follow
```

A NETCONF session sees the same two transitions as `<notification>` documents. The transcript
below is a real `ssh -s netconf` session against the simulator (readability only: the server
sends one line per message, and `ssh` hides the `]]>]]>` delimiter when the session is used
interactively):

```console
$ ssh -p 1830 -s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null admin@localhost netconf
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities>
  <capability>urn:ietf:params:netconf:base:1.1</capability>
  <capability>urn:ietf:params:netconf:base:1.0</capability>
  <capability>urn:ietf:params:netconf:capability:candidate:1.0</capability>
  <capability>urn:ietf:params:netconf:capability:writable-running:1.0</capability>
  <capability>urn:ietf:params:netconf:capability:confirmed-commit:1.0</capability>
  <capability>urn:ietf:params:netconf:capability:notification:1.0</capability>
</capabilities><session-id>1</session-id></hello>

<rpc message-id="1"><create-subscription><stream>sim-events</stream></create-subscription></rpc>
<rpc-reply message-id="1"><ok/></rpc-reply>

<!-- radio0 fails, the alarm is raised, and the PTP clock loses its reference: -->
<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
  <eventTime>2026-09-22T16:10:35.600314747Z</eventTime>
  <event xmlns="urn:sim:sim-events"><type>AlarmRaised</type><resource>radio0</resource>
    <severity>critical</severity><message>radio link radio0 down: rssi -97.5 dBm</message>
    <alarm>radioLinkDown</alarm></event>
</notification>
<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0">
  <eventTime>2026-09-22T16:10:35.601412344Z</eventTime>
  <event xmlns="urn:sim:sim-events"><type>StateTransition</type><resource>ptp/clock</resource>
    <message>ptp clock: locked -&gt; holdover-in-spec (radio link radio0 down)</message>
    <from>locked</from><to>holdover-in-spec</to></event>
</notification>
```

The RSSI in the alarm text depends on what ATPC and ACM had selected before the failure, because
the fade is applied to the current transmit power and the result is clamped to the model's range.

### gNMI (optional)

gNMI is off by default. Enable it in the configuration, restart, and the same tree is available
over gRPC on `:9339`:

```yaml
gnmi:
  enabled: true
  port: 9339
```

```bash
# the model schema and the embedded YANG modules need no running simulator
go run ./cmd/simulator schema | jq '.children[].name'
go run ./cmd/simulator schema --yang --module sim-sync | head

# a real gNMI client (not installed here) reads and writes the same paths as RESTCONF
gnmic -a localhost:9339 --insecure get --path 'ptp/clock'
gnmic -a localhost:9339 --insecure set --update-path system-info/location --update-val field-site-7
curl -s http://localhost:8080/restconf/data/sim-device:system-info/location   # the same write
# {"sim-device:location":"field-site-7"}

# and streams events: fail the radio link and watch the link's leaves change
gnmic -a localhost:9339 --insecure subscribe --path 'interfaces/interface[name=radio0]/radio-link' &
curl -X POST http://localhost:8080/api/simulate/radio-failure -d '{"link":"radio0"}'
```

The gNMI service is a thin layer over the same router: leaf-level `Set`, `Get`, `Subscribe` with
`ONCE` and `STREAM`/`ON_CHANGE`, no TLS and no authentication. Its RPCs are exercised by the Go
client in `internal/gnmi/*_test.go` and by `test/integration/gnmi_test.go`, which drives the
compiled binary in a container. See [docs/protocols/gNMI.md](docs/protocols/gNMI.md) for the path
rules, the error mapping and the deliberate limits.

[docs/demo.md](docs/demo.md) walks through the whole scenario, and
[docs/cli.md](docs/cli.md) documents the commands; `scripts/demo.sh` reproduces it in one go.

## Repository layout

```
cmd/simulator/       binary entry point, delegates to internal/cli
internal/cli/        cobra commands (start, alarm inject, dump, config validate, schema, version)
internal/config/     YAML configuration, defaults and validation
internal/log/        log/slog JSON logging on stderr
internal/event/      EventBus on Go channels
internal/clock/      injectable Clock, RealClock and FakeClock
internal/store/      running / candidate / startup datastores
internal/router/     path <-> model, OID <-> path, RPC dispatch, schema tree
internal/model/      managed-object structs (path/xml/json tags)
internal/radio/      RRL domain: link budget, ATPC, ACM, alarms, failure injection
internal/l2/         L2 domain: VLAN/QinQ, MAC table, STP/RSTP, LLDP, counters, storms
internal/datatree/   data-tree read/edit engine shared by the NETCONF and RESTCONF codecs
internal/sync/       sync domain: PTP, SyncE, ESMC/SSM, holdover, cross-domain reaction
internal/{snmp,netconf,restconf,gnmi,metrics}/   management planes (gnmi optional)
internal/netconf/notif/                          RFC 5277 create-subscription and notification dispatch
internal/tools/      blank imports pinning the approved dependency stack
yang/                YANG modules (documentation only, embedded by the `yang` package)
configs/             YAML configuration
docs/                architecture, store, eventbus, config, cli, metrics, demo, ADRs, protocols
scripts/             check.sh, demo.sh
test/integration/    testcontainers-based integration tests (build tag `integration`)
testdata/            golden files (testdata/netconf/ NETCONF transcripts)
```

## Documentation

- [ROADMAP.md](ROADMAP.md) — phased plan and definition of done.
- [CONTRIBUTING.md](CONTRIBUTING.md) — branches, commit conventions, code style, tests.
- [AGENTS.md](AGENTS.md) — instructions for agentic IDEs.
- `docs/` — architecture, store, event bus, configuration, CLI, metrics, demo and ADRs.
- `docs/protocols/` — SNMP, NETCONF, RESTCONF, gNMI, PTP, SyncE, L2 and RRL references.

## License

MIT — see [LICENSE.md](LICENSE.md).
