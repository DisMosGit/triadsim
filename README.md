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

> **Status: Phase 4 — RESTCONF and L2 switching.** The repository builds, tests and starts; the
> foundations, managed-object models (radio, L2, sync, device), the running/candidate/startup
> store, the router, the SNMP v2c agent with the Prometheus endpoint, the NETCONF subsystem
> (`get-config`/`edit-config`/`commit`/`discard-changes`, confirmed commit with rollback,
> `create-subscription` notifications), the chi-based RESTCONF server and the L2 domain (VLAN and
> QinQ, MAC forwarding database, simplified STP/RSTP, LLDP, counters, broadcast-storm simulation)
> are in place. The radio and sync domain logic lands in Phases 5–7; see [ROADMAP.md](ROADMAP.md).

## Quick start

Requirements: Go 1.27+ (Docker is optional and only needed for integration tests in later phases).

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
```

Configuration lives in [`configs/default.yaml`](configs/default.yaml); every field has a
documented default and is described in [docs/config.md](docs/config.md).

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

The cross-domain scenario — radio failure → PTP holdover → SNMP trap + NETCONF notification +
metric — is described in `.docs/desicion.md` §3.6 and will be reproduced by `scripts/demo.sh`.

## Repository layout

```
cmd/simulator/       binary entry point, delegates to internal/cli
internal/cli/        cobra commands (start, ...)
internal/config/     YAML configuration, defaults and validation
internal/log/        log/slog JSON logging on stderr
internal/event/      EventBus on Go channels
internal/clock/      injectable Clock, RealClock and FakeClock
internal/store/      running / candidate / startup datastores
internal/router/     path <-> model, OID <-> path, RPC dispatch
internal/model/      managed-object structs (path/xml/json tags)
internal/radio/      RRL domain: link budget, ATPC, ACM, alarms
internal/l2/         L2 domain: VLAN/QinQ, MAC table, STP/RSTP, LLDP, counters, storms
internal/datatree/   data-tree read/edit engine shared by the NETCONF and RESTCONF codecs
internal/sync/       sync domain: PTP, SyncE, ESMC/SSM, holdover
internal/{snmp,netconf,restconf,gnmi,metrics}/   management planes
internal/netconf/notif/                          RFC 5277 create-subscription and notification dispatch
internal/tools/      blank imports pinning the approved dependency stack
configs/             YAML configuration
docs/                architecture, store, eventbus, config, ADRs, protocols
scripts/             check.sh, demo.sh
test/integration/    testcontainers-based integration tests (build tag `integration`)
testdata/            golden files (testdata/netconf/ NETCONF transcripts)
yang/                YANG modules (documentation only, embedded)
```

## Documentation

- [ROADMAP.md](ROADMAP.md) — phased plan and definition of done.
- [CONTRIBUTING.md](CONTRIBUTING.md) — branches, commit conventions, code style, tests.
- [AGENTS.md](AGENTS.md) — instructions for agentic IDEs.
- `docs/` — architecture, store, event bus, configuration and ADRs.
- `docs/protocols/` — SNMP, NETCONF, RESTCONF, PTP, SyncE, L2, RRL references.

## License

MIT — see [LICENSE.md](LICENSE.md).
