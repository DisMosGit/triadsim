# Tasks

> Before implementing: this change sets `skip_specs: true` as a recorded shortcut (see
> `proposal.md` - Capabilities). Write the spec deltas for the five behaviours and drop the flag
> first if the capability taxonomy has been decided by then.

## 1. Surface a metrics listener failure

- [ ] 1.1 In `internal/cli/start.go`, replace `go func() { _ = server.Serve(metricsListener) }()` (line 145) so a non-`http.ErrServerClosed` error is logged at `Error` and cancels the root context, matching how the SNMP and NETCONF listeners already fail the process. Verify: a test that closes the metrics listener under the running server asserts the error is logged and the context is cancelled; `go test ./internal/cli/...` passes.
- [ ] 1.2 Update `docs/metrics.md` to state what happens when the metrics listener fails at serve time, and do not claim a taken port is silent (the `net.Listen` check already reports that). Verify: the documented behaviour matches the code path added in 1.1.

## 2. Reject unknown fields on `/api/simulate/*`

- [ ] 2.1 Add `DisallowUnknownFields` to `Server.decodeJSON` (`internal/restconf/server.go:526`) and route the resulting error through the existing `malformedRequest` 400 document. Leave `ops.decodeJSON` (`internal/restconf/ops/codec_json.go:44`) untouched, as scoped by design decision 6. Verify: a table test posting `{"link":"radio0","stte":"down"}` asserts 400 with the shared error body, and every existing valid simulation request still returns its previous status.
- [ ] 2.2 Document the strictness in `docs/protocols/RESTCONF.md`. Verify: the document states that unknown fields are rejected on the simulation endpoints specifically, and the documented example returns the status the code returns.

## 3. Make the `getBulk` cap discoverable

- [ ] 3.1 Add a regression test asserting that a `getBulk` requesting 500 repetitions answers `noError` with exactly `maxBulkRepetitions` varbinds. Verify: `go test ./internal/snmp -run TestGetBulk` passes and the test fails if `maxBulkRepetitions` is raised or the clamp is removed.
- [ ] 3.2 Document the cap in `docs/protocols/SNMP.md`, naming the value 100 and citing RFC 3416 §4.2.3 (a responder may return fewer repetitions than requested). Do not make the cap configurable. Verify: grep the doc for the cap value and confirm the code's constant matches it.

## 4. Make `Router.List` resilient

- [ ] 4.1 Change `Router.List` (`internal/router/router.go:526`) to skip a record it cannot describe instead of returning `nil, err` from the collect loop (line 551), and report the skipped paths to the caller rather than swallowing them. Verify: a test with one unresolvable path and two resolvable ones asserts both resolvable results are returned and the unresolvable path appears in the reported skips.
- [ ] 4.2 Update both callers for the new signature — `Router.Dispatch` (`internal/router/router.go:629`) proceeds past a skip, and `removeSubtree` stops depending on `List` once task 5.1 lands. Document the skip contract on `List`. Verify: `go build ./...` passes and no caller discards the skip report without a comment saying why.

## 5. Make L2 entry writes atomic

- [ ] 5.1 Give `removeSubtree` (`internal/l2/vlan.go:303`) a path-only enumeration instead of `Router.List`, so deleting a subtree stays complete when a path no longer resolves against the model (design decision 3). Verify: a test writes a leaf under the prefix whose path does not resolve, calls the delete path, and asserts the leaf is gone.
- [ ] 5.2 Assemble the leaves in `writeMAC` (`internal/l2/mac.go:258`) and `writeVLAN` (`internal/l2/vlan.go:257`) into one batch and submit it with a single `Router.Apply`, replacing the per-leaf `router.Set` calls. Verify: a test through the store double asserts one `Apply` per entry, and a test asserts running and candidate agree on every leaf afterwards.
- [ ] 5.3 Take `Router.Transaction` at the public method boundary — `learn`, `SetStatic`, `DeleteMAC`, `FlushMAC` and the VLAN counterparts — **outside** `m.mu`, and document the `txMu` then `m.mu` order in a comment. Never call `Transaction` from inside a locked region; `txMu` is not reentrant. Verify: `go test -race ./internal/l2/...` passes, and a concurrency test learning entries while a `Snapshot` reader runs never observes a partially written entry (four leaves and a count that disagree).
- [ ] 5.4 Update the `internal/l2` write-path description in `AGENTS.md` and `CONTRIBUTING.md` (Layer rules) if it no longer matches, and note the atomic write in `docs/protocols/L2.md`. Verify: the documented write path names `Router.Transaction`/`Router.Apply`, matching the code.

## 6. Integration verification

- [ ] 6.1 Run the full gate on the finished change. Verify: `./scripts/check.sh` passes, `go test -race ./...` passes, and `go vet -tags=integration ./...` is clean.
- [ ] 6.2 Confirm no golden file drifted as a side effect. Verify: `go test ./... -update` followed by `git status --short testdata/` reports no change; if a transcript legitimately changed, confirm the diff matches the intended behaviour change and no file was hand-edited.
- [ ] 6.3 Confirm the behaviour changes are observable end to end, not just unit-tested. Verify: a malformed `/api/simulate/*` body returns 400, an SNMP walk over a store containing one unresolvable record still returns the other records, and `getBulk` with 500 repetitions returns 100 varbinds.
