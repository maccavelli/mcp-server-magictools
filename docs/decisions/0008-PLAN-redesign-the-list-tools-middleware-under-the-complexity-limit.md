---
status: in-progress
date: 2026-11-17
associated-madr: "0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md"
---

# Implement the ListToolsMiddleware redesign

Associated MADR:
[0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md](0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md)

## Goal

`internal/handler/list_tools_handler.go`'s `ListToolsMiddleware` is a small type with one method per layer, no
function in the file's redesigned part scores above 9 under golangci-lint v2.15.0 (limit 50), and every row of the
MADR's behaviour table behaves exactly as it does at `310a541`, held by tests that were written and seen passing
and seen failing under planted breaks before the code changed. `ListToolsMiddleware`'s signature and its
registration (`internal/handler/handlers.go:187`) do not change. The change passes CI's current linter on the current
Go pins and v2.15.0, so the Go-pins record can follow it.

## Scope

### In scope

| Item | Files |
| :--- | :--- |
| Characterisation tests for the 21 rows and the two end-to-end tests (MADR, behaviour table) | new `internal/handler/list_tools_characterization_test.go`, `internal/handler/list_tools_register_test.go` |
| The redesign (the owner's answer to Q1; this plan is written for E) | `internal/handler/list_tools_handler.go`, `ListToolsMiddleware` and its new helpers only |
| The unlocked read of `InternalTools` and the stale comment, if Q2 is answered 1 | `internal/handler/list_tools_handler.go`; `internal/handler/handlers.go:161-163` (the comment only) |
| A race test for the unlocked read, if Q2 is answered 1 | `internal/handler/list_tools_characterization_test.go` |
| The docs step: doc comments on the new type and methods; one dated annotation in the 0005 MADR's evidence row E11 and the sentence that cites it | `internal/handler/list_tools_handler.go`; `0005-MADR-make-execute-pipeline-schema-and-handler-agree.md` |

### Out of scope

* Any change to what a client sees: the session-id lookup (F1), what a panic returns (F2), the dropped cursor (F4),
  the unsanitised fallback (F5) stay as they are unless the owner answers Q2 otherwise. They are pinned by tests.
* `CallToolMiddleware` (F9, Q4) and the other v2.15.0 finding, `internal/handler/pipeline_tools.go:161:33` (F10), which
  is the Go-pins record's.
* The Go pins, the CI workflow, the Makefile, `go.mod`, `go.sum`, the fork's `go.mod`, `pipeline_tools.go` and its
  tests, the README and the platform guide: other agents' uncommitted work. This plan stages none of them.
* The session cache's admission behaviour (F6) and `align_tools`.
* Any build, install, restart or call of the owner's running server.

## Grounding Reference

* The MADR's behaviour table (rows L01 to L21, E-1, E-2), evidence index (E1 to E19, R1 to R5) and findings (F1 to F10),
  all at `310a541`.
* The prototypes and measurements, kept in the drafting scratch folder and not committed: the characterisation tests
  as written and seen passing on `310a541`; the 24 planted breaks of the current code and their results (existing tests
  fail 6, the new rows fail all 24); designs A to F; the per-function scores; the benchmark; the 24 planted breaks of
  design E.
* `0005-MADR-make-execute-pipeline-schema-and-handler-agree.md` and its PLAN: this plan lands first and amends one
  citation of it.
* `0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md` and its PLAN (staged): they follow this
  plan.

## Prerequisites

* The owner has answered the MADR's open questions Q1 to Q4 and approved this plan. **No commit and no push is
  authorised until then, and this repository is under no standing approval:** every commit and every push waits for
  the owner's word for this plan. Tags and releases need a separate word.
* The scratch folder is a `git archive` of the commit being built on, never the working tree, which holds other
  agents' uncommitted changes.
* The heavy-run host rules: a full-module `go test ./...`, any `-race` run and the whole-module `make lint` go through
  `~/.agents/skills/available-dev-hosts/scripts/pick-host` and `run-on-host` (`--race` for a race run), or, on the
  session host, `run-heavy -- <command>`, one at a time per host. A race run carries an explicit `-timeout`. A
  single-package `go test` without `-race` may stay on the session host.
* Two linters exist for the gates: CI's current v2.13.1 built with Go 1.26.6, and v2.15.0, each built with
  `GOBIN=<scratch>/bin go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@<version>`; their `version`
  lines are kept with the evidence. v2.13.1 cannot type-check under Go 1.27.4 (the Go-pins record's E10), so the
  current-pins gate runs under `GOTOOLCHAIN=go1.26.6`.
* Before the first commit: `git rev-parse --path-format=absolute --git-path hooks` is the user-global hooks directory
  or a wrapper that chains to it (it was at drafting); otherwise stop.

## Conventions

* **K1. Characterise first.** No line of `list_tools_handler.go` changes until P1's tests are committed, green on the
  unchanged code.
* **K2. A planted break per assertion**, applied only to a scratch copy, each `gofmt`-clean, each seen to fail the named
  row; the first lines of the failure are kept. Nothing is planted in the working tree.
* **K3. Existing tests are read-only.** The redesign's diff touches no existing `*_test.go`.
* **K4. The docs step is in the same commit as the code it describes** (P2).
* **K5. Evidence.** Full output of every gate goes to a scratch file; the exit status is taken before any filter; a
  pipeline never stands between a check and the decision that uses its status.
* **K6. Deviations stop the phase,** are recorded here with a date, and in the MADR when a decision or a fact changes.
  No workaround: no loosened assertion, skipped test, widened limit or `//nolint`.
* **K7. Commits.** The coordinator commits each phase with `git commit --no-edit` after the phase's gates pass, the
  pre-add checks (`gofmt`, and `make lint` on the staged Go) and the disclosure guard have run. The agent doing a
  phase stages only that phase's files, by path. Nothing in a committed file carries a hostname, an account name or
  a real-machine absolute path.
* **K8. No contact with the owner's running server,** no read of a credential or a `*.devops.*.json` file, no request
  to a provider.

## Implementation Steps

Prices: model class (lightweight, mainstream, strongest) and effort (low, medium, high), cheapest first. The strongest
tier is spent on the one phase where judgment dominates and a miss is expensive: the independent review of the path
every `tools/list` takes (P5). The test-writing and the redesign are mechanical against a written table and a
prototype, so mainstream medium is enough; the net of 24 planted breaks is what catches an error, not the model.

### P0, place the records

Lightweight, low.

1. The owner's answers to Q1 to Q4 are recorded in the MADR (`status: accepted`, an Open-questions line per answer,
   the date) and this plan's status becomes `in-progress`.
2. The coordinator commits the two records on their own, after `python3 scripts/check_records.py --check` and
   `markdownlint-cli2` pass on them.

Files: `docs/decisions/0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md`, and this plan.

### P1, characterisation tests on the current code

Mainstream, medium. Files: `internal/handler/list_tools_characterization_test.go`,
`internal/handler/list_tools_register_test.go`. Test files only.

1. Add the two files. The first holds one function per row, L01 to L21, driven by a table so no function scores above
   the limit, and the in-memory-session test (E-1); the second holds E-2, which registers `Register`,
   `RegisterPipelineTools` and `RegisterPrompts` as `cmd/mcp-server-magictools/main.go` does and lists over an
   in-memory session. The in-memory session's id is `""`, so the session cache is seeded under `""` there, as the rows
   say (E-1). The file names and row ids are the MADR's, so a reader can hold the table against the tests.
   Two points the tests must keep: the session cache is filled with each tool added twice (a key added once leaves the
   cache's small queue at the next new key, F6), and E-2 takes the expected JSON of the cached tool before the call, so
   a sanitiser that edits in place cannot hide itself.
2. Run them on the unchanged code, with the command below. Every row passes. Keep the output.

   ```text
   go test -count=1 -v ./internal/handler/ \
     -run 'TestListToolsCharacterization|TestListToolsOverInMemorySession|TestListToolsThroughRegister'
   ```

3. **Planted breaks of the current code** (K2): one scratch copy of `310a541` with the two files, 24 plants applied one
   at a time to `list_tools_handler.go`, each followed by the same `go test`; every plant must fail at least one row,
   and every row must fail under at least one plant. The rows each plant failed in the prototype run are in the table
   after step 4.

4. Gates for this commit: `gofmt -l internal/handler` empty; `go vet ./internal/handler/`; the repository's lint
   config over `./internal/handler/...` under `GOTOOLCHAIN=go1.26.6` with v2.13.1 (expect `0 issues.`), and with v2.15.0
   (expect exactly the two findings that exist at `310a541`: gocognit at `list_tools_handler.go:19:1` and gocritic at
   `pipeline_tools.go:161:33`, and nothing in the new files: the prototype produced no finding); the
   whole `internal/handler` package passes; statement coverage of `ListToolsMiddleware` is 100%.

The plants of step 3 and the rows each must fail:

| Plant | Fails |
| :--- | :--- |
| P01 the recover block stops clearing `err` | L19 |
| P02 the dedup is removed | L07 |
| P03 the fallback stops skipping nil inventory entries | L13 |
| P04 the fallback also triggers on an empty non-nil list | L14 |
| P05 the default session id changes | L05, L11, L15, L18 |
| P06 the sync gate is inverted | L04, L05, E-1, E-2 |
| P07 the panic counter is not incremented | L17, L18 |
| P08 `""` is mapped to `active-session` | L10, E-1, E-2 |
| P09 other methods go through the list path | L01, L02 |
| P10 merged session tools are copied | L05 |
| P11 the base result's cursor is kept | L16 |
| P12 the recover is removed | L17 |
| P13 the fallback drops the input schema | L13 |
| P14 a `next` error no longer triggers the fallback | L12 |
| P15 the merge reads the wrong cache id | L05, L10, E-1 |
| P16 session tools come before base tools | L05, L10, E-1 |
| P17 the recover returns an error | L17, L18, L19, L20 |
| P18 nil cache entries are not skipped | L07 |
| P19 `next` gets a different context | L03 |
| P20 the merge reads another session | L05, L06, L10, E-1 |
| P21 nil entries of the base result are dropped | L08 |
| P22 the middleware writes to the session cache | L21 |
| P23 session tools are re-sanitised on the way out | E-2 |
| P24 base tools are re-sanitised on the way out | L14, by a side effect (an empty list becomes nil); E-2 compares the registered tools with themselves, so a re-sanitise that leaves them unchanged is not detectable and is not claimed |

Exit: the commit holds tests only, they pass on the unchanged code, every plant failed its named row. CI's current
linter stays green on it.

### P2, the redesign and its docs step

Mainstream, medium. Files: `internal/handler/list_tools_handler.go`; with Q2 answered 1, also
`internal/handler/handlers.go:161-163` (the comment only) and one test in
`internal/handler/list_tools_characterization_test.go`; and the 0005 MADR's annotation.

1. **With Q2 answered 1 and F3's test first:** add `TestListToolsFallbackAndConfigReload`, which runs one goroutine
   calling `OnConfigReloaded` and four calling the middleware on the fallback path under `-race` (the prototype probe
   shape; a single writer, because concurrent `OnConfigReloaded` calls race with each other in `syncSearchGates`,
   `handlers.go:105`, which is not the question). Written against the redesigned code before the lock is added; run
   under `-race` through the heavy-run queue and **seen failing with a data race on `InternalTools`**; keep the first
   lines of the report. Then add the read lock below.
2. Replace `ListToolsMiddleware`'s body with the design the owner chose. For E: `ListToolsMiddleware` returns
   `(&listToolsLayers{h: h, next: next}).handle`; the type holds only `h` and `next` and is never written after
   construction; `handle` keeps the method gate and the recover block and calls `base` then `mergeSession`; `base`
   calls `next` and falls back to `staticInventory`; `mergeSession` takes and returns the slice; `listToolsSessionID`
   and `recoveredListTools` are plain functions. The recover returns an empty non-nil list and a nil error and
   increments the counter and logs as before. The dropped `if result == nil` guard is a deliberate removal of a branch
   that cannot run (the named result is assigned only by the final return); it is named in the commit's description
   and in P5's checklist. With Q2 answered 1, `staticInventory` takes `toolsMu.RLock`, copies the slice header and
   releases the lock before iterating (design F). With Q1 answered A or C, use the prototype named in the MADR
   instead; the rows and gates are the same.
3. `CallToolMiddleware` and everything below it in the file are byte-identical.
4. **Docs step, same commit** (K4):
   * Doc comments on the type and each method: what the layer does, that the type is immutable after wiring and what a
     future edit must not add to it (per-call state), that session tools come from the cache as stored and are not
     sanitised, copied or capped here.
   * With Q2 answered 1, rewrite the stale comment at `handlers.go:161-163`, which names a method that does not exist.
   * In `0005-MADR-make-execute-pipeline-schema-and-handler-agree.md`, strike through the span
     `list_tools_handler.go:70-96` in evidence row E11 and add the new location (the file and the line range of
     `mergeSession`, taken from the committed code), and add the same correction to the sentence that cites E11 in its
     section on the schema's two union branches. Each annotation carries the date and names this record by filename.
     Nothing else in that record changes. This amendment is covered by the owner's approval of this plan.
   * The guides and the README do not describe the middleware (searched at `310a541`: the only mentions are the
     prompts' middleware at `docs/guides/mcp-tools.md:100-103`, which this change does not touch).
5. Gates for this commit: the whole `internal/handler` package passes with no existing test file modified
   (`git diff --stat` for the phase names `list_tools_handler.go`, the docs and the new test, and no existing
   `*_test.go`); the characterisation tests pass unchanged; per-function gocognit under v2.15.0, from the
   all-functions configuration, is at most 9 for the redesigned functions (design E's prototype: handle 3, base 4,
   staticInventory 3, mergeSession 9, listToolsSessionID 2); `go doc -all ./internal/handler` is identical to the
   previous commit's (the exported surface does not change); coverage of every new function is 100%.

Exit: one commit with the code, the race test, the comments and the 0005 annotation.

### P3, planted breaks of the redesign

Mainstream, medium. A scratch copy of the P2 commit; no repository change.

1. 24 plants of the redesigned source, one at a time, the same list as P1 translated to the new names (the prototype
   list for design E: dedup removed, nil skips removed, fallback on an empty list, default session id changed, sync gate
   inverted, panic not counted, `""` mapped, other methods through the list path, tools copied, recover removed,
   schema dropped, `next` error not triggering the fallback, wrong id, wrong session, session tools first, nil cache
   entries kept, wrong context, nil base entries dropped, cache written, session tools re-sanitised, recover returning
   an error, and two the type must rule out: the result memoised across calls, and tools kept on the type across
   calls). Each must fail at least one row of P1's tests; keep the first lines of each failure.
2. With Q2 answered 1: the lock plant (the read lock removed) must fail the race test of P2 step 1 under `-race`.
3. `gofmt` on each plant before it is run; the scratch copy is deleted afterwards.

Exit: every plant failed a named row. A plant that passes is a deviation (K6): the row is too weak and P1 is
amended before anything is committed.

### P4, the repository's gates

Mainstream, low. Heavy runs one at a time per host (Prerequisites).

1. `gofmt -l cmd internal` empty; `go vet ./...`; `go mod tidy` leaves `go.mod` and `go.sum` unchanged (the working
   tree's own changes to them belong to another record: run it in the scratch copy of the phase's commit).
2. `CGO_ENABLED=0 go test ./...` through `pick-host` then `run-on-host`.
3. `go test -race -count=20 -timeout 20m ./internal/handler/...` through `run-heavy` (or `run-on-host --race`): exit 0,
   no `DATA RACE` in the output. The baseline at `310a541` was exit 0 in 313 s (MADR, R2).
4. `make lint` on the scratch copy of the phase's commit, whole module, through the queue, with `GOLANGCI_LINT` set to
   each binary: v2.13.1 under `GOTOOLCHAIN=go1.26.6` (expect `0 issues.`); v2.15.0 under go1.26.6 and under go1.27.4
   (expect one finding, `pipeline_tools.go:161:33`, and none in `internal/handler/list_tools_*`). Keep each `version`
   line.
5. Cross-builds, in the scratch copy so no `dist/` file is written to the tree: `CGO_ENABLED=0 GOOS=linux GOARCH=amd64
   go build ./...`, `GOOS=darwin GOARCH=arm64`, `GOOS=windows GOARCH=amd64`.
6. `python3 scripts/check_records.py --check` and `markdownlint-cli2` on the records and on the edited 0005 MADR.

Exit: every gate green with its output kept.

### P5, independent review

Strongest tier, high. A fresh reviewer who has not seen this plan's reasoning or the MADR's recommendation, given the
two commits' diffs, the repository at the parent commit, and the MADR's behaviour table as a specification to check
against (not as a conclusion). It reads for:

* every row of the table against the new code, by reading, not by the tests;
* that the removed `if result == nil` branch cannot run, and that every panic path still ends in an empty list, a nil
  error, one counter increment and one error log with the stack;
* that the type holds nothing a call writes, and that no goroutine or cache outlives a call;
* that the base call, the fallback and the merge run in the same order, with the same logs, as before;
* the lock (Q2 answered 1): no lock held across a call to `next`, no nested lock, no deadlock with `OnConfigReloaded`;
* that no existing test file changed, that `CallToolMiddleware` is byte-identical, and that the exported surface is
  unchanged;
* that the 0005 annotation is accurate and changes nothing else, and that no comment says more than the code does.

Findings are closed or put to the owner. The coordinator does not wave a gap through (a reported "not verified" is
closed or put to the owner).

### P6, commits and sequence

Coordinator. P0, P1 and P2 are three commits, in that order, each after its gates (K7); P3 to P5 produce no commit
but their output is attached to P2's handoff. A push waits for the owner's word. After P2 is pushed the Go-pins
change (0006) may land, with the linter at v2.15.0 and this finding gone, and after it the code phases of 0005, which
find the annotated evidence row. If the owner answers Q2 with 3, this plan is amended first: F1's fix changes rows L10
and E-1 and the 0005 plan's seeding key.

## Verification

* P1: 23 rows green on the unchanged code; 24 plants, each failing a named row.
* P2: no existing test changed; the characterisation tests unchanged and green; per-function scores at most 9;
  surface identical; coverage 100% of the new functions.
* P3: every plant of the redesign failed a named row; with Q2 answered 1 the lock plant failed under `-race`.
* P4: the gates with their output kept; both linters; the race run; the cross-builds.
* A citation check over both records (every `path:line` resolves at `310a541` and the line range says what the
  record claims), seen to fail on a planted false citation in a temporary copy.
* `markdownlint-cli2` and `scripts/check_records.py --check` on both records and the edited 0005 MADR.
* A disclosure scan of the records and the diff against the owner's deny list.

## Rollout

Source, tests and docs in three commits (P0, P1, P2) on the owner's word. No migration, no data change, no
configuration change. Nothing reaches the owner's running server until the owner builds, installs and restarts it;
this plan does not do that, and a client's `tools/list` is unchanged by design (the rows). Sequence: this plan, then
0006's code, then 0005's code phases.

## Rollback

* P2's commit is reverted with `git revert` on the owner's word; the tests of P1 stay and pass on the reverted code
  (they were written against it). The 0005 annotation reverts with it.
* P1's commit is reverted separately if the tests themselves are the problem.
* If a defect in the redesign reaches a built binary: the owner reinstalls the previous binary (the Makefile's install
  keeps none, so the owner keeps one first) and the source is reverted as above.
* No stored data or format is touched, so there is nothing to migrate back.

## Completion Criteria

1. P1's tests exist, pass on `310a541`'s code and failed under every plant.
2. The redesign is committed; no function in it scores above 9 under v2.15.0; `ListToolsMiddleware`'s signature and
   registration and the package's exported surface are unchanged; no existing test file changed.
3. Every row of the behaviour table passes unchanged on the redesign; every plant of P3 failed a named row.
4. `make lint` is clean for `list_tools_*` under v2.13.1 (Go 1.26.6) and v2.15.0 (Go 1.26.6 and 1.27.4); the race run
   and the cross-builds pass.
5. The independent review's findings are closed or put to the owner.
6. The 0005 MADR's evidence row E11 points at the new location.
7. The owner's word for each commit and push is on record; this plan's status becomes `complete` (`implemented` in
   the vocabulary of `scripts/check_records.py`, whichever the repository's checker accepts when the work lands) with an
   execution record: what each phase did, each gate's output, every plant's failure, every deviation, and what was not
   done and why.

## Execution Record

### 2026-11-17, approval and the terms of commits

* The owner accepted the MADR and approved this plan (the MADR's second Decided list, items 2 to 6): design E; fix only
  the F3 data race and the stale comment here; all 23 characterisation tests as their own commit first; leave
  `CallToolMiddleware` alone. This plan is `in-progress`.
* **Addition, 2026-11-17, after an account change:** where this plan says the coordinator commits each phase (K7, P0
  step 2, P6), from 2026-11-17 the owner makes the commits and the pushes themselves, from the coordinator's staged
  list of files by path, until the owner says otherwise. No agent and not the coordinator commits or pushes. Nothing
  above is rewritten; the gates that precede a commit (the pre-add checks and the disclosure guard) still run before
  the owner commits.
* The phases are executed in two hand-backs: P0 and P1, then P2 to P4 after P1 is committed by the owner.
* Gates of this plan run on a `git archive` of the commit being built on plus the phase's own diff, never on the
  working tree, which holds another record's uncommitted change (0006).

### 2026-11-17, P1: the characterisation tests (tests only)

* **What:** `internal/handler/list_tools_characterization_test.go` (rows L01 to L21, one function per row, and
  `TestListToolsOverInMemorySession`, row E-1) and `internal/handler/list_tools_register_test.go`
  (`TestListToolsThroughRegister`, row E-2). No production file changed. The F3 race test is not in this change: it
  fails on the unchanged code, which is the defect, and goes with P2's lock.
* **Seen passing on the unchanged code:** a `git archive` of `8f8cc99` plus the two files (the working tree was not
  used): all 21 rows and both end-to-end tests pass. `internal/handler` is identical to `310a541`, the code the MADR
  describes, apart from the two new files.
* **Planted breaks of the current code:** 24, one at a time, each applied to a scratch copy of that archive; each
  failed at least one named row, and every row failed under at least one plant. The first failing row and its test line
  for each are kept with the evidence (scratch); P01 to P24 are the table in P1 step 3.
* **Gates (run once each, in full, on the same archive plus the two files):** `gofmt -l` empty; `go vet` clean; whole
  module `make lint` with golangci-lint v2.13.1 built with Go 1.26.6 (CI's current linter on the current pins):
  `0 issues.`; with v2.15.0 built with Go 1.26.6 and with Go 1.27.4: exactly the two findings that exist at `310a541`
  (gocognit at `list_tools_handler.go:19:1` and gocritic at `pipeline_tools.go:161:33`) and none in the new files;
  `go test -race -count=20 -timeout 20m ./internal/handler/...` through the heavy-run queue: `ok`, exit 0, no data
  race reported. The gate run was queued before a usage-limit stop and ran once, start to finish, after it; nothing
  was cut short and nothing was re-run.
* **Deviations:** none.
