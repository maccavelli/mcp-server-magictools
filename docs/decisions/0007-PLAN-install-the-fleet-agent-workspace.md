---
status: implemented
date: 2026-11-17
associated-madr: "0007-MADR-install-the-fleet-agent-workspace.md"
---

# Implement the fleet agent workspace

Associated MADR: [0007-MADR-install-the-fleet-agent-workspace.md](0007-MADR-install-the-fleet-agent-workspace.md)

## Grounding Reference

This plan installs the workspace layer decided in
[0007-MADR-install-the-fleet-agent-workspace.md](0007-MADR-install-the-fleet-agent-workspace.md).
Templates are the `templates/` directory of the `workspace-scaffolding` skill.
Apply order is that skill's `references/apply.md`. Docs layout is
`documentation-writing`. Record shape is `madr-and-plan-writing`, plus the
strict markdownlint profile, because pair `0007` is outside the legacy filter.

The skill's own plant suite already covers the checker and the linter. This
plan still plants the gates that are new on this repository, in a scratch
copy, before calling them done.

## Prerequisites

* Owner approval to execute this plan (`proceed`, `execute the plan`, or
  `do phase N`). Until that approval, the only files this work adds are this
  pair.
* The working tree may already contain the uncommitted Go pin bump for pair
  `0006`. Do not revert it. Do not `git checkout`, `git restore`, or
  `git reset` those paths. Do not commit.
* `git rev-parse --path-format=absolute --git-path hooks` is the user-global
  hooks directory. If a later turn asks for a commit, use
  `git commit --no-edit` and load `sysadmin-git-branch-gates` first. This
  plan does not commit.
* One docs tree. Do not create `docs/reports/` or an empty directory. Do not
  create a second `docs/guides/`; the guides are already there.
* Do not add `.editorconfig`, Dependabot, `SECURITY.md`, `CODEOWNERS`,
  `CONTRIBUTING.md`, `CHANGELOG.md`, a CI workflow, a regular-file `AGENTS.md`,
  or a Go lint target.

## Stage 1: Line endings and ignores

1. Copy `templates/.gitattributes` to `.gitattributes`. `cmp` it against the
   template. Do not add a provenance comment. Do not
   `git add --renormalize`.
2. Append to `.gitignore` only the fragment entries that are absent. The
   absent ones are `__pycache__/` and `*.py[cod]`, under the fragment's
   Python comment. Leave every existing line in place, including the
   coverage, log, tmp, cache, and worktree lines the fragment also lists.

## Stage 2: Makefile recipes

1. Prepend `templates/Makefile.snippet` to `Makefile`. Keep the existing
   `MOD_VERSION` line and every current target, including the uncommitted
   `1.26.11` value.
2. Do not add a Go gate, a `preflight` target, or a `test` change.
3. The snippet sets `SHELL` and `.SHELLFLAGS` for every recipe. After the
   splice, run `make help`, `make check-records`, and `make markdownlint`.
   `make check-records` and `make markdownlint` will fail until Stage 3 and
   Stage 4 have landed; re-run them in Stage 5. If `make help` fails because
   of `-eu` or `pipefail`, stop. Do not weaken `.SHELLFLAGS`.

## Stage 3: Docs gates and the existing records

1. Copy `templates/.markdownlint-cli2.jsonc` to `.markdownlint-cli2.jsonc`.
   Then make only these two edits:
   * Set the legacy override `filter` to
     `["**/000[1-6]-{MADR,PLAN}-*.md"]`.
   * Add `!**/internal/forks/**` to `globs`, beside the vendored-skill
     exclusions.
2. Delete `.markdownlint.json`.
3. Copy `templates/scripts/check_records.py` to `scripts/check_records.py`.
   `cmp` it against the template.
4. `git mv` each `docs/NNNN-MADR-*.md` and `docs/NNNN-PLAN-*.md` for `0001`
   through `0006` into `docs/decisions/`. One move per pair, so the MADR and
   the PLAN land together. Do not renumber. Do not edit bodies except for the
   three items below.
5. In `docs/decisions/0003-PLAN-bound-the-orphan-reaper-process-walk.md` and
   `docs/decisions/0004-PLAN-a-parse-error-must-not-replace-the-operators-server-registry.md`,
   change frontmatter `status: complete` to `status: implemented`. Change no
   other status.
