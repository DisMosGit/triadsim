# Documentation

Index of the TriadSim documentation. Start with the architecture, then the subsystem you need.

## Architecture and subsystems

| Document | Contents |
|---|---|
| [architecture.md](architecture.md) | Modular monolith, package responsibilities, dependency rules, data flow, startup |
| [store.md](store.md) | running / candidate / startup, the `Store` contract, diff/commit/rollback, persistence |
| [eventbus.md](eventbus.md) | Event types, structured fields, publish/subscribe semantics, buffering and drop policy, lifecycle |
| [config.md](config.md) | YAML reference, defaults, validation rules, `startup.json` |
| [cli.md](cli.md) | `start`, `alarm inject`, `dump`, `config validate`, `schema`, `version` and their flags |
| [metrics.md](metrics.md) | Prometheus metric names, labels and examples |
| [demo.md](demo.md) | The cross-domain scenario step by step |

## Protocol references

| Document | Contents |
|---|---|
| [protocols/SNMP.md](protocols/SNMP.md) | SNMP v2c, community `public`, OID tree, vendor OIDs, traps, examples |
| [protocols/NETCONF.md](protocols/NETCONF.md) | SSH subsystem, hello, EOM/chunked framing, `get-config`/`edit-config`/`commit`/`discard-changes`, errors, walkthrough |
| [protocols/RESTCONF.md](protocols/RESTCONF.md) | URL structure, media types, methods, error codes, `curl` examples |
| [protocols/RADIO-RRL.md](protocols/RADIO-RRL.md) | RRL link budget, RSSI, ATPC, ACM, modulation profiles, fade margin, alarms |
| [protocols/L2.md](protocols/L2.md) | 802.1Q, QinQ, MAC table, STP/RSTP (simplified), LLDP, counters, MIB mapping |
| [protocols/PTP.md](protocols/PTP.md) | IEEE 1588, PTP state machine, domain, master/holdover/freerun, simplifications |
| [protocols/SYNCE.md](protocols/SYNCE.md) | SyncE, ESMC/SSM, quality levels, relation to PTP, simplifications |
| [protocols/gNMI.md](protocols/gNMI.md) | gNMI paths, encodings, Capabilities/Get/Set/Subscribe, events, errors, limits |

## Decisions

| Document | Contents |
|---|---|
| [adr/0001-record-architecture-decisions.md](adr/0001-record-architecture-decisions.md) | How and when architecture decisions are recorded |
| [adr/0002-model-vs-yang.md](adr/0002-model-vs-yang.md) | Go structs as the model, YANG as embedded documentation |
| [adr/0003-modular-monolith.md](adr/0003-modular-monolith.md) | One binary, one store and one event bus instead of services |
| [adr/0004-gosnmp-ssh-xml.md](adr/0004-gosnmp-ssh-xml.md) | The protocol stack: gosnmp, x/crypto/ssh, chi, gRPC + openconfig/gnmi |
| [adr/0005-running-write-through.md](adr/0005-running-write-through.md) | Writes to running are mirrored into candidate, so a commit cannot revert them |
| [adr/template.md](adr/template.md) | ADR template |

## Planned

Not written yet; they arrive with the phases that produce their content:
`testing.md` (test strategy), `glossary.md` (RRL/PTP/QinQ terms) and `faq.md`.

Higher-level project documents live in the repository root: [README.md](../README.md),
[ROADMAP.md](../ROADMAP.md), [CONTRIBUTING.md](../CONTRIBUTING.md) and [AGENTS.md](../AGENTS.md).
