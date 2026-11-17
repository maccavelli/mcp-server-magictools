---
status: accepted
date: 2026-11-17
---

# Install the fleet agent workspace without rewriting existing decisions

## Context and Problem Statement

This repository is the MagicTools MCP orchestrator. It has product source,
operator guides under `docs/guides/`, and six MADR/PLAN pairs, `0001` through
`0006`, filed directly in `docs/`. It has no `CLAUDE.md`, no `AGENTS.md`, no
`docs/decisions/`, no `scripts/check_records.py`, no `.gitattributes`, and no
fleet markdownlint configuration. A root `.markdownlint.json` sets line length
120 and dash lists. `GEMINI.md` is a second, short commit-protocol note.
`internal/forks/hnsw` is a vendored copy of `github.com/coder/hnsw` and carries
that project's README.

The fleet workspace is one layer: `CLAUDE.md` as the only instruction file,
`AGENTS.md` as a symlink to it, thin pointers for Claude, Grok, OpenCode,
Codex, and Kilo, the fixed docs tree, `scripts/check_records.py`, markdownlint,
and LF line endings. How a record is written, and where a document sits, belong
to `madr-and-plan-writing` and `documentation-writing`. This decision is only
where that layer lands in this repository, and what happens to the files
already here.

A scratch copy of the tree, with the twelve records moved under
`docs/decisions/` and the fleet checker and linter pointed at it, showed:

* `scripts/check_records.py --check-all` reports exactly two errors, both
  `status: … 'complete' is not a PLAN status`. They are
  `0003-PLAN-bound-the-orphan-reaper-process-walk.md` and
  `0004-PLAN-a-parse-error-must-not-replace-the-operators-server-registry.md`.
  Every other frontmatter status on `0001`–`0006` is in the checker's
  vocabulary. Relative links between those records resolve after the move.
* With the fleet `.markdownlint-cli2.jsonc` and a legacy filter covering
  `0001`–`0006`, markdownlint reports four `MD046` indented code blocks in
  `0003-PLAN-bound-the-orphan-reaper-process-walk.md`, two heading and list
  spacing findings plus one line over 200 characters in `GEMINI.md`, and many
  findings in `internal/forks/hnsw/README.md`. The root `README.md` and
  `docs/guides/` are clean under that configuration.
* Copying the existing `.markdownlint.json` into that scratch tree makes
  markdownlint merge it back in: line length becomes 120, and `MD004` fires on
  asterisk lists inside the legacy records. The record profile's
  `combine: "replace"` no longer isolates those files.

The working tree also holds uncommitted Go pin work for pair `0006` (`Makefile`,
`README.md`, `docs/guides/platform-installation.md`, `go.mod`, `go.sum`,
`internal/forks/hnsw/go.mod`, and the staged `0006` records). `git rev-parse
--path-format=absolute --git-path hooks` resolves to the user-global hooks
directory, so commit messages stay with `git commit --no-edit`. This decision
does not commit anything.

## Decision Drivers

* A clone must get the same gates and the same mutate-only-with-a-pair rule as
  the rest of the fleet.
* Copied templates stay byte-identical except for deltas this record names.
* Existing decisions keep their numbers and their rationale.
* The checker is copied, not patched. Status words it rejects have to change
  in frontmatter.
* The vendored HNSW README is upstream text. Rewriting it to satisfy this
  repository's human style would fork that text.
* Uncommitted product edits already in the tree stay in the tree. This install
  does not revert them and does not commit them.
* Go lint, the CI workflow, and product identity stay outside this install.

## Considered Options

* Install the fleet workspace, move `0001`–`0006` into `docs/decisions/`
  without renumbering, and keep those records on the legacy lint profile.
* Teach `scripts/check_records.py` to accept records that sit directly in
  `docs/`, and to accept PLAN status `complete`.
* Rewrite `0001`–`0006` onto the strict record profile in this same change.
* Add `CLAUDE.md` and the pointers, and do not add the checker or the linter.

## Decision Outcome

Chosen option: "Install the fleet workspace, move `0001`–`0006` into
`docs/decisions/` without renumbering, and keep those records on the legacy
lint profile", because the templates already express the layout and the gates,
and the scratch run shows the existing records can pass those gates with a
small, named set of edits.

### Confirmation

`python3 scripts/check_records.py --check-all` and
`npx --yes markdownlint-cli2@0.23.2` both exit 0.
`git ls-files -s CLAUDE.md AGENTS.md` shows mode `100644` for `CLAUDE.md` and
mode `120000` for `AGENTS.md`, and the link target is exactly `CLAUDE.md`.
Every tracked text file reports `attr/text=auto eol=lf` from
`git ls-files --eol`. Each byte-copied file is `cmp`-identical to its template.
The two named deltas in `.markdownlint-cli2.jsonc` are the legacy `filter` and
the `internal/forks` glob, and nothing else.

## Consequences

* Good, because any of the five harnesses opens the same instruction file and
  the same pointer gate.
* Good, because `0001`–`0006` keep their numbers, their prose, and their
  relative links. Sibling links still resolve from `docs/decisions/`.