6. In `0003-PLAN`, convert the four indented command blocks (the baseline
   `git`/`go` lines, the `isLegitimateDescendant` signature, the verification
   command list, and the `go test -overlay` line) into fenced blocks with a
   language tag. Keep the command text. Do not edit a block that is already
   fenced. Do not edit any other record's fences.
7. Remove `<!-- markdownlint-disable … -->` comments from the `0003`, `0005`,
   and `0006` records. Re-run markdownlint. If removing one of them reports a
   new finding, restore that comment and add a dated deviation to this plan.
   Do not convert asterisk lists to dashes. Do not add strict-profile headings
   to `0001`–`0006`.
8. Write `docs/architecture.md` from `templates/docs/architecture.md`. Replace
   `{{WHAT_IT_IS}}` with a present-tense description: MagicTools is a local
   MCP orchestrator; the module is `github.com/maccavelli/mcp-server-magictools`;
   the entrypoint is `cmd/mcp-server-magictools`; orchestrator packages are
   under `internal/`; install scripts are under `scripts/`; operator guides
   are under `docs/guides/`; one docs tree covers the product. Replace
   `{{WHAT_IS_NOT_HERE}}` with bullets for operator config and the Badger
   datastore (outside this repository), downstream MCP servers (separate
   programs named from the operator's `servers.yaml`), release binaries
   (tag workflow; `dist/` is gitignored local output), and
   `internal/forks/hnsw` (vendored `github.com/coder/hnsw`, excluded from
   markdownlint here). Leave the template's fixed CI, secrets, and host-layer
   bullets in place.
9. Write `docs/README.md` from `templates/docs/README.md`. The records table
   lists `0001` through `0007`, MADR and PLAN, with the status each file has
   after step 5.
   2026-11-17: the table also lists `0008`, which was added while this plan
   was in progress. Leaving it out would make the index omit a record. The "I want to…" table points at `architecture.md`,
   `../CLAUDE.md`, this MADR, and the existing guides using the same tasks as
   the root README's "I want to" table, with `guides/` relative links. Do not
   leave the template sentence that says no guide exists.
10. In the root `README.md`, add a link to `docs/README.md` in the "I want to"
    table and replace the sentence that says the decision artifacts remain in
    `docs/` so it points at `docs/decisions/` and `docs/README.md`. Do not
    otherwise rewrite the product README. Keep the uncommitted pin-bump hunks.
11. Reflow `GEMINI.md` only enough to clear `MD022`, `MD032`, and `MD013`: a
    blank line between the heading and the list, and a wrap of the line that
    exceeds 200 characters. Do not change the commit-protocol sentences.

## Stage 4: Agent files

1. Write `CLAUDE.md` from `templates/CLAUDE.md` with these slots:
   * `{{SCOPE}}`: one short paragraph. MagicTools is a local MCP orchestrator
     and gateway that supervises downstream stdio MCP servers, indexes their
     tools, and proxies calls through one entry point. What exists today is
     in `docs/architecture.md`. The record index is in `docs/README.md`.
   * `{{DEPENDENCIES}}`: the Go module and `go.sum` are already in the tree.
     No dependency may be added without a MADR in this repository that names
     it. Lockfiles change in the same commit as the first import, and are
     cleaned in the commit that removes the last. Build and test commands
     already live in the root README and the `Makefile`; this file does not
     invent a new preflight.
   * `{{PREADD}}`: before staging, run the repository's pre-add command if one
     exists (none is defined here). Before every commit, run
     `make check-records` and `make markdownlint`. A file that fails is not
     committed.
2. From the repository root, `ln -s CLAUDE.md AGENTS.md`. The link target is
   the relative name `CLAUDE.md`, never an absolute path. `git add` both.
   `git ls-files -s AGENTS.md` must show `120000`.
3. Copy every pointer and gitignore in `references/file-set.md`, and
   `templates/opencode.json`, and `cmp` each one. That is both rule files for
   each of the five harnesses (the MADR/PLAN pointer and the
   coordinator-agents pointer), `.claude/.gitignore`, `.kilo/.gitignore`, and
   root `opencode.json`.

## Stage 5: Verify

1. `python3 scripts/check_records.py --next` prints `0008`.
   2026-11-17: ~~`0008`~~. Record `0008` was added while this work was in
   progress, so the next number is `0009`.
2. `python3 scripts/check_records.py --check-all` exits 0.
3. `npx --yes markdownlint-cli2@0.23.2` exits 0.
4. `make check-records` and `make markdownlint` exit 0.
5. `cmp` every byte-copied file against its template. The only allowed
   difference is the two deltas in `.markdownlint-cli2.jsonc`.
6. Read `git ls-files --eol` and `git ls-files -s CLAUDE.md AGENTS.md` in
   full. Every tracked text file shows `attr/text=auto eol=lf`. `CLAUDE.md`
   is `100644`. `AGENTS.md` is `120000` and its target is exactly `CLAUDE.md`.
   Any other line is a finding. Do not renormalize the tree to force it.
7. In a scratch copy, not in this working tree, show each new gate fail once:
   * a broken relative link in a record, so `--check-all` prints
     `broken relative link:` and exits 1;
   * a PLAN whose frontmatter status is `complete`, so `--check-all` prints
     `status:` and exits 1;
   * a `* item` list in a scratch `README.md`, so markdownlint prints
     `MD004/ul-style`.
   Record the observed failure text in this plan's execution record.
8. Scan files added or changed by this plan for the local account name, mail
   and git-host domains, and user-profile path prefixes. Read those strings
   from the environment at run time. Do not write them into a file. A hit is
   a finding to redact before the stage is done.
9. Do not stage product paths this plan did not edit (`go.mod`, `go.sum`,
   `internal/forks/hnsw/go.mod`, `docs/guides/platform-installation.md`).
   Do not commit.

## Rollback

Delete the files this plan added (`CLAUDE.md`, the symlink `AGENTS.md`, the
pointer trees, `opencode.json`, `.gitattributes`, `.markdownlint-cli2.jsonc`,
`scripts/check_records.py`, `docs/README.md`, `docs/architecture.md`, and this
pair if the install is abandoned). Restore `.markdownlint.json` from git.
`git mv` the `0001`–`0006` records back to `docs/`. Restore the two PLAN
statuses, the `0003-PLAN` code blocks, the disable comments, the `Makefile`
prepend, the `.gitignore` append, the root README link, and the `GEMINI.md`
reflow from the pre-stage contents. Do not use `git reset --hard` or
`git clean`. The uncommitted pin bump must still be present afterward.

## Completion Criteria

* Pair `0007` is the executed record, status `implemented` on this plan only
  after every criterion here is met. The checker's vocabulary has no
  `complete`. The MADR stays `proposed` until the owner accepts the decision;
  execution approval is not the same as accepting the MADR.
  On 2026-11-17 the owner accepted it. The MADR status is `accepted`.
* `0001`–`0006` live in `docs/decisions/`, numbers unchanged, and `--check-all`
  exits 0.
* `0003-PLAN` and `0004-PLAN` frontmatter status is `implemented`.
* `.markdownlint.json` is gone. `.markdownlint-cli2.jsonc` differs from the
  template only by the legacy filter and the forks glob.
* `CLAUDE.md` is a regular file and `AGENTS.md` is a symlink to it.
* All five harness pointers and both coordinator pointers are `cmp`-clean.
* The scratch plants failed in the way Stage 5 names, and the identifier scan
  reported no hit.
* No commit was created. The uncommitted `0006` pin bump is still in the
  working tree.

## Execution record

### 2026-11-17: legacy docs for the future gates

The owner asked to lint and fix the legacy docs so a future CI job can run
these gates. This turn did Stage 3 steps 1 through 7, the location sentence
in the root README from step 10, and the `GEMINI.md` reflow in step 11.
Stages 1, 2, and 4, Stage 3 steps 8 and 9, and the Stage 5 plants are not
done. No commit. The CI workflow was not edited. CI does not call these
commands yet. Both commands pass on the tree as it is:

* `python3 scripts/check_records.py --check-all` exited 0.
* `python3 scripts/check_records.py --next` printed `0009`.
* `npx --yes markdownlint-cli2@0.23.2` exited 0, 0 issues in 27 files.

What changed:

* `.markdownlint-cli2.jsonc` is the fleet template plus the legacy filter
  `**/000[1-6]-{MADR,PLAN}-*.md` and the `!**/internal/forks/**` glob.
  `.markdownlint.json` is deleted, because leaving it merges a second rule
  set over the record profile.
* `scripts/check_records.py` is a byte copy of the template (`cmp` matched).
* `0001` through `0006` moved with `git mv` into `docs/decisions/`.
* `0003-PLAN` and `0004-PLAN` frontmatter status is `implemented`.
* The four indented command blocks in `0003-PLAN` are fenced. The command
  text is unchanged.
* The `markdownlint-disable` comments in `0003`, `0005`, and `0006` are
  gone. A re-lint reported nothing new from removing them.
* `GEMINI.md` has a blank line under its heading, and the line that was over
  200 characters is wrapped. The commit sentences are the same words.
* The root README now points at `docs/decisions/` for the records.

Uncommitted amendments already in `0005` and `0006` were left in place,
aside from the disable-comment removal.

Deviation, 2026-11-17. `docs/0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md`
appeared untracked during the move. It is outside the legacy filter, so the
strict profile applies. The gate failed on placement, one hard tab inside a
Go fence, and `### Consequences`. The file's own text said to move it if the
`0001`–`0006` move landed first. It is now
`docs/decisions/0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md`.
The tab is four spaces, `## Consequences` is an H2, and its
`markdownlint-disable MD004` comment is removed. The sentence that said the
file still sat in `docs/` now says it lives in `docs/decisions/`. The decision
text is otherwise unchanged. A `0008` PLAN appeared in `docs/decisions/`
before the gates were re-run. It was not edited here. The last run linted
28 files, including that plan, and exited 0.

An identifier scan of the dirty paths, for the local account name, the home
directory, and the global git email and its domain, reported no hit. The
scanned strings were not written down.

### 2026-11-17: scaffold completed

The owner asked to complete the scaffolding. Stages 1, 2, 4, and 5 are done.
Stage 3 steps 8, 9, and the rest of step 10 are done. No commit.

* `.gitattributes` is a byte copy of the template. `git ls-files --eol` was
  read in full: 427 lines, and every line contains `attr/text=auto eol=lf`.
  The tree was not renormalized.
* `.gitignore` gained only the Python cache comment, `__pycache__/`, and
  `*.py[cod]`.
* `Makefile` has the snippet prepended. `MOD_VERSION` is still `1.26.11`.
  `make help` exited 0. `.SHELLFLAGS` was not weakened.
* `docs/architecture.md` and `docs/README.md` are written. The index lists
  `0001` through `0008`. The root README links to `docs/README.md`.
* `CLAUDE.md` is mode `100644`. `AGENTS.md` is mode `120000` and its target
  is exactly `CLAUDE.md`.
* Every pointer, both gitignores, and `opencode.json` are `cmp`-identical
  to the templates. `scripts/check_records.py` is too.
* `make check-records` and `make markdownlint` exited 0. Markdownlint
  reported 0 issues in 42 files.
* Scratch plants, not in this tree:
  * control `--check-all` exited 0;
  * `[gone](missing.md)` printed
    `broken relative link: docs/decisions/0001-MADR-x.md:11: missing.md`
    and exited 1;
  * PLAN status `complete` printed
    `status: docs/decisions/0001-PLAN-x.md: 'complete' is not a PLAN status (proposed | accepted | in-progress | deferred | implemented | rejected | withdrawn | superseded by NNNN-PLAN-<slug>.md)`
    and exited 1;
  * a `* item` list in a scratch `README.md` printed
    `README.md:3:1 error MD004/ul-style Unordered list style [Expected: dash; Actual: asterisk]`
    and exited 1.
* A second identifier scan of the dirty paths, for the same kinds of string,
  reported no hit.
* `go.mod`, `go.sum`, `internal/forks/hnsw/go.mod`, and
  `docs/guides/platform-installation.md` were not staged.

### 2026-11-17: owner accepted the MADR

The owner set the decision to accepted. `0007-MADR` frontmatter is
`status: accepted`. The index row matches. `0001`, `0002`, `0004`, and
`0008` stay `proposed`. This plan stays `implemented`.
