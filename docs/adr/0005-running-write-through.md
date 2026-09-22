# 5. Writes to running are mirrored into candidate

- **Status:** Accepted
- **Date:** 2026-09-22
- **Deciders:** TriadSim maintainers

## Context

The store keeps three datastores (`docs/store.md`). NETCONF works on `candidate` and applies it to
`running` with `<commit>`; `Commit` is candidate-authoritative, so it makes `running` a copy of
`candidate`. Three other writers, however, write `running` directly:

- SNMP `SetRequest` (`internal/snmp/agent.go`), because SNMP has no commit and a SET must take
  effect immediately;
- the L2 domain, which configures VLANs, MAC entries, STP port state and LLDP neighbours through
  `Router.Set` on running (`AGENTS.md`, `ROADMAP.md` Phase 4);
- a NETCONF `edit-config` or RESTCONF write that targets `running`, which the server advertises
  through `:writable-running:1.0` as well as `:candidate:1.0`.

A running-only write was invisible to candidate, so the next `<commit>` replaced running with
candidate and silently reverted it. Phase 4 recorded this as a deliberate compromise for RESTCONF
writes and flagged it for revisit; it applied just as much to SNMP and to the domain's own
configured state.

The same asymmetry broke removal. `internal/l2` removes a list entry by deleting its leaves from
`running` only, while `Router.SetState` wrote read-only state (a MAC entry's `age`, a counter)
to both datastores. A running-only entry plus one aging sweep therefore left an orphan
`age` leaf in candidate, and the next `<commit>` validated it as a `MACEntry{Type:"", Port:0}`
and answered `invalid-value` forever. A running-only delete could likewise be resurrected by a
commit that copied the stale candidate leaves back.

## Decision

Candidate is a superset of running, and the store enforces it: every mutation of `running` is
mirrored into `candidate` in the same critical section. Concretely:

- `(*Memory).Set(ctx, Running, path, value)` also writes the path to candidate.
- `(*Memory).Delete(ctx, Running, path)` also deletes the path from candidate.
- Writes and deletes against `Candidate` remain candidate-only: they are the pending edits that
  `<discard-changes>` drops and `<commit>` applies.
- `Commit` stays candidate-authoritative; the invariant is what keeps it from reverting live
  configuration. `Restore` (the confirmed-commit rollback) still leaves candidate untouched, so a
  rolled-back configuration remains a pending edit that can be re-committed.
- `Router.SetState` and `Router.DeleteState` become single writes to running, which removes the
  two-operation window in which a commit could observe the datastores apart.

## Consequences

- Configuration written by any plane survives a commit, and a deleted entry stays deleted. The
  `<commit>` result no longer depends on which protocol wrote what.
- Cross-plane writes are last-writer-wins and unsynchronized: a running-targeted edit updates the
  paths it addresses in candidate too, so it can overwrite a pending candidate edit for the same
  path. Accepted — the device has no cross-plane locking and no authentication (`docs/adr/0004`,
  no-auth model), so there is no session to arbitrate with.
- `Diff` never reports a running-only change any more; it reports exactly the pending edits.
- The invariant is enforced in `internal/store`, so it does not depend on every caller remembering
  it, and `internal/router` keeps ownership of read-only (`config:"false"`) enforcement.
- Documentation must stay in sync: `docs/store.md`, `docs/protocols/RESTCONF.md`,
  `docs/protocols/NETCONF.md` and `AGENTS.md` describe the write path.

## Alternatives

- **Drop `:writable-running` and route every write through candidate.** Rejected: SNMP has no
  commit, and a domain such as L2 must apply live configuration (STP port state, learned entries)
  without a management session. Removing the capability would only hide the divergence.
- **Make `Commit` merge candidate over running instead of copying it.** Rejected: it breaks the
  candidate-authoritative model that `<discard-changes>` and the confirmed commit rely on, and it
  makes the result of a commit depend on unrelated writes that happened after the edit.
- **Keep the asymmetry and document it per plane.** Rejected: it was already documented for
  RESTCONF and the defect still reached SNMP and the L2 domain, and the orphaned-state variant
  permanently broke `<commit>`.
