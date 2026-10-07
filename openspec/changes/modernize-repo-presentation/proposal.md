# Proposal

## Why

The repository has two competing planning surfaces and neither is authoritative. `ROADMAP.md`
(988 lines, Russian) still tells contributors to "pick the lowest open phase first", but Phases 0-8
are all complete and its Phase 8.2 backlog is stale: 15 items sit unticked, yet 11 of them are
already implemented by the P2/P3 remediation commits (`d98aaf9`, `6460667`, `7300b77`, `a3276f9`,
`9d688f4`, `ba427bb`, `0640bc9`) — so the document points new contributors at finished work.
`CHANGELOG.md` duplicates release history that GitHub Releases can own. Meanwhile `openspec/` was
initialized and left empty, so the spec-driven workflow the project intends to use has no content
and no entry point in the contributor docs.

## What Changes

- **Remove `ROADMAP.md`** (988 lines). The 4 backlog lines that are genuinely still open — 5
  defects, because the last line bundles two — are migrated to a separate OpenSpec change
  (`remediate-residual-backlog`) as tasks carrying file/line evidence. The 11 already-implemented
  items are not migrated; each was re-verified against git history and source rather than trusted
  from the roadmap.
- **Remove `CHANGELOG.md`**. Release history moves to GitHub Releases. The v0.1.0 notes are
  deliberately not backfilled — history restarts at the next tag (decided with the maintainer).
- **Update every reference** to the two files: `AGENTS.md`, `README.md`, `docs/README.md`,
  `docs/config.md`, `docs/adr/0001`, `docs/adr/0002`, `docs/adr/0005`, `docs/adr/template.md`, and
  one prose mention in `internal/restconf/errors.go`. There are no CI configs to update — this
  repo has none.
- **Finish adapting `.agents/skills/commit/SKILL.md` to this project.** The maintainer has already
  scrubbed the other project it was written for — its metadata, paths, examples and hardcoded lists
  — leaving deliberately generic slots. The remaining work is to remove the two foreign remnants
  that survive (a `feat(care): add AdaptiveWateringSaga` example, and a pre-commit hooks step for a
  repository that configures no hooks) and to fill the generic slots with this repository's gate,
  ignore list, layer ladder, and commit types and scopes.
- **Rewrite `README.md`** with badges and a status line that states the version and what is
  implemented, plus an OpenSpec usage section and a pointer to GitHub Releases for release notes.
  The current status banner still reads "Phase 7" and links to the roadmap being removed.
- **Update `CONTRIBUTING.md`** with the OpenSpec loop (propose -> review -> implement -> archive)
  and drop the roadmap-based guidance.
- **Add minimal `.github/`** issue and pull-request templates that reference OpenSpec. The
  directory does not exist today, so these are additions, not edits.
- **BREAKING (docs only):** any external link or bookmark to `ROADMAP.md` or `CHANGELOG.md` stops
  resolving. No API, wire format, configuration or runtime behavior changes.
- **Explicitly skipped:** `CODE_OF_CONDUCT.md` and `SECURITY.md`. Decided with the maintainer as
  ceremony that a single-author pet project does not need.

## Capabilities

No spec-level behavior changes. This change removes and rewrites documentation, adapts a tooling
skill, and adds GitHub templates; the simulator's managed objects, protocol surfaces, datastores and
CLI are untouched. Per the schema's own guidance, a change that does not alter behavior must not
invent a requirement to satisfy validation, so this change sets `skip_specs: true` in its
`.openspec.yaml`.

### New Capabilities

None.

### Modified Capabilities

None. `openspec/specs/` is empty, and no requirement in it changes.

## Impact

- **Removed:** `ROADMAP.md`, `CHANGELOG.md`.
- **Rewritten:** `README.md`, `CONTRIBUTING.md`, `.agents/skills/commit/SKILL.md`.
- **Edited:** `AGENTS.md`, `docs/README.md`, `docs/config.md`, `docs/adr/0001-record-architecture-decisions.md`,
  `docs/adr/0002-model-vs-yang.md`, `docs/adr/0005-running-write-through.md`, `docs/adr/template.md`,
  `internal/restconf/errors.go` (one comment).
- **Added:** `.github/` issue and pull-request templates.
- **Code behavior:** none. The single Go edit is a comment inside `internal/restconf/errors.go`, so
  `go build ./...`, `go test ./...` and the golden files are unaffected.
- **Dependencies:** none added or removed.
- **Follow-on:** `remediate-residual-backlog` carries the migrated work items.
