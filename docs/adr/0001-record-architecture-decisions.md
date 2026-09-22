# 1. Record architecture decisions

- **Status:** Accepted
- **Date:** 2026-09-22
- **Deciders:** TriadSim maintainers

## Context

TriadSim makes architectural choices that are cheap to apply now and expensive to reverse later:
models instead of a runtime YANG parser, one binary instead of services, no authentication
anywhere, simplified STP and PTP state machines. Without a written record, the reasoning behind
those choices is lost and the same debate is repeated in later phases (or worse, silently
reversed by a well-meaning change).

## Decision

Every architecturally significant decision is recorded as a numbered Architecture Decision
Record in `docs/adr/`, using the format in [template.md](template.md).

A decision is significant when it:

- fixes or changes a package boundary, a public interface or a wire-visible behaviour;
- closes off an alternative that a future phase is likely to revisit;
- introduces or removes a dependency, an external service or a persistence format;
- deviates from `ROADMAP.md`, `AGENTS.md` or a protocol reference.

Rules:

1. One decision per file, numbered sequentially: `0001-record-architecture-decisions.md`,
   `0002-...`. Numbers are never reused, even when a record is superseded.
2. A record is immutable once accepted. To change a decision, add a new record and set the old
   one to `Superseded by ADR-NNNN`; both stay in the repository.
3. Status is one of `Proposed`, `Accepted`, `Rejected`, `Deprecated` or `Superseded by ADR-NNNN`.
4. Records are written in the same pull request as the change they justify, so a reviewer sees
   both together.
5. Keep them short: context, decision, consequences, alternatives. Reference
   `docs/protocols/*.md` rather than repeating protocol detail.

## Consequences

- Reviewers can see why the project looks the way it does, and can challenge a decision in one
  place instead of re-deriving it.
- Traceability between code, roadmap and protocol documentation improves.
- The cost is small: one short document per significant decision, maintained with the code.

## Alternatives

- **No ADRs, rely on commit messages.** Rejected: commit history is not discoverable and cannot
  be amended once a decision is superseded.
- **A single living "design" document.** Rejected: it grows unbounded, and superseded reasoning
  is edited away instead of kept.
- **A wiki or issue tracker.** Rejected: decisions must live with the code that implements them
  and be reviewable in the same pull request.
