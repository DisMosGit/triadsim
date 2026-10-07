---
name: commit
description: "Review all uncommitted changes and split them into separate logically grouped conventional commits (type(scope): summary), ordered from foundational to dependent, staging files or hunks explicitly. Use when the user asks to commit, to split uncommitted work into commits, or to write commit messages."
allowed-tools: Bash(git:*)
license: MIT
metadata:
  version: "2.0"
---

Turn the current uncommitted work into a sequence of self-contained conventional commits, ordered so every commit builds on the one before it.

**Input**: Optionally a subset of paths, a requested grouping, or a scope. Without one, the scope is everything uncommitted — staged, unstaged, and untracked.

**Steps**

1. **Survey the worktree**

   Run and read all of:
   - `git status --short` — staged, unstaged, untracked
   - `git diff --stat` and `git diff` — unstaged content
   - `git diff --cached` — already-staged content
   - `git log --format='%s' -20` — the subject style this repository actually uses. New commits are subject-only, so read a batch of subjects to match tone and scope.
   - `git check-ignore -v <path>` — separate intentional content from ignored output. Never infer “ignored” from a filename alone.

   Read enough of every changed file to know *what it does*, not just which directory it sits in. Never group by filename alone: a single file often carries two concerns, and two files often carry one.

2. **Group into concerns**

   Each group is one concern that stands alone and reads coherently on its own. Name every group by the message you intend to write for it before touching the index.

   - A package’s tests ride with the code they test, as `internal/<pkg>/<name>_test.go` alongside `internal/<pkg>/<name>.go`; an integration test under `test/integration/` rides with the change it covers. A separate `test(<scope>)` commit is for standalone test infrastructure only (shared fixtures, test helpers).
   - Config settings ride with the feature that consumes them; an event that crosses a domain boundary rides with the domain that publishes it.
   - Docs describing changed behaviour ride with the change; substantial prose becomes its own `docs(<scope>)` commit.
   - Change-plan artifacts are the files under `openspec/changes/<name>/`: the plan first, then any spec delta with the code, and task tick-offs in the same commit as the work that task describes. Never tick a task ahead of its work.
   - A regenerated artifact rides with the change that made it stale. Regenerate and commit it with the code, never on its own.
   - Formatting-only churn in files unrelated to a change is never mixed into it — leave it out, or ask (step 8). A repository-wide formatter pass produces exactly this churn.

3. **Order foundational to dependent**

   Dependencies point forward: no commit may need content that only lands in a later one. A typical ladder:

   `model` → `store` / `router` / `datatree` → `radio` / `l2` / `sync` → `snmp` / `netconf` / `restconf` / `gnmi` → `cli` / `metrics` → docs → task tick-offs.

   Not every change uses every rung; preserve the relative order of the rungs it does use. A commit that makes a domain package depend on a management plane, or `internal/model` depend on anything, is on the wrong rung.

4. **Write the messages**

   `type(scope): summary`, e.g. `feat(l2): add QinQ outer-tag rewrite`.

   - **Types** (authority: `CONTRIBUTING.md`): `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `chore`, `revert`.
   - **Scopes** (authority: `CONTRIBUTING.md`): `radio`, `l2`, `sync`, `model`, `store`, `router`, `event`, `snmp`, `netconf`, `restconf`, `gnmi`, `cli`, `metrics`, `docs`, `deps`. Do not invent a scope outside the list without saying why. Pick the narrowest scope that is true.
   - Imperative mood, lowercase, no trailing period. Keep the subject short — treat a short line as the target rather than a limit to fill.
   - **Subject only — no body.** Every commit is a single line: `type(scope): summary`. Do not pass a second `-m`, do not write explanatory paragraphs, and do not append trailers.
   - No `Co-Authored-By` or “Generated with” trailers.

5. **Stage explicitly**

   Stage one group at a time with explicit paths: `git add -- <path>…`.

   When one file’s hunks belong to different groups, split them: `git add -p` when the session is interactive, otherwise build a filtered patch and `git apply --cached <patch>`. Confirm what is actually staged with `git diff --cached --stat` and `git status --short` before committing.

   Never `git add -A`, `git add .`, `git add -f`, or `git commit -a`. Committing is not editing: never modify tracked content to force a clean split. If a file genuinely mixes unrelated concerns, that is an ambiguity — ask (step 8).

6. **Run the safety gate before every commit**

   Read the full staged diff — not just the stat — then check:

   - **Secrets**: this project commits no credentials — no token, key or password belongs in the tree. Never stage one, and never stage a credential to “fix it later”. If credential-looking content appears in the diff, stop and report it.
   - **Ignored and build output**: never force-add what `.gitignore` covers — `bin/`, `startup.json` (persisted device state), `configs/local*.yaml`, `.docs/`, `*.out` and `coverage.*`. The deliberate exception is `openspec/` and `.agents/`, which are untracked but **not** ignored: they belong in the history. Verify with `git check-ignore -v <path>` rather than guessing.
   - **Generated files**: `testdata/*.golden.json` is committed and must be regenerated with the change that made it stale — `go test ./internal/netconf -update`, then inspect the diff. Never hand-edit a golden file. If a check reports a stale golden file, regenerate it and commit the result with the code.
   - **Build**: run `./scripts/check.sh` — `gofmt -l .`, `go vet ./...`, `go vet -tags=integration ./...` and `go test ./...`, plus the integration tests when Docker is present (`make lint && make test` runs the same gate). A change's spec delta is gated by `openspec validate "<name>" --strict`. If an auto-formatter rewrites files, re-check `git status` and put each resulting edit in the commit it belongs to, or ask. Never commit a tree the commit itself breaks.

7. **Commit and confirm the sequence**

   Commit each group with its single-line message: `git commit -m "<subject>"`. Then re-run `git status --short` before starting the next group. Never `--amend` unless the user asks for it, and never amend a commit that may be pushed — check `git branch -r --contains <sha>` first.

8. **Stop and ask when a grouping is ambiguous**

   Ask — with the concrete candidates, the exact paths or hunks each would take, and a recommended option — when:

   - a file’s hunks mix unrelated concerns and cannot be split without editing it;
   - a change plausibly belongs to two groups, or its tests/docs could accompany either of two commits;
   - formatting-only churn lands in files unrelated to the change;
   - a file’s intent is genuinely unclear, or work is already partly committed so an amend would be needed;
   - credential-looking content appears in the diff — never rewrite history to remove an already-committed secret, stop and report it;
   - task tick-offs disagree with the code actually present;
   - the user’s requested grouping conflicts with a dependency order.

   Otherwise proceed: the instruction is to ask on ambiguity, not to gate every commit.

9. **Verify and report**

   After the last commit, check the result:

   - `git log --oneline <original-head>..HEAD` — the commits, in order
   - `git status --short` — only intentionally-left changes remain

   Report each commit with its hash, subject line, and files/hunks; what was intentionally left uncommitted and why; anything the safety gate skipped; and any commit that did not come out self-contained.

**Guardrails**

- Never push, create or delete branches, rebase, reset, or drop stashes.
- Never rewrite history — not even to remove a secret; stop and report instead.
- Never commit unless committing is what was asked for.
- Write subject-only commit messages — one line, no body.
- In plan mode, present the grouping plan for approval instead of committing.
- Grouping is a judgment call about the user’s intent: when the diff does not settle it, ask rather than guess.