* Good, because the checker and the linter configuration stay on the fleet
  copy, so a later refresh can `cmp` them.
* Bad, because two PLAN frontmatter statuses change from `complete` to
  `implemented`. The checker has no `complete` status. `implemented` is its
  word for a finished plan. The body of each plan stays as written.
* Bad, because `0001`–`0006` do not grow the strict profile's required
  headings. They stay on the legacy profile until a later record adopts that
  profile for them.
* Bad, because `.markdownlint-cli2.jsonc` carries two deltas, so a refresh
  that expects only the `filter` to differ must preserve the forks glob as well.
* Neutral, because product Go CI, `Makefile` build targets, and `GEMINI.md`'s
  commit sentences stay. `GEMINI.md` is only reflowed enough for the human
  markdownlint rules. It still mentions `GIT_EDITOR=true`, which the new
  `CLAUDE.md` does not use. Replacing that sentence is a separate decision.

## Pros and Cons of the Options

### Install the fleet workspace, move `0001`–`0006` into `docs/decisions/` without renumbering, and keep those records on the legacy lint profile

* Good, because `documentation-writing` already requires a record to sit in
  `docs/decisions/` or `docs/reports/`, and the checker enforces that path.
* Good, because the legacy filter is the fleet's own adoption mechanism for
  records that predate the strict profile.
* Bad, because frontmatter `complete` still has to become `implemented`, and
  four indented code blocks in `0003-PLAN` have to become fences, or the gates
  fail.
* Bad, because `.markdownlint.json` has to be removed. Leaving it makes the
  linter merge a second rule set into the record profile.

### Teach `scripts/check_records.py` to accept records that sit directly in `docs/`, and to accept PLAN status `complete`

* Good, because the twelve files could stay where they are, and `complete`
  could stay the status word already on disk.
* Bad, because the script would no longer `cmp` against the template, and
  every refresh would have to re-apply the fork.
* Bad, because a record filed directly in `docs/` still violates the docs-tree
  rule this install exists to put in place.

### Rewrite `0001`–`0006` onto the strict record profile in this same change

* Good, because one profile would cover every record, and the legacy filter
  could stay empty.
* Bad, because those records would gain `## Consequences`,
  `## Grounding Reference`, `## Prerequisites`, `## Rollback`, and
  `## Completion Criteria` whether or not the original decision used them.
  That rewrites historical structure.
* Bad, because the adoption note for an existing tree says to fix cheap
  defects and then set the legacy filter, not to restructure the records.

### Add `CLAUDE.md` and the pointers, and do not add the checker or the linter

* Good, because the instruction file would exist with almost no edits to
  current markdown.
* Bad, because nothing would fail when a later record used a bad status, a
  broken link, or the wrong directory. A gate that is not installed is not a
  gate.

## More Information

Named deltas, and only these, may differ from the templates:

* `.markdownlint-cli2.jsonc` legacy `filter` is
  `**/000[1-6]-{MADR,PLAN}-*.md`. Pair `0007` is outside that filter and must
  satisfy the strict profile. The filter matches numbers, not the directory,
  so it still applies after the move.
* `.markdownlint-cli2.jsonc` globs gain `!**/internal/forks/**`, for the same
  reason the template already excludes vendored skills: that README is linted
  with the upstream project, not here.
* `.gitignore` is a merge. Append the Python cache entries that are absent
  (`__pycache__/`, `*.py[cod]`). Do not duplicate entries already present.
* `CLAUDE.md` slots and the two `docs/architecture.md` slots are filled from
  the product as it is. There is one docs tree. The Go module is the purpose
  of the repository, not an adjacent stack.
* PLAN status `implemented` on `0003` and `0004` is the vocabulary mapping
  above. No other status word on `0001`–`0006` changes.
* `.markdownlint.json` is removed so it cannot merge over the fleet config.
  Its two rules, dash lists and a line length, are already in the fleet human
  profile (line length 200 there, 120 in the file being removed).
* `.gitattributes` is copied unchanged. Do not `git add --renormalize` the
  product tree in this install. If `git ls-files --eol` does not show
  `attr/text=auto eol=lf` on tracked text without that rewrite, stop.
* `0003-PLAN` indented command blocks become fenced blocks. The command text
  stays. Existing fenced blocks in records are not edited.
* `<!-- markdownlint-disable … -->` comments in `0003`, `0005`, and `0006`
  are removed only when a re-lint of the legacy profile still passes. If a
  comment is still load-bearing, leave it and record that in the plan.

Out of scope: Go sources, `go.mod`, `go.sum`, CI workflows, release installers,
`GEMINI.md` policy text beyond the reflow the linter requires, and any commit.
The uncommitted `0006` pin bump stays untouched except where a scaffold file
is the same path (`Makefile`, `README.md`, and the `0006` record paths, which
move with `git mv` and keep their bytes aside from the edits named above).

Implementation is [0007-PLAN-install-the-fleet-agent-workspace.md](0007-PLAN-install-the-fleet-agent-workspace.md).
