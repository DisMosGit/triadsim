# NETCONF

**Protocol:** Network Configuration Protocol (RFC 6241) over SSH (RFC 6242)
**Transport:** SSH subsystem `netconf`
**Default port:** `1830/TCP` (see [Ports](#ports))
**Capabilities:** `base:1.0`, `base:1.1`, `candidate:1.0`, `writable-running:1.0`,
`confirmed-commit:1.0`, `notification:1.0`

## 1. Scope

TriadSim implements the NETCONF base with the candidate datastore, the confirmed commit
(RFC 4741 §8.4) and event notifications (RFC 5277). This document describes exactly what the
server does; the tables below are the contract the golden tests in `testdata/netconf/` verify.

| Operation | Status | Notes |
|---|---|---|
| `get-config` | implemented | `running`, `candidate`, `startup`; subtree filter |
| `edit-config` | implemented | `merge`, `replace`, `create`, `delete`, `remove`; target `running` or `candidate` |
| `commit` | implemented | validates, applies candidate to running, persists `startup.json`, publishes `ConfigChanged` |
| `commit` with `<confirmed/>` | implemented | `<confirm-timeout>` (default 600 s) reverts running unless a confirming commit arrives — see [§7.3](#73-commit) |
| `discard-changes` | implemented | restores candidate from running |
| `close-session` | implemented | replies `<ok/>` and ends the session |
| `create-subscription` | implemented | one stream, `sim-events` — see [§7.6](#76-create-subscription) |
| notifications | implemented | a `<notification>` per EventBus event — see [§7.7](#77-notifications) |
| `commit` with `<persist>`, `<persist-id>` | not implemented | `operation-not-supported`; the server advertises `:confirmed-commit:1.0`, not `1.1` |
| `cancel-commit` | not implemented | `operation-not-supported` |
| `get` | not implemented | state data is exposed over SNMP only |
| `copy-config`, `delete-config`, `lock`, `unlock`, `kill-session`, `validate` | not implemented | `operation-not-supported` |
| XPath filters | not implemented | `operation-not-supported`; only `type="subtree"` |

`get-config` returns configuration data only: leaves tagged `config:"false"` in the model
(measured levels, counters, uptime) are state, not configuration, and are never returned.

## 2. Ports

| Port | Use |
|---|---|
| `1830` | SSH subsystem `netconf` (default in `configs/default.yaml`) |

The IANA port `830` is privileged: binding it needs `root` or `CAP_NET_BIND_SERVICE`, which would
break `make run` for a normal user. TriadSim therefore uses the unprivileged `1830`, exactly as
the SNMP agent uses `1161` instead of `161`. Set `netconf.port` in the YAML configuration to use
another port.

## 3. SSH subsystem

`internal/netconf/ssh.go` serves the subsystem with `golang.org/x/crypto/ssh`:

- **No authentication.** Any user, password, public key or keyboard-interactive exchange is
  accepted (`NoClientAuth`, plus permissive `password`/`publickey`/`keyboard-interactive`
  callbacks). This is deliberate; see `AGENTS.md` (no auth anywhere).
- **Ephemeral host key.** An ed25519 host key is generated at startup and never persisted, so
  clients see a new key after every restart. Use `-o StrictHostKeyChecking=no
  -o UserKnownHostsFile=/dev/null` in scripts.
- **One session channel per connection.** Only `session` channels are accepted, and only the
  `netconf` subsystem; any other channel type or subsystem is rejected.

Connect with the subsystem name as the remote command (OpenSSH requires this position):

```bash
ssh -p 1830 -s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null admin@localhost netconf
```

## 4. Hello and capabilities

The server sends its `<hello>` as soon as the subsystem starts, always with end-of-message
framing, because framing is negotiated afterwards:

```xml
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <capabilities>
    <capability>urn:ietf:params:netconf:base:1.1</capability>
    <capability>urn:ietf:params:netconf:base:1.0</capability>
    <capability>urn:ietf:params:netconf:capability:candidate:1.0</capability>
    <capability>urn:ietf:params:netconf:capability:writable-running:1.0</capability>
    <capability>urn:ietf:params:netconf:capability:confirmed-commit:1.0</capability>
    <capability>urn:ietf:params:netconf:capability:notification:1.0</capability>
  </capabilities>
  <session-id>1</session-id>
</hello>
```

- Only implemented capabilities are advertised. `confirmed-commit:1.0` is the RFC 4741 version:
  `<confirmed/>` and `<confirm-timeout>` without `<persist>`, `<persist-id>` or
  `<cancel-commit>`, which belong to `:confirmed-commit:1.1` and are rejected with
  `operation-not-supported`.
- `notification:1.0` makes `create-subscription` available on every session; the simulator
  exposes one stream and no replay, so the capability carries no `?streams=` parameter.
- `session-id` is a monotonic counter starting at `1` for the first session of a run.
- The client `<hello>` must advertise `base:1.0` or `base:1.1`; otherwise the server closes the
  session.
- **Framing negotiation:** if the client advertises `base:1.1` (and this server always does), the
  rest of the session uses chunked framing. Otherwise end-of-message framing is used.
- The client hello is sniffed: a client that starts with a chunk header (`#`) instead of `<` is
  understood, even though RFC 6242 has both hellos use end-of-message framing.

## 5. Framing (RFC 6242 §4)

Both mechanisms are implemented in `internal/netconf/framing.go`.

**End-of-message** (`base:1.0`): every message ends with the delimiter `]]>]]>`. The message
itself must not contain that sequence.

```
<rpc message-id="1"><get-config><source><running/></source></get-config></rpc>]]>]]>
```

**Chunked** (`base:1.1`): a message is one or more chunks followed by end-of-chunks. The chunk
size is **hexadecimal**:

```
chunk           = "\n" "#" chunk-size "\n" chunk-data
end-of-chunks   = "\n" "##" "\n"
```

The message `<rpc/>` is therefore sent as (the first byte is the LF that precedes the chunk
header):

```

#6
<rpc/>
##
```

One message is limited to 16 MiB: a message that exceeds the limit is answered with `too-big` and
ends the session. Broken framing is answered with `malformed-message` before the session ends.

## 6. Data model

The XML data tree mirrors the runtime managed-object model: an element name is a router path
segment, and nesting is path nesting. The data root is the device, so `<config>` and `<data>`
contain the top-level containers `system-info` and `interfaces`.

| XML | Router path |
|---|---|
| `<system-info>` | `system-info` |
| `<system-info><device-id>` | `system-info/device-id` |
| `<interfaces><interface><name>radio0</name>` | `interfaces/interface[name=radio0]` |
| `<interfaces><interface><name>radio0</name><radio-link><tx-power>` | `interfaces/interface[name=radio0]/radio-link/tx-power` |
| `…<modulation-profile><id>5</id>` | `…/radio-link/modulation-profile[id=5]` |

Rules:

- **Lists** carry their key as a child leaf, and the key leaf is always encoded first
  (`<interface><name>radio0</name>…`). A list entry in `<config>` must contain its key; a
  missing key is `missing-element`.
- **Namespaces** are ignored when parsing: elements are matched by local name. On output each
  module subtree declares its namespace once: `urn:sim:device` for `system-info`, `interfaces`,
  `interface` and `counters`, `urn:sim:radio-link` for `radio-link`, `modulation-profile` and
  everything below them. The envelope uses `urn:ietf:params:xml:ns:netconf:base:1.0`.
- **Values** are XML character data: booleans are `true`/`false`, numbers are decimal, a
  `float64` leaf is printed with the shortest exact representation (`22.5`, `1500`). A value that
  does not fit its model type is `invalid-value`.
- The device topology is fixed by the model template (`radio0`, `eth0`, `eth1`, 12 modulation
  profiles). Unknown interfaces, unknown nodes and `radio-link` under a non-radio interface are
  `unknown-element`.

## 7. Operations

### 7.1. get-config

```xml
<rpc message-id="3" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <get-config>
    <source><running/></source>
    <filter type="subtree">
      <interfaces>
        <interface>
          <name>radio0</name>
          <radio-link><tx-power/></radio-link>
        </interface>
      </interfaces>
    </filter>
  </get-config>
</rpc>
```

Reply (end-of-message framing shown):

```xml
<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="3">
  <data>
    <interfaces xmlns="urn:sim:device">
      <interface>
        <name>radio0</name>
        <radio-link xmlns="urn:sim:radio-link"><tx-power>21.5</tx-power></radio-link>
      </interface>
    </interfaces>
  </data>
</rpc-reply>]]>]]>
```

- `<source>` is mandatory and accepts `<running/>`, `<candidate/>` or `<startup/>`. `startup`
  reflects the last committed and persisted configuration.
- `<filter>` is optional. Without it the whole configuration is returned, in model declaration
  order (key leaves first, list entries ordered by key, numerically for numeric keys).
- Simplified **subtree filter** semantics:
  - a container element selects its whole subtree, or only the children it names;
  - a list element with a key child leaf selects that entry (content match); without a key it
    selects every entry;
  - a leaf element without text selects the leaf; with text it also requires that value;
  - an element name that is not in the model is `unknown-element`.
- No match yields an empty `<data/>`.
- `type="xpath"` is rejected with `operation-not-supported`.

### 7.2. edit-config

```xml
<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0">
  <edit-config>
    <target><candidate/></target>
    <config>
      <interfaces>
        <interface>
          <name>radio0</name>
          <radio-link>
            <tx-power>22.5</tx-power>
            <atpc><target-rsl>-50</target-rsl></atpc>
          </radio-link>
        </interface>
      </interfaces>
    </config>
  </edit-config>
</rpc>
```

- `<target>` is mandatory: `running` (the `writable-running` capability) or `candidate`.
  `startup` is rejected — a commit is what persists startup.
- `<default-operation>`: `merge` (default), `replace` or `none`.
- Operation attributes are matched by local name, so both `operation="merge"` and
  `nc:operation="merge"` work:

| Operation | Container or list entry | Leaf |
|---|---|---|
| `merge` (default) | children are merged | value is set |
| `replace` | the subtree is cleared first, then the children are written | value is set |
| `create` | `data-exists` when the subtree already holds configuration data | `data-exists` when the leaf exists |
| `delete` | clears the subtree, `data-missing` when it is empty | `data-missing` when the leaf is absent |
| `remove` | clears the subtree, silent when it is empty | silent when the leaf is absent |

- `test-option` accepts `test-then-set` (default) and `set`; `test-only` is
  `operation-not-supported`. `error-option` accepts `stop-on-error` (default);
  `continue-on-error` and `rollback-on-error` are `operation-not-supported`.
- **Whole-edit validation.** The edits are applied to a snapshot of the target datastore, the
  snapshot is validated with the same validator `commit` uses, and only then is the difference
  written back. A rejected edit — an out-of-range value, a missing mandatory sibling — returns
  `invalid-value` and leaves the datastore untouched, so `discard-changes` is never needed to
  undo a failed `edit-config`.
- **State leaves are protected.** A write to a leaf tagged `config:"false"` is `access-denied`,
  and a subtree `replace`/`delete` never removes state leaves, so counters and measured levels
  survive any configuration edit.
- Writing to `running` takes effect immediately but is not persisted until a `commit` (the
  simulator has no separate `startup` write operation in this phase).

### 7.3. commit

```xml
<rpc message-id="2" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><commit/></rpc>
```

`commit` validates the candidate, applies it to running, writes `startup.json`, and publishes a
`ConfigChanged` event on the event bus. `<source><candidate/></source>` is accepted explicitly;
other sources are `operation-not-supported`. A candidate the model rejects is `invalid-value`, and
the running datastore, the startup datastore and the file then stay untouched.

**Confirmed commit** (`:confirmed-commit:1.0`, RFC 4741 §8.4.1):

```xml
<rpc message-id="3" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><commit><confirmed/><confirm-timeout>30</confirm-timeout></commit></rpc>
```

- `<confirmed/>` applies the candidate exactly like a plain commit, but the change is reverted if
  no confirming commit arrives within `<confirm-timeout>` seconds (default **600**).
  `<confirm-timeout>` without `<confirmed/>` is `missing-element`, and a non-positive or
  non-numeric timeout is `invalid-value`.
- **Confirming commit:** any later successful `<commit/>` — with or without `<confirmed/>`,
  from any session — confirms the change: the timer is cancelled and the configuration stays.
  A follow-up `<commit><confirmed/></commit>` from the **same** session may add changes, keeps the
  rollback target of the first confirmed commit and only restarts the timer.
- **Rollback:** the server snapshots running before the commit and, when the timeout expires or
  the session that issued the confirmed commit ends (including `close-session` and server
  shutdown), restores running and `startup.json` from that snapshot and publishes `ConfigChanged`
  with the reason `confirmed-commit rollback`. The candidate is **not** rolled back: the
  uncommitted configuration stays visible and can be re-committed after `discard-changes`.
- A confirmed commit from another session while one is in progress is `access-denied`: the
  simulator can only revert one unconfirmed configuration. Because the candidate is shared, this
  is the equivalent of the locking RFC 6241 suggests for confirmed commits.
- `<persist>`, `<persist-id>` (both `:confirmed-commit:1.1`) and the `<cancel-commit>` operation
  are `operation-not-supported`. To cancel a confirmed commit before the timeout, wait for it or
  end the session.

The golden transcript `testdata/netconf/confirmed-commit.xml` records the full dialog: edit
candidate → confirmed commit → `get-config` sees the new value → confirming commit → the value
stays.

### 7.4. discard-changes

```xml
<rpc message-id="4" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><discard-changes/></rpc>
```

Copies running back into candidate, discarding every uncommitted change.

### 7.5. close-session

Replies `<ok/>` and closes the SSH session channel. The session's notification subscription ends
with it (RFC 5277 §2.3), and a confirmed commit it issued but never confirmed is reverted. There
is no explicit unsubscribe or `kill-session`; sessions end when the client disconnects.

### 7.6. create-subscription

```xml
<rpc message-id="5" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><create-subscription xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><stream>sim-events</stream></create-subscription></rpc>
```

`create-subscription` (RFC 5277) replies `<ok/>` and from then on the session receives
notifications asynchronously, while it remains able to send RPCs.

| Parameter | Behaviour |
|---|---|
| `<stream>` | optional; only `sim-events` is accepted. An absent `<stream>` selects it, an unknown name is `invalid-value` |
| `<filter>` | not implemented → `operation-not-supported` |
| `<startTime>`, `<stopTime>` | replay is not implemented → `operation-not-supported` |
| other children | `unknown-element` |

A second `create-subscription` in the same session replaces the previous subscription. The
subscription ends when the session ends; there is no unsubscribe RPC in this phase.

### 7.7. Notifications

An event notification is a complete XML document (RFC 5277 §2.2.1), framed with the session's
negotiated framing (end-of-message for `base:1.0`, chunked for `base:1.1`):

```xml
<notification xmlns="urn:ietf:params:xml:ns:netconf:notification:1.0"><eventTime>2024-01-02T03:04:05Z</eventTime><event xmlns="urn:sim:sim-events"><type>AlarmRaised</type><resource>radio0</resource><severity>major</severity><message>radio link down</message></event></notification>
```

- `eventTime` is the event's timestamp in RFC 3339 UTC; it is set by the publishing domain, or by
  the event bus when the publisher left it zero.
- `<event>` mirrors `event.Event`: `type` (`AlarmRaised`, `AlarmCleared`, `ConfigChanged`,
  `StateTransition`), `resource`, optional `severity` and optional `message`. The same payload is
  what a RESTCONF `sim-events:event` stream would carry; RESTCONF event streams are not
  implemented (Phase 4 answers `/restconf/streams` with `501 operation-not-supported`).
- Every EventBus event is rendered once and queued for every subscribed session. A session that
  does not read quickly enough loses notifications and the drop is logged (the event bus already
  drops for slow subscribers); one slow client never delays another.
- Events published before `create-subscription` are not replayed, and there is no notification
  log. Notifications are not answered: the client must not reply to them.
- A committing session also receives its own `ConfigChanged` notification if it is subscribed.

## 8. Errors

Failures are reported as `<rpc-error>` inside the reply:

```xml
<rpc-reply xmlns="urn:ietf:params:xml:ns:netconf:base:1.0" message-id="4">
  <rpc-error>
    <error-type>protocol</error-type>
    <error-tag>invalid-value</error-tag>
    <error-severity>error</error-severity>
    <error-message>interfaces/interface[name=radio0]/radio-link/tx-power: expected a number, got "high"</error-message>
    <error-path>interfaces/interface[name=radio0]/radio-link/tx-power</error-path>
  </rpc-error>
</rpc-reply>
```

Per-node failures — a read-only write, `data-exists`, `data-missing`, an unparsable value or an
unknown node below a list entry — carry `error-path` with the router path of the offending node.
A whole-edit validation failure (an out-of-range value the model rejects, a missing mandatory
sibling) reports `invalid-value` with the validator's message and no path.

| Situation | `error-type` | `error-tag` |
|---|---|---|
| Malformed XML, broken framing, oversized message | `rpc` | `malformed-message`, `too-big` |
| Missing `message-id` | `rpc` | `missing-attribute` |
| Missing `<source>`, `<target>`, `<config>` or a list key | `protocol` | `missing-element` |
| `<confirm-timeout>` without `<confirmed/>` | `protocol` | `missing-element` |
| Element or list key not in the data model, unknown `create-subscription` parameter | `protocol` | `unknown-element` |
| Value not parseable, or rejected by the model; unknown event stream; non-positive `<confirm-timeout>` | `protocol` | `invalid-value` |
| Write to a `config:"false"` node | `protocol` | `access-denied` |
| `create` where configuration data exists | `protocol` | `data-exists` |
| `delete` where configuration data is missing | `protocol` | `data-missing` |
| Confirmed commit while another session owns one | `protocol` | `access-denied` |
| Unknown operation, unsupported option, XPath filter, `<persist>`/`<persist-id>`, `cancel-commit`, notification filter or replay | `protocol` | `operation-not-supported` |
| Store or persistence failure | `application` | `operation-failed` |

A message that cannot be parsed — malformed XML, a missing `message-id`, or a root element that
is not `<rpc>` — is answered with the error and then ends the session, as RFC 6241 §7.1 requires.
Other errors leave the session open.

## 9. Walkthrough

The transcript below is an actual session, recorded (with the response omitted for brevity) in
`testdata/netconf/edit-config.xml` and replayed by the golden test:

```
$ ssh -p 1830 -s -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null admin@localhost netconf
<hello xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><capabilities><capability>urn:ietf:params:netconf:base:1.0</capability></capabilities><session-id>7</session-id></hello>]]>]]>
<rpc message-id="1" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><edit-config><target><candidate/></target><config><interfaces><interface><name>radio0</name><radio-link><tx-power>22.5</tx-power><atpc><target-rsl>-50</target-rsl></atpc><acm><min-profile>2</min-profile></acm></radio-link></interface></interfaces></config></edit-config></rpc>]]>]]>
<rpc message-id="2" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><commit/></rpc>]]>]]>
<rpc message-id="3" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><get-config><source><running/></source><filter><interfaces><interface><name>radio0</name><radio-link><tx-power/><atpc><target-rsl/></atpc><acm><min-profile/></acm></radio-link></interface></interfaces></filter></get-config></rpc>]]>]]>
<rpc message-id="4" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><edit-config><target><candidate/></target><config><interfaces><interface><name>radio0</name><radio-link><tx-power>999</tx-power></radio-link></interface></interfaces></config></edit-config></rpc>]]>]]>
<rpc message-id="5" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><get-config><source><candidate/></source><filter><interfaces><interface><name>radio0</name><radio-link><tx-power/></radio-link></interface></interfaces></filter></get-config></rpc>]]>]]>
<rpc message-id="6" xmlns="urn:ietf:params:xml:ns:netconf:base:1.0"><close-session/></rpc>]]>]]>
```

The server answers with its hello, `<ok/>` for RPC 1 and 2, the committed `22.5` for RPC 3, an
`invalid-value` error for RPC 4, the unchanged `22.5` for RPC 5 and `<ok/>` for RPC 6. The full
responses are in `testdata/netconf/edit-config.golden.xml`.

The confirmed-commit dialog is recorded the same way in
`testdata/netconf/confirmed-commit.xml`: the confirmed commit applies `27.5`, `get-config`
returns it, the confirming commit keeps it. The rollback itself needs the confirm timeout to
expire, so it is covered by the `FakeClock` tests rather than by a transcript.

Sessions share one candidate datastore and there is no `lock`/`unlock` in this phase: with
concurrent sessions the last write wins, and only one confirmed commit can be in progress
(another session's confirmed commit is `access-denied`).

## 10. Tests

Golden transcripts live in `testdata/netconf/`:

| File | Contents |
|---|---|
| `edit-config.xml` / `.golden.xml` | hello, edit-config, commit, get-config round trip, rejected invalid-value edit |
| `get-config.xml` / `.golden.xml` | hello, subtree filters, namespace handling, XPath rejection, `startup` source |
| `confirmed-commit.xml` / `.golden.xml` | hello, confirmed commit with `<confirm-timeout>`, get-config, confirming commit |

They are replayed through the real session code and framing by `internal/netconf/golden_test.go`.
Regenerate them after an intentional change and review the diff:

```bash
go test ./internal/netconf -update
```

Unit and session tests cover the SSH subsystem, hello negotiation, both framing mechanisms, RPC
parsing, all five edit operations, validation, error mapping and the event publication; the
confirmed-commit tests drive the rollback with the `internal/clock` `FakeClock`, and the
notification tests assert delivery in both framing modes.

## 11. References

| Document | Title |
|---|---|
| RFC 4741 | NETCONF Configuration Protocol (:confirmed-commit:1.0, §8.4) |
| RFC 6241 | Network Configuration Protocol (NETCONF) |
| RFC 6242 | Using the NETCONF Protocol over Secure Shell (SSH) |
| RFC 5277 | NETCONF Event Notifications |
| RFC 7950 | The YANG 1.1 Data Modeling Language (list key encoding, XML representation) |
