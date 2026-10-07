# Design

## Context

See `proposal.md` - Why for motivation. The constraints that shape the approach:

- `ROADMAP.md` is the only place the project's "Phase 0-8" vocabulary is defined. Every phase
  reference elsewhere (`README.md` status banner, `docs/adr/0002`, `docs/adr/0005`, `docs/config.md`)
  points into it, so removing the file removes the definition those references rely on.
- `CHANGELOG.md` is the only in-repo record of the v0.1.0 release.
- `.agents/skills/commit/SKILL.md` has already been scrubbed of the other project it was written
  for: the `adapted-for`/`author` metadata, its paths, its examples and its hardcoded lists are gone,
  leaving deliberately generic slots such as "the project's lint, test, coverage, and validation
  commands" and "the repository's documented scope list". Two foreign remnants survive (a
  `feat(care): add AdaptiveWateringSaga` example and a pre-commit hooks step), and the generic slots
  are unfilled.
- The repository has no CI and no `.github/`. `.gitignore` already excludes `bin/`, `startup.json`,
  `configs/local*.yaml` and `.docs/`.
- `openspec/specs/` is empty, so there is no existing capability taxonomy to conform to.

## Goals / Non-Goals

**Goals:**
- One planning surface. After this change, OpenSpec holds in-flight work and GitHub Releases holds
  release history; nothing in the repository competes with either.
- No dangling reference to a removed file anywhere in the tree.
- A commit skill that describes *this* project's toolchain and would produce a correct commit here
  without the reader knowing another project exists.

**Non-Goals:**
- Backfilling `openspec/specs/` for the ~34k lines of existing behavior. That is a separate
  decision, deliberately not taken here.
- Adding CI. The project has none by choice, and the commit skill's type list has no `ci` type.
- Creating the `remediate-residual-backlog` change's implementation. This change only guarantees the
  migrated items survive.

## Decisions

**1. Retire the phase vocabulary rather than preserve it.**
The status banner and the phase references in ADRs become a version plus a statement of what is
implemented. Alternatives: (a) keep numbering the phases inside OpenSpec - rejected, because
OpenSpec changes are the unit of planned work and a parallel phase counter would immediately begin
drifting again, which is the defect this change exists to fix; (b) keep a historical
"Phases 0-8 complete" note - rejected as a sentence that references a document nobody can read.
ADRs 0002 and 0005 cite "ROADMAP.md Phase 4/7" as the origin of a decision; those become a plain
statement of the decision, since an ADR's value is the decision, not the phase number.

**2. Open Phase 8.2 work migrates to a change, not to specs.**
Specs describe behavior that is required; the five open items are defects that are not yet fixed, so
writing them as requirements would assert behavior the code does not have. They become tasks in
`remediate-residual-backlog` with file/line evidence. Alternative: fold them into this change -
rejected, because it would mix a docs sweep with code fixes and make both harder to review.

**3. The migrated change is created before the file is deleted.**
Task ordering: create `remediate-residual-backlog`, then delete `ROADMAP.md`. Applying the change in
pieces therefore cannot lose the backlog. The 11 already-implemented items are not migrated at all -
they are complete, and copying them forward would recreate the false-open-boxes problem this change
is fixing.

**4. The commit skill is written for this repository, not for repositories in general.**
The maintainer's scrub removed the former project's specifics and left generic slots ("the project's
lint, test, coverage, and validation commands", "the repository's documented scope list"). This
change fills them with this repository's concrete facts: the gate is `./scripts/check.sh`, golden
files under `testdata/` are regenerated rather than hand-edited, the ladder follows the Layer rules
in `CONTRIBUTING.md`, and the commit types and scopes are that file's lists. Alternative: leave the
slots generic and make the reader resolve each one from `CONTRIBUTING.md` - rejected by the
maintainer, who wants the skill usable without cross-referencing a second document.

This knowingly accepts a duplication risk the skill's earlier form argued against: a restated scope
list can drift from `CONTRIBUTING.md`. Mitigation: the skill names `CONTRIBUTING.md` as the
authority for types and scopes, so a reader who finds a mismatch knows which copy to fix.

**5. Steps describing absent tooling are deleted, not reworded.**
The maintainer's scrub already generalised or removed most of the former project's tooling. Two
remnants are deleted outright rather than translated: the pre-commit hooks step (this repository
configures no hooks - `.git/hooks` holds only samples, and there is no `.pre-commit-config.yaml`)
and "coverage" in the build line (there is no coverage target; the `Makefile` mentions
`coverage.out` only in `clean`). The secrets section stays a short note: this project commits no
credentials, and the `.env` line in `.gitignore` is the stock Go template. A skill that describes
friction the reader will never hit teaches them to distrust it.

**6. Release notes live only in GitHub Releases.**
No in-repo replacement file, and the v0.1.0 notes are not backfilled (maintainer decision). Recorded
here so it reads as a choice rather than an oversight.

**7. `.github/` templates stay minimal and OpenSpec-pointing.**
One issue template and one pull-request template. Both ask for the OpenSpec change name and nothing
that a solo maintainer would answer with "n/a".

## Risks / Trade-offs

- **External links to `ROADMAP.md` / `CHANGELOG.md` break** (bookmarks, prior PR comments) ->
  accepted; the content is recoverable from git history, and both files were already stale.
- **v0.1.0 release notes are discarded** -> accepted by the maintainer; from v0.1.0 onward GitHub
  Releases is the single record.
- **A reference is missed and a link rots silently** -> mitigation: the tasks include an explicit
  exhaustive grep for both filenames as a verification step, not just a spot check.
- **The adapted skill names a build gate that drifts** (e.g. `scripts/check.sh` is renamed) ->
  mitigation: the skill names the script and the `Makefile` target that wraps it, so a reader can
  find the live definition from either.
- **The restated commit types and scopes drift from `CONTRIBUTING.md`**, which is the duplication
  decision 4 accepts -> mitigation: the skill names `CONTRIBUTING.md` as the authority for both, and
  task 2.4 requires them to match at the time of the change.
- **Removing the roadmap loses the "what's left" view** -> mitigation: that view moves to
  `openspec list`, which is the point of the change; `remediate-residual-backlog` gives it content
  immediately.

## Migration Plan

Order matters so the change is safe to apply incrementally:

1. Create the `remediate-residual-backlog` change with all five items captured.
2. Adapt `.agents/skills/commit/SKILL.md`.
3. Rewrite `README.md`, `CONTRIBUTING.md`, `AGENTS.md`, `docs/*` and the ADRs to drop both
   references and the phase vocabulary.
4. Add `.github/` templates.
5. Delete `ROADMAP.md` and `CHANGELOG.md` last, once nothing points at them.
6. Verify: an exhaustive grep for both filenames returns nothing outside `openspec/changes/archive/`
   and git history; `./scripts/check.sh` still passes.

**Rollback:** revert the commit. Both removed files are restored by `git revert`, and no code or
configuration depends on their absence.
