# Design

## Context

See `proposal.md` - Why for the five defects and their evidence. Facts that shape the approach, all
verified in source:

- `Router.Transaction` (`internal/router/router.go:493`) serializes read-modify-write edits and its
  doc comment states plainly: *"fn must not start a nested transaction: txMu is not reentrant."*
  `Router.Apply` (`internal/router/router.go:499`) applies one batch atomically and checks the
  context once, so the caller must assemble a complete batch before calling it.
- `writeMAC` (`internal/l2/mac.go:258`) calls `m.router.Set` once per leaf — four fields, then the
  age via `state`, then `syncCount` — so one entry costs six separate store writes, each mirrored
  into candidate.
- Every `internal/l2` public method takes `m.mu` and calls those helpers *while holding it*. The
  package has no `Router.Transaction` call at all.
- `Router.List` (`internal/router/router.go:526`) returns `([]Result, error)` and has exactly two
  callers: `Router.Dispatch` (`internal/router/router.go:629`) and `removeSubtree`
  (`internal/l2/vlan.go:304`). The SNMP walk does not use it — it reads the cached object index.
- `Router` carries no logger, so a new diagnostic cannot simply be logged from inside it.
- `serveCmd` already reports a metrics **bind** failure (`internal/cli/start.go:139-142` wraps
  `net.Listen` in an error return). Only the post-bind `server.Serve` error is discarded.

## Goals / Non-Goals

**Goals:**
- Each defect fixed at the layer that owns it, with a regression test that fails before the fix.
- The L2 fix must not introduce a lock-order inversion, and the reasoning must be written down,
  because the failure mode is a deadlock that only shows up under concurrency.

**Non-Goals:**
- Retrofitting `Router.Transaction` across every `internal/l2` write path. Only the helpers named in
  the proposal change; spreading it further is separate work with its own risk.
- Adding a logger to `Router`, or widening the SNMP `max-repetitions` cap into configuration.
- Making RESTCONF request bodies strict in general. Only the simulation endpoints are in scope.

## Decisions

**1. Metrics: surface the post-bind `Serve` error by shutting the simulator down.**
The goroutine becomes: on a non-`http.ErrServerClosed` error, log at `Error` and cancel the root
context so the process exits non-zero like every other listener failure. Alternatives: (a) log and
keep running - rejected, because a simulator that silently lost its metrics endpoint is the state
the audit complained about; (b) leave the bind check as the only guard - rejected, because it does
not cover a listener that dies later. Note the proposal's framing correction: a taken port is
*already* reported by `net.Listen`, so the fix is strictly about serve-time failures.

**2. L2: one atomic batch, inside a transaction taken at the public boundary.**
Two changes to `writeMAC` / `writeVLAN` / `removeSubtree`:

- Assemble the leaves into a `map[string]any` and submit them with a single `Router.Apply` instead
  of one `router.Set` per leaf. This is what makes the entry atomic in the store, and it collapses
  six store writes (each mirrored into candidate) into one.
- Take `Router.Transaction` in the *public* method — `learn`, `SetStatic`, `DeleteMAC`,
  `FlushMAC`, `SetVLAN`, and the `Delete*` counterparts — **outside** `m.mu`, so the lock order is
  always `txMu` then `m.mu`.

Alternatives considered: wrapping inside the helpers (rejected — they already run under `m.mu`, so
the order would become `m.mu` then `txMu`; nothing violates that today, but any future path that
takes `txMu` and then calls into L2 would deadlock, and `Router.Transaction`'s own doc comment warns
against nesting); relying on `Apply` alone (rejected — it makes the *write* atomic but leaves the
read-decide-write window open, which is the race the transaction exists to close).

**3. L2 deletion enumerates paths, not described records.**
`removeSubtree` uses `Router.List` purely to learn which paths to delete, but `List` resolves each
path against the model. Those are different needs, and conflating them is why decision 4 alone
would be unsafe: a record `List` skips is a leaf `removeSubtree` silently leaves behind. The fix
enumerates the path set without describing it (the store already exposes path listing — see
`internal/cli/start.go:280`), so deleting a subtree stays complete even when a path no longer
resolves. Decision 4 then only affects readers.

**4. `Router.List` skips what it cannot describe and returns the rest.**
Replace `return nil, err` inside the collect loop (`internal/router/router.go:551`) with a skip, so
one unresolvable record no longer discards the whole listing. Skips are reported to the caller
rather than swallowed, since `Router` has no logger and the audit's complaint about `getBulk` is
precisely that silence is worse than a limit. Concretely: return the skipped paths alongside the
results (two callers, so the signature change is cheap) and let each caller decide — `Dispatch`
proceeds, and any caller that cannot tolerate a gap can fail. Do not invent a logger field on
`Router` for this.

**5. `getBulk`: document and test the cap, do not remove it.**
RFC 3416 §4.2.3 clause (3) allows a responder to return fewer repetitions than requested when the
request would take significantly more processing time, and clause (1) allows a lesser number to
respect a local constraint — so `maxBulkRepetitions = 100` is a legitimate resource guard, not a
bug. The defect is that it is undiscoverable. Fix: state the cap in `docs/protocols/SNMP.md` and add
a test asserting that a request for 500 repetitions answers `noError` with exactly 100. Alternatives:
making the cap configurable (rejected — no evidence anyone needs it, and it adds config surface to a
simulator); answering an error status (rejected — the response is well-formed, and `tooBig` would
misreport the reason).

**6. Strict decoding is scoped to the simulation endpoints.**
`Server.decodeJSON` (`internal/restconf/server.go:526`) gains `DisallowUnknownFields`, so a typo in
`/api/simulate/*` becomes a 400 instead of a silent 200. The datastore-operation decoder
`ops.decodeJSON` (`internal/restconf/ops/codec_json.go:44`) is deliberately left alone: those bodies
are structured payloads where a stricter rule is a separate question with its own compatibility
surface.

## Risks / Trade-offs

- **L2 deadlock** if a future path takes `txMu` and then calls an L2 method -> mitigation: the order
  `txMu` then `m.mu` is documented in the code comment added by the change, and decision 3 keeps
  `removeSubtree` off the transaction where it does not need one.
- **Holding `txMu` across a whole L2 read-modify-write widens contention** with NETCONF/RESTCONF
  edits -> accepted: the critical section is a map build plus one `Apply`, which is strictly shorter
  than the six sequential store writes it replaces.
- **Stricter `/api/simulate/*` bodies reject requests that used to succeed** -> intended, and it is
  the reported defect; `docs/protocols/RESTCONF.md` records it.
- **A changed `Router.List` signature touches two call sites** -> accepted as cheaper than a silent
  skip; both call sites are updated in the same commit.
- **The `getBulk` item closes as documentation rather than a behaviour change** -> this is a
  deliberate de-escalation, recorded here so it does not look like the item was quietly dropped.
- **Spec deltas are deferred** (`skip_specs` in this change's `.openspec.yaml`) -> accepted by the
  maintainer; see the proposal's recorded shortcut.

## Migration Plan

Five independent fixes; each is a self-contained commit with its own regression test, so the change
can land in any order and be reverted in pieces. Suggested order: the three that need no shared
machinery first (metrics, strict decoding, `getBulk` documentation), then `Router.List` with its
signature change, then the L2 transaction work last, since it is the only one with a concurrency
risk and benefits from the `List` fix already being in.

**Rollback:** revert the individual commit. No data migration, no configuration change, no
dependency change.
