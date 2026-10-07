# Proposal

## Why

`modernize-repo-presentation` removes `ROADMAP.md`, and its Phase 8.2 backlog is the only record of
the defects left over from the v0.1.0 audit. That document has already proved unreliable: 15 items
sit unticked, yet 11 of them are implemented — the six P2 and four P3 items, plus `<rpc>` with an
unparsable body, which now answers `<rpc-error>` before closing the session
(`internal/netconf/rpc.go:166`). So rather than trust it, every surviving item was re-verified
against the source. Four unticked lines remain genuinely open, describing five defects (the last
line bundles `Router.List` and `getBulk`). This change exists so those five are not lost when the
file goes away.

## What Changes

Five independent fixes. Each carries the evidence that it is still open.

1. **A serve-time failure of the metrics listener is invisible.** `internal/cli/start.go:145` runs
   the Prometheus server as `go func() { _ = server.Serve(metricsListener) }()`. The bind itself is
   checked — `net.Listen` at `internal/cli/start.go:139` returns an error that fails startup — but
   once the listener is up, every later `Serve` error is discarded: no log line, no non-zero exit,
   no shutdown. Every other listener in `serveCmd` fails the process; this one silently does not.
   (The audit's phrasing implied a taken port goes unreported; it does not. The gap is narrower and
   this change fixes exactly the gap that exists.)

2. **L2 writes a multi-leaf entry without a transaction.** `internal/l2` never calls
   `Router.Transaction` (zero occurrences in the package). `writeMAC` (`internal/l2/mac.go:258`),
   `writeVLAN` (`internal/l2/vlan.go:257`) and `removeSubtree` (`internal/l2/vlan.go:303`) each
   write their leaves one at a time, and `writeMAC` then updates the entry count
   (`internal/l2/mac.go:277`). A `Router.Snapshot` read, or a `<commit>` from another plane, landing
   between two leaves observes a partially written entry — or an entry whose count disagrees with
   its rows. `Router.Transaction` and `store.Apply` were added for exactly this in the P2-4 work and
   the domain with the most runtime writes never adopted them.

3. **`/api/simulate/*` silently ignores unknown request fields.** `decodeJSON`
   (`internal/restconf/server.go:526`) decodes with `json.Decoder.Decode` and no
   `DisallowUnknownFields`, so `{"link":"radio0","stte":"down"}` returns 200 and does nothing. A
   typo is indistinguishable from success.

4. **`Router.List` discards the whole listing when one record cannot be described.**
   `internal/router/router.go:551` returns `nil, err` from inside the collect loop, so one
   unresolvable path throws away every result already gathered. This reaches users as an SNMP walk
   or a RESTCONF listing that returns nothing at all.

5. **`getBulk` clamps `max-repetitions` to 100 with no trace.** `internal/snmp/agent.go:243` caps
   repetitions at `maxBulkRepetitions` (`internal/snmp/agent.go:21`). The cap itself is legitimate —
   RFC 3416 §4.2.3 permits a responder to return fewer repetitions than requested, including to
   bound processing time — but it is documented nowhere (`docs/protocols/SNMP.md` does not mention
   it) and is not covered by a test, so a manager requesting 500 repetitions gets 100 and no way to
   learn the limit exists. This item is a transparency defect, not a conformance defect.

## Capabilities

### New Capabilities

None. **Recorded shortcut:** these five fixes do change observable behaviour, so they would
normally carry spec deltas, and `openspec validate` would reject a zero-delta change without a
marker. `openspec/specs/` is empty and the capability taxonomy is a separate, still-open question,
so this change sets `skip_specs: true` instead of inventing five thin capabilities that later work
would have to reorganise. Whoever picks this change up should write the deltas and drop the flag
before implementing.

### Modified Capabilities

None. `openspec/specs/` is empty.

## Impact

- **Code:** `internal/cli/start.go`, `internal/l2/{mac,vlan}.go`, `internal/restconf/server.go`,
  `internal/router/router.go`, `internal/snmp/agent.go`. Five files, five localised fixes.
- **Docs:** `docs/metrics.md`, `docs/protocols/SNMP.md`, `docs/protocols/RESTCONF.md`, and the
  `internal/l2` note in `AGENTS.md` if the transaction boundary changes the documented write path.
- **Behaviour:** three user-visible changes — a bad `/api/simulate/*` field becomes a 400, a listing
  with one bad record returns the rest, and a metrics-port failure becomes visible. Two are
  internal (atomicity, a documented limit).
- **Tests:** one regression test per fix, plus a concurrency test for the L2 transaction boundary.
- **Dependencies:** none.
- **Compatibility:** `Router.Transaction` is documented as non-reentrant, so the L2 change must not
  nest transactions; see `design.md`.
