# Configuration

The simulator reads a single YAML file. `Load` starts from the built-in defaults and only
overrides the keys present in the file, so a partial file is always valid:

```bash
go run ./cmd/simulator start --config configs/default.yaml
make run CONFIG=configs/local.yaml     # configs/local*.yaml is gitignored
```

## Fields

| Key | Type | Default | Meaning |
|---|---|---|---|
| `snmp.port` | int | `1161` | SNMP v2c agent port (community is fixed to `public`) |
| `snmp.trap-port` | int | `1162` | destination port for traps (traps only, no informs) |
| `netconf.port` | int | `1830` | SSH subsystem `netconf`; any login/password is accepted |
| `restconf.port` | int | `8080` | RESTCONF HTTP port; no authentication |
| `metrics.port` | int | `9090` | Prometheus `/metrics` endpoint |
| `gnmi.enabled` | bool | `false` | enables the optional gNMI service |
| `gnmi.port` | int | `9339` | gNMI gRPC port, used when `gnmi.enabled` is true |
| `log.level` | string | `info` | `debug`, `info`, `warn` or `error` (case-insensitive) |
| `startup.file` | string | `startup.json` | persisted startup datastore |

All ports are unprivileged so the simulator runs without root: NETCONF uses `1830` instead of the
IANA `830`, and SNMP uses `1161` instead of `161`.

## Example

`configs/default.yaml` reproduces the defaults with an explanatory comment per field:

```yaml
snmp:
  port: 1161
  trap-port: 1162
netconf:
  port: 1830
restconf:
  port: 8080
metrics:
  port: 9090
gnmi:
  enabled: false
  port: 9339
log:
  level: info
startup:
  file: startup.json
```

## Validation rules

`Config.Validate` is called by `Load` and rejects the file when:

- an unknown key is present — decoding is strict (`KnownFields`), so a typo fails at startup
  instead of being silently ignored;
- YAML is malformed, or a value has the wrong type;
- a port is outside `1–65535`;
- two enabled planes share a port. The always-on planes are SNMP, SNMP traps, NETCONF, RESTCONF
  and metrics; `gnmi.port` is checked only when `gnmi.enabled` is true;
- `log.level` is not one of `debug`, `info`, `warn`, `error`;
- `startup.file` is empty.

`Validate` never modifies its receiver, and `Load` wraps a validation failure with the file path
so the cause is visible in the startup log. A missing or unreadable file, and an empty `--config`
value, are errors as well; an empty (or comments-only) file is valid and yields the defaults.

## startup.json

`startup.file` is runtime state, not configuration: the store writes the committed configuration
there and loads it at boot (see [store.md](store.md)). The file is listed in `.gitignore` together
with `configs/local*.yaml`.

The document is versioned (`version: 1`) and each leaf stores its Go type next to the value, so an
`int` or `uint32` leaf does not turn into a `float64` after a restart; see the format and the
accepted kinds in [store.md](store.md#persistence). A missing file is not an error — the first
boot simply starts from the built-in seed.

## Notes

The configuration is intentionally flat, matching the field list in
[ROADMAP.md](../ROADMAP.md) Phase 0.4. SNMP community strings are not configurable in the MVP:
community `public` is hardcoded, and there is no trap-receiver list — `docs/protocols/SNMP.md`
documents the same flat schema.
