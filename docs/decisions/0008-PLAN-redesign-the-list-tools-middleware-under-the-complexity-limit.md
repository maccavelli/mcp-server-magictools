---
status: implemented
date: 2026-11-18
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

### 2026-11-17, P1 committed by the owner

The owner committed and pushed hand-back 1 (P0 and P1) themselves, as `5e8821e` ("test(handler): add characterization
tests for list-tools middleware"), level with the remote `main`. This is the owner's commit, not the coordinator's.
The second hand-back, P2 to P4, starts from that commit.

### 2026-11-17, P2: the redesign and its docs (staged, not committed)

* **The change:** design E, as the owner ruled. `ListToolsMiddleware` keeps its signature and its registration; it
  returns `(&listToolsLayers{h: h, next: next}).handle`. The unexported type holds two fields, the handler and the next
  handler, and nothing a call writes. Methods and functions, with the cognitive complexity golangci-lint v2.15.0
  reports: `handle` 3 (method gate, recover, base, merge), `base` 4, `staticInventory` 3, `mergeSession` 9,
  `listToolsSessionID` 2; `ListToolsMiddleware` and `recoveredListTools` have no branch (0). `CallToolMiddleware` and
  the rest of the file are byte-identical and still score 50.
* **Tests unchanged:** the three existing test files and the two committed characterisation files are not modified.
  All 21 rows and both end-to-end tests pass on the redesigned code, and so does the whole `internal/handler` package
  without `-race`. The only test change is one function added to `list_tools_characterization_test.go` (35 added lines,
  none removed): `TestListToolsFallbackWhileConfigReloads`, the F3 test.
* **F3, red then green:** on `5e8821e`'s unchanged code the F3 test under `go test -race -count=5` fails, `FAIL`, one
  `DATA RACE` report: a write by `OnConfigReloaded` at `handlers.go:147` against a read in the middleware's closure at
  `list_tools_handler.go:57`. With the read lock in `staticInventory` it passes `-race -count=20`, 20 of 20.
* **Lock order, read at every acquisition of `toolsMu`:** five sites, none nested. The writer
  (`handlers.go:146-148`) takes `Lock`, assigns the slice and unlocks: it calls nothing while held. The three readers in
  `align_tools` (`align_tools_resolve_helpers.go:65-66` and `:178-179`, `align_tools_helpers.go:142-146`) take `RLock`
  and call only `strings`, one atomic store (`storeAlignFastPathTrace`) and `toSchemaMap`, which marshals JSON: none
  acquires another lock, and none can reach `tools/list`. The new read takes `RLock`, copies the slice header and
  releases before it iterates; `handle` holds no lock at that point, and nothing is called under it. A goroutine that
  holds the lock and takes nothing else cannot be an edge of a wait-for cycle, and the fallback cannot run under a
  `toolsMu` read lock of its own goroutine, because no site that holds the lock calls into the middleware. Backed by a
  planted break: a read lock that is never released (P29 below) hangs the F3 test, and `-timeout` ends it.
* **The dead guard dropped:** the closure's `if result == nil` in the recover block could not be false: the named result
  is assigned only by the final `return`, after every point at which a panic can occur. The recover now returns the
  empty list directly. P27 (the recover returns a nil result) fails L17 to L20, so the rows would see it; nothing
  depends on the dropped branch.
* **Preserved, with the row that fails if it changes:** F1 (session id looked up as `""`): L10, E-1; F2 (a panic
  discards the base tools): L17, L18, L20; F4 (cursor and metadata dropped): L16; F5 (fallback raw and reduced): L12,
  L13; F6 (no cap in the middleware): L09; F7 (`err = nil` explicit): L19; F9 (`CallToolMiddleware` untouched): no row,
  held by the byte-identical file tail.
* **F8:** the stale comment at `handlers.go:161-163` is rewritten (`handlers.go:161-164`): it said a method that does
  not exist would be used; it now says what happens to the refreshed inventory.
* **Docs:** doc comments on the type and every method; a dated annotation after the evidence table of the 0005 MADR
  pointing E11 and its sentence at `list_tools_handler.go:103-131`, `:133-141` and `:84-101`, the original text left
  as written. **Deviation from this plan's text, by the coordinator's brief for this hand-back:** step 4 said to
  strike through the old span; nothing was struck, only a line was added. `docs/architecture.md` and the guides do not
  describe the middleware. Open: the 0005 PLAN's sequencing note cites `list_tools_handler.go:55-68` and `:82-95`,
  which moved; that plan's own P2 re-derives every cited line, so it is left to it.

### 2026-11-17, P3: planted breaks of the redesigned code (scratch copies, none in the tree)

* 28 plants, one at a time, each `gofmt`-clean and each run against the 21 rows, the two end-to-end tests and the F3
  test. P01 of P1's list ("the recover stops clearing `err`") cannot be re-formed: in the new structure `err` is never
  assigned before the recover, which is finding F7; its job is covered by P17 and P14. P02 to P24 are P1's plants
  re-formed; P25 (result memoised across calls), P26 (per-call tools kept on the type) and P27 (the recover returns a
  nil result) are new; P28 removes the read lock; P29 never releases it.
* **Every plant failed a named row; no survivors.** P02 L07; P03 L13; P04 L14; P05 L05; P06 L04; P07 L17; P08 L10;
  P09 L01; P10 L05; P11 L16; P12 L17; P13 L13; P14 L12; P15 L05; P16 L05; P17 L17; P18 L07; P19 L03; P20 L05; P21 L08;
  P22 L05 and L21; P23 E-2; P24 L14 (by a side effect only: E-2 compares the registered tools with themselves); P25 L04;
  P26 L10, E-1 and E-2; P27 L17 to L20. P28 (lock removed) fails the F3 test under `-race`. P29 (lock never released)
  hangs the F3 test and ends only at `-timeout`.

### 2026-11-17, P4: the gates, and a stop (staged, not committed)

All on a `git archive` of `5e8821e` plus this change; the working tree, which holds the 0006 change, was not used.

| Gate | Exit | Result |
| :--- | :--- | :--- |
| `gofmt -l cmd internal` | 0 | empty |
| `go vet ./...` | 0 | clean |
| `go mod tidy -diff` | 0 | no diff |
| `make lint`, v2.13.1, Go 1.26.6 | 0 | `0 issues.` |
| `make lint`, v2.15.0, Go 1.26.6 | 2 | one finding, `pipeline_tools.go:161:33`; the gocognit finding is gone |
| `make lint`, v2.15.0, Go 1.27.4 | 2 | the same one finding |
| cross-builds, `CGO_ENABLED=0`, linux and windows | 0, 0 | compile |
| `go test -count=1 -timeout 20m ./...` | 0 | every package ok |
| `python3 scripts/check_records.py --check-all` (17:26:39) | 0 | records, pairing, links: no error |
| `markdownlint-cli2`, the repository's config (17:26:40) | 0 | 42 files, 0 issues |
| `go test -race -count=20 -timeout 30m ./internal/handler/...` | **1** | **one `DATA RACE`, in `TestMinifyResponse_PreMarshalParity`, unrelated to this change; see below** |
| tools/list tests, `-race -count=20 -timeout 30m` | 0 | no race; the F3 test 20 of 20 |

* **The stop.** The full-package race run failed once: a background task of `MinifyResponse` writing through
  `Store.SaveRawResource` (`proxy_service.go:461`) races with `Store.Close` in the test's deferred cleanup
  (`minify_golden_test.go:105`). Nothing in it touches `tools/list` or any file of this change. Evidence that it is
  pre-existing: a pristine `git archive` of `5e8821e`, with no 0008 change at all, under the same command fails the
  same way once (`TestMinifyResponse_Golden` this time, the same two stacks); a targeted 300-count run of the two
  tests involved did not reproduce it on either tree; earlier whole-package runs of this plan, on `310a541` and on
  `8f8cc99` plus the P1 tests, passed. So it is an intermittent race between a background write and `Store.Close` in
  the handler tests, in code this plan does not change. It was not re-run until green, skipped or suppressed. The
  required gate is not met as written; the options are the owner's (reported with this hand-back).

### 2026-11-17, the independent review of P2 to P4, and the pass that answered it (not committed)

* **Verdict:** no blocker. The review (strongest tier, a fresh reviewer) ran a differential harness of its own, the old
  closure copied verbatim and run against the new code, 2,134 cases, with 0 differences. The F3 race reproduced on the
  base (26 reports) and is gone on the redesign. 35 of its 37 plants failed a test. The `tools/list` wire output through
  `Register` is byte-identical between the two trees. Complexity as recorded above.
* **Survivors, verbatim:** `B20 recover: logged at warn, not error: SURVIVED (pass)`; and B35, "read lock held across
  the build of the tools". B35 cannot be seen by behaviour: a lock held a little longer changes no output. It is held
  by the doc comment of `staticInventory`, which says only the slice header is copied under the lock and nothing is
  called while it is held, and by the review of lock order above.
* **S1 (should-fix), answered.** Row L17 checked the message, `panic=` and `stack=` but not the level. It now also
  requires exactly one recovery record and that record at `level=ERROR`, and exactly one `ERROR` record in the capture.
  It passes on the committed closure and on the redesign, so the committed code does log exactly one ERROR record. Two
  plants on scratch copies of both trees fail it: "logged at warn" (`the recovery record is not at ERROR level:
  … level=WARN …`, `list_tools_characterization_test.go:472`) and "logged twice" (`want exactly one recovery record,
  got 2`, `:469`). **This changes a row of an already-committed test file; it is a strengthening: no assertion was
  loosened, and the old checks stay.**
* **N1, answered.** `base`'s doc comment said anything but a list result with non-nil `Tools` falls back. A typed nil
  `*mcp.ListToolsResult` from `next` does not: reading its `Tools` panics and the recover answers with the empty list
  (the same on the committed closure and on the redesign). The comment now says so. A new row pins it, **L22** ("next
  returns a typed nil list result: the nil dereference is recovered, not the fallback"), seen passing on the committed
  closure and on the redesign; the plant "a typed nil result is caught and takes the fallback" fails it
  (`list_tools_characterization_test.go:529`) on both. The characterisation set is now L01 to L22, E-1 and E-2, and the
  header comment of the test file says L22.
* **N2, a follow-up, not fixed here.** Registration's sanitiser edits the inventory's own schema maps in place, so the
  fallback's schemas are raw only in part: reproduced here, 16 of 18 equal the inventory's raw JSON after `Register`
  and 18 of 18 after `OnConfigReloaded` (which unmarshals the inventory afresh). That makes F5 and point 5 of the MADR's
  section on what 0005 relies on only partly accurate: the fallback is raw after a reload and for 16 tools at startup,
  and not for the other two. It goes to the F5 follow-up record. The annotation added to the 0005 MADR states nothing
  about the fallback's schemas, so nothing in it is made false and it is not amended.
* **N3, a named follow-up record, not fixed here.** `handlers.go:198` and `:206` (`Register`'s log line and the loop
  in `addTool`; these numbers are those of the scratch tree, `3443524` plus the files of this change, which carries one
  more comment line: at `3443524` itself they are `:197` and `:205`) read `h.InternalTools` without `toolsMu`, while the config watcher is already running
  (`main.go:389` starts it, `main.go:446` calls `Register`): the same class as F3, at startup. It was not folded into
  F3: F3 is the middleware's own read, the owner's ruling is to fix that one race here, and N3 would need edits to
  `Register` and `addTool`, which this plan does not otherwise change. The owner's ruling stands as made; if the owner
  wants N3 fixed with it, that is an amendment, put to them.
* **N4 and N6** are named by the review; their text was not passed to this pass and is not described here. **N5**,
  answered below.
* **N5, answered by annotation.** P2 step 1 of this plan names the F3 test `TestListToolsFallbackAndConfigReload`; the
  committed name is `TestListToolsFallbackWhileConfigReloads`, which the P2 entry above uses. Nothing is rewritten; the
  P4 table above gained its two missing rows, `check_records.py --check-all` and markdownlint, with the results of the
  P4 run.
* **The gate deviation, as it stands.** The required `go test -race -count=20 -timeout 30m ./internal/handler/...` was
  red once in this plan's own P4 run and once on a pristine copy of `5e8821e`, on a race unrelated to this change
  (a `MinifyResponse` background write against `Store.Close`). The owner ruled (2026-11-17; the label was "Fix that race
  first under its own short record, then re-run the gate (Recommended)"): that race is fixed first, under
  `0009-MADR-stop-store-close-racing-background-writes.md`, and this gate is re-run after its fix lands. The evidence so
  far on how often it happens, stated without resolving it: red twice (this plan's two runs), green five of five (the
  investigator of 0009, on `5e8821e`), green six of six (the reviewer: three on the base, three on the redesign). Until
  the re-run, this plan's race gate is not met as written.
* **Who commits.** Addition, the evening of 2026-11-17: the owner restored the coordinator's commits and pushes to the
  default branch. The line above that says the owner makes them, from the coordinator's staged list, held until then;
  neither is rewritten. The coordinator stages and commits this change after 0009's fix has landed and the gate has
  been re-run; the five files of this change are in the working tree, unstaged, until then.
* **Light gates after this pass** (the full set of P4 is re-run after 0009 lands): in the next entry.

### 2026-11-17, light gates after the review pass (a `git archive` of `3443524` plus the five files; not committed)

| Gate | Exit | Result |
| :--- | :--- | :--- |
| `gofmt -l cmd internal` | 0 | empty |
| `go vet ./internal/handler/` | 0 | clean |
| `make lint`, v2.13.1, Go 1.26.6 | 0 | `0 issues.` |
| `make lint`, v2.15.0, Go 1.26.6 | 2 | one finding, `pipeline_tools.go:161:33`, and nothing else |
| `make lint`, v2.15.0, Go 1.27.4 | 2 | the same one finding |
| `python3 scripts/check_records.py --check-all` | 0 | no error |
| `markdownlint-cli2`, the repository's config | 0 | 0 issues |
| tools/list tests, `-race -count=20 -timeout 20m` (the existing files, L01 to L22, E-1, E-2 and the F3 test) | 0 | `ok`, 0 `DATA RACE`; L-set and F3 test 20 of 20 |

The full set of P4, including `go test ./...` and the whole-package race run, is re-run after 0009's fix lands. This
pass did not touch that gate.

### 2026-11-17, N3 by the owner's ruling: stopped before building, on a finding; N4 and N6 recorded

* **The ruling.** The owner ruled on N3 (2026-11-17, relayed by the coordinator; the label was "In 0008, by amendment:
  take the same lock in registration (Recommended)"): the same lock on the same inventory in registration, a few lines,
  with a race test seen failing first, closing the class while the code is open; the cost named was that 0008 touches
  two more functions than first approved. **Annotation of the entry above:** its bullet saying N3 is "a named
  follow-up record, not fixed here" is superseded by this ruling and is not rewritten. Nothing of N3 is in the code yet,
  for the reason below.
* **The stop (a deviation: stopped before choosing, as the brief required).** The brief asked whether readers of the
  inventory's elements race registration's in-place sanitising, and to stop with three options if they do. They do.
  `addTool` passes each inventory element's schema map to `util.SanitizeToolSchema`, which writes into that same map
  (`internal/util/schema_util.go:28`, `:49`, `:57`: the missing `type`, and `props[k] = sanitizeMap(pm)` for every
  nested property, on every call, changed or not). A read lock on the slice cannot protect those maps: the readers'
  own reads of them (the fallback hands the schema to the SDK, which marshals it; `align_tools` calls `toSchemaMap`,
  which returns the same map) happen after the lock is released.
* **What was measured** (scratch copies of `3443524` plus the five files, `-race -count=3`; none of it is in the tree):
  * Registration against a writer replacing the inventory (`OnConfigReloaded`, one goroutine), with no registration
    lock: **95 `DATA RACE` reports, exit 1**; the first is a write at `handlers.go:147` against a read at
    `handlers.go:206` (`addTool`, reached from `registerProxyTools`); the sites involved are `:141`, `:147`, `:195`,
    `:206`, `:207`, `:212`, `:213`, `:220`, `:226`. All of these `handlers.go` numbers are those of that scratch tree;
    at `3443524` the ones from `:195` on are one lower.
  * The same with option 1 below (a copy of the slice header taken under the read lock in `Register` and `addTool`, the
    copy iterated, the lock released before `AddTool`): **0 reports, exit 0**.
  * Four readers marshalling the fallback's schemas while registration runs, with option 1 in place: **61 reports and a
    `fatal error: concurrent map iteration and map write`**, exit 1; the earlier run without option 1 gave 49. The write
    side is `schema_util.go:57` through `addTool`'s call at `handlers.go:220`.
  * Reachability: in the binary the config watcher starts at `main.go:389` and `Register` runs at `main.go:446`, before
    the server serves at `:452` or `:454`, so no reader of the schema maps runs during registration; the slice race with
    the watcher is the one that is live. The map race needs a request during registration, which only a test makes.
* **Lock order, for any of the options that take the lock.** `AddTool` of the SDK takes the server's own mutex and never
  `toolsMu`, and nothing in it calls back into a middleware (middleware runs only when a request is handled), so a lock
  held across it would not cycle; the options below do not hold it across it anyway: the header is copied and the lock
  released before the loop and before `AddTool`.
* **The three options put to the owner** (nothing chosen): (1) the lock as ruled, copy and iterate, release before
  `AddTool`, with the race test, and a doc comment on `addTool` saying it writes to the inventory's schema maps and is
  safe only because registration completes before serving, no behaviour change, the map race left documented and
  unreachable in the binary (recommended); (2) the lock and sanitise a deep copy of the schema, so inventory elements
  are never written after loading: closes the map race too, but the fallback would then emit all 18 schemas raw where
  it emits 16 raw and 2 sanitised after `Register` today (a visible change, the F5 follow-up's territory), about
  fifteen more lines and a test, not built or tested; (3) leave N3 as the earlier follow-up record and build nothing
  here.
* **N4 (the review's, recorded and corrected).** The review's text: "The 0005 annotation is accurate but uses two
  conventions. It is an addition only: 9 lines added, 0 removed. `mergeSession :103-131` and `listToolsSessionID
  :133-141` include their doc comments; `staticInventory :84-101` leaves out its doc comment at `:79-83`. Every range
  contains its function. The 0005 PLAN's citations (`:55-68`, `:82-95`) are stale, which the author left open for 0005's
  own P2." **Done:** the annotation in the 0005 MADR is not in `HEAD` (it is this change's, uncommitted), so it was
  corrected in place: all three ranges now run from the doc comment to the closing brace and were re-derived from the
  file after this pass's edits: `mergeSession` `:104-132`, `listToolsSessionID` `:134-142`, `staticInventory`
  `:80-102`, and the sentence says so. They move again only if `list_tools_handler.go` does. The 0005 PLAN's
  stale citations stay open for 0005's own P2.
* **N6 (the review's, recorded).** "Pre-existing and preserved: when the base slice has spare capacity, the middleware
  appends session tools into `next`'s own result array. This is harmless because the SDK builds a new slice per call."
  Found and preserved. It is pinned, on purpose, by a new row, **L23**, seen passing on the committed closure and on the
  redesign; the plant "the base slice is copied before the merge" fails it on both
  (`list_tools_characterization_test.go:533`, "the result no longer shares the base slice's backing array"; it also
  fails L14, line 417, because an empty base slice becomes nil). A later copy of the base slice would be harmless and
  would change this row by intent. The set is now L01 to L23, E-1 and E-2.

### 2026-11-17, light gates after row L23 (a `git archive` of `3443524` plus the five files; not committed)

The same eight gates as the previous light-gate entry, run again after the last Go change (row L23 in the test file),
each from its own start to its own end with the exit status taken before any filter: `gofmt -l cmd internal` 0, empty;
`go vet ./internal/handler/` 0; `make lint` with v2.13.1 on Go 1.26.6 0, `0 issues.`; with v2.15.0 on Go 1.26.6 and on
Go 1.27.4 exit 2 each, the one finding `pipeline_tools.go:161:33` and nothing else; `check_records.py --check-all` 0;
`markdownlint-cli2` 0, 0 issues; the tools/list tests (the existing files, L01 to L23, E-1, E-2 and the F3 test) under
`-race -count=20 -timeout 20m` through the heavy-run queue 0, `ok`, 0 `DATA RACE`, L-set and F3 test 20 of 20. The
whole-package race gate and the rest of P4 still wait for 0009's fix. The N3 lock is not in this run, because it is
not built (see the entry above).

### 2026-11-17, N3 built as the owner ruled, and the entry above resolved (not committed)

* **The stop above is resolved.** The owner chose option 1 (label "The lock as ruled; document the map writes
  (Recommended)"; the first ruling's label was "In 0008, by amendment: take the same lock in registration
  (Recommended)"). The MADR carries a dated amendment. The earlier entry's bullet calling N3 "a named follow-up record"
  stays as written and is superseded.
* **The test first, red.** `TestRegisterWhileConfigReloads` (new file `internal/handler/register_inventory_lock_test.go`):
  one goroutine calls `OnConfigReloaded` in a loop while `Register`, `RegisterPipelineTools` and `RegisterPrompts` run 20
  times on new servers. On `HEAD` plus the earlier files and no lock, `go test -race -count=5`: **FAIL in 5 of 5 runs, 65
  `DATA RACE` reports**, the first: `Write … by OnConfigReloaded() handlers.go:147` against `Previous read … by addTool()
  handlers.go:206` (that scratch tree's number; `:205` at `3443524`), reached from `registerProxyTools` and `Register`.
  Some reports (44 lines of the output name the
  sanitiser) are the writer's unmarshal of the new inventory against registration's sanitising of it, unordered because
  the slice was published without the lock; the lock orders them.
* **How the test avoids the documented map race.** It opens no session and calls neither the `tools/list` middleware nor
  `align_tools`, so no reader of the schema maps exists during registration. The only other goroutine is the writer, which
  unmarshals a fresh inventory, publishes it under the lock and never touches an element afterwards; `Register` copies the
  header under the same lock, which orders the writer's building of the elements before registration's sanitising of them.
  A race between registration's map writes and a reader needs a reader, and there is none. It is one writer because
  concurrent `OnConfigReloaded` calls race with each other in `syncSearchGates`. Without `-race` it cannot fail.
* **The lock, green.** `Register` reads the length under `RLock` before its log line; `addTool` copies the header under
  `RLock`, iterates the copy and releases before the loop and before `AddTool`. `go test -race -count=20` of the new test:
  exit 0, 0 reports, 20 of 20.
* **Lock order.** `AddTool` takes the SDK server's own mutex, never `toolsMu`, and calls no middleware, so there is no edge
  from it; the lock is released before the loop and before the call, so nothing is held across it. Readers of `toolsMu`
  elsewhere (`align_tools_helpers.go:142-146`, `align_tools_resolve_helpers.go:65-66` and `:178-179`, the fallback) hold it
  only around their own loops and never while registering.
* **Reachability of the map race, answered from the code.** The readers of the inventory's elements in non-test code are
  `align_tools_helpers.go:143-145`, `align_tools_resolve_helpers.go:67` and `:181` (with `toSchemaMap` at `:210`) and the
  `tools/list` fallback: all run only while a client request is handled (`AlignTools`, `proxy_handlers.go:97`, and the
  middleware). The config watcher's reload (`handlers.go:140-148`) builds a fresh slice by unmarshalling the inventory
  JSON and assigns it; it never reads an element of the old slice. Nothing started before `Register` reads them: the
  watcher (`main.go:389`) only calls `OnConfigReloaded`; the server's `InitializedHandler` runs on a client's connection;
  `startBackgroundWorkers` and `executeBootSequence` run from it (`main.go:438-439`) or from `startHTTPMode` (`:514`), after
  `Register`, and read the inventory JSON afresh (`main.go:1461`), not `h.InternalTools`; `NewHandler`'s own unmarshal
  (`handlers.go:92`) runs before any goroutine. Serving begins after `Register` returns (`main.go:446`, then `:452` or
  `:454`). So the premise holds; nothing stopped this part.
* **Plants** (scratch copy, `-race -count=2`, each `gofmt`-clean, against the registration test and the tools/list tests):
  the lock removed from `Register`'s read: fails `TestRegisterWhileConfigReloads`, 1 report; the lock removed from
  `addTool`'s read: fails it, 173 reports; the lock taken and never released in `addTool`: a hang that the 90 s `-timeout`
  ends (exit 1, `panic: test timed out`); **the lock held across the SDK's `AddTool`: survived**, every test passes, with
  `-race` on the tools/list and registration tests and without it on the whole package. No test can see it: the SDK's
  `AddTool` never takes `toolsMu`, so a read lock held across it delays only a writer and breaks nothing. It is held by the
  doc comment of `addTool` (the same standing as B35).
* **Wire comparison.** The reviewer's five-scenario probe (`tools/list` through `Register`, `RegisterPipelineTools` and
  `RegisterPrompts` over an in-memory session: not synced; synced, no cache; cache under the empty id with a name clash;
  cache under `active-session` only; wrong-typed cache under the empty id) on three trees: the committed closure, the
  redesign without the lock, the redesign with it. All three outputs are identical (10 lines, 47,677 bytes), and equal to
  the reviewer's own `wire_base.txt` and `wire_new.txt`.
* **Unchanged:** the three existing test files; L01 to L23, E-1, E-2 and the F3 test pass; `CallToolMiddleware` is untouched.
* **Gates** are in the next entry.

### 2026-11-17, light gates after the registration lock (a `git archive` of `3443524` plus the 0008 files; not committed)

| Gate | Start-end | Exit | Result |
| :--- | :--- | :--- | :--- |
| `gofmt -l cmd internal` | 20:49:05-20:49:05 | 0 | empty |
| `go vet ./internal/handler/` | 20:49:05-20:49:08 | 0 | clean |
| `make lint`, v2.13.1, Go 1.26.6 | 20:49:08-20:49:31 | 0 | `0 issues.` |
| `make lint`, v2.15.0, Go 1.26.6 | 20:49:31-20:49:40 | 2 | one finding, `pipeline_tools.go:161:33`, nothing else |
| `make lint`, v2.15.0, Go 1.27.4 | 20:49:40-20:49:53 | 2 | the same one finding |
| `python3 scripts/check_records.py --check-all` | 20:49:53-20:49:53 | 0 | no error |
| `markdownlint-cli2` | 20:49:53-20:49:53 | 0 | 0 issues |
| tools/list tests and `TestRegisterWhileConfigReloads`, `-race -count=20 -timeout 20m` | 20:49:53-20:50:28 | 0 | `ok`, 0 `DATA RACE`; L01 to L23, the F3 test and the registration test each 20 of 20 |

Also, before this entry: the registration test alone under `-race -count=20`, 20:42:21-20:42:29, exit 0, 0 reports. The
whole-package race gate and the rest of P4 still wait for 0009's fix. The Go change of this pass is `handlers.go`
(the lock, the two doc comments) and the new test file; `list_tools_handler.go` is unchanged since the entry above, so
the ranges in the 0005 annotation still hold.

### 2026-11-17, the second independent review, and the four corrections it asked for (not committed)

* **Verdict:** no blocker. The reviewer's own race test went from 107 reports on `HEAD` to none with the lock. The premise
  of the owner's ruling holds: `Register` is called only at `main.go:446`, once per process, before anything is served;
  the HTTP mode uses one server for every connection; nothing at request time calls `Register` or `addTool`; nothing
  started earlier reads the schema maps. The wire output is byte-identical; the differential harness again found 0
  differences in 2,134 cases.
* **Survivors, verbatim:** `R06 addTool: read lock held across the SDK's AddTool: SURVIVED (pass)`, held by the doc
  comment of `addTool`, since `AddTool` never touches `toolsMu` (this entry's own plant LK3 gave the same result);
  `T01 L17 level checks dropped, with R07: SURVIVED (pass)` and `T02b L17 exactly-one (len) and ERROR-count checks dropped,
  with R08: SURVIVED (pass)`. The last two are the reviewer's controls: with the level check and the count check dropped
  and the matching plant applied, nothing else fails, which shows that those checks are what catch the plants.
* **N-a, corrected in place** (the amendment and these entries are uncommitted): the amendment cited the two registration
  reads as `handlers.go:198` and `:206` "at `3443524`". At `3443524` they are `:197` and `:205` (the reviewer's `HEAD` race
  stack shows `:205`); `:198` and `:206` belong to the scratch tree, which carries one more comment line. The amendment
  now gives the numbers for `3443524` and says which tree the others belong to, and the three PLAN places that quote the
  scratch numbers say so (`:147` is the same in both).
* **N-b, done.** `addTool`'s doc comment named the fallback and `align_tools` as readers of the schema maps but not the SDK
  itself: `Server.AddTool` keeps the tool, and with it the same inventory map, and the SDK's `tools/list` marshals it on
  every call. Request time only, so the conclusion stands. Verified in the module cache (read-only):
  `go-sdk@v1.6.1/mcp/server.go:277` is `st := &serverTool{tool: t, handler: h}` (the review and brief said `:276`; it is
  `:277`); `remarshal` is called at `:255`, marshals and unmarshals into a fresh map, so it only checks a copy; the base
  `tools/list` appends `t.tool` at `:738`. The comment and the amendment's new reachability bullet say so.
* **N-c, done.** Row L17's last check counted every `ERROR` record that any goroutine logged while it ran, and the
  package's background pool logs through the same logger, so it could fail on records that are not the recovery's. It now
  counts the recovery records at `ERROR` only; the two checks before it (exactly one recovery record, and that record at
  `ERROR`) were already enough for the plants. The plants still fail it: "logged at warn" at
  `list_tools_characterization_test.go:473` (`the recovery record is not at ERROR level … level=WARN …`) and "logged
  twice" at `:470` (`want exactly one recovery record, got 2`). A control, an unrelated `ERROR` record logged next to the
  recovery record, does not fail it.
* **N-d, kept, by the coordinator's decision** (the coordinator's, not mine, and the owner is told): row L23 pins an
  aliasing no client can observe, since the SDK builds a fresh slice for every call, and a later defensive copy of the base
  slice would fail it as a regression. The decision is to keep the row, because the owner ruled every characterisation row
  in and it costs nothing while the redesign is verified against it, and to say so: its comment and this entry state
  that **L23 documents an implementation detail, not a contract**: a later change may remove the aliasing, and the row with
  it, without a decision record. This annotates the earlier entry's wording that the row was "pinned, on purpose".
* **The follow-up records now owed, none drafted, none fixed here:** F1, the session-id lookup; F2, the panic path; F4, the
  dropped cursor and metadata; F5, with N2 and the schema-map writes of registration; and **F-new**, a live race on every
  config reload that this change does not touch: `syncSearchGates` writes `h.Store.SearchGates` and `h.Store.Fusion` as
  plain fields (`handlers.go:105`, `:111`) while every search reads them unlocked (`internal/db/search_fusion_helpers.go:40`,
  `:263`, `:296-297`, `:346-352`, `:397`; `pipeline_swarm_helpers.go:70`), shown under `-race` with one writer; and two
  reloads can overlap, since `handleChange` (`internal/config/watcher.go:174`) is called from three places (`:69`, `:97`,
  `:111`; the review says three goroutines) and holds its lock only around the hash check. (The assignments at `handlers.go:105` and `:111`, the field reads
  at the db sites named and the `handleChange` calls were read by me; the race itself is the review's finding, and the
  concurrent writer in this plan's own tests shows the same two fields racing: it is why those tests use one writer.)

### 2026-11-17, light gates after the second review's corrections (a `git archive` of `3443524` plus the 0008 files; not committed)

| Gate | Start-end | Exit | Result |
| :--- | :--- | :--- | :--- |
| `gofmt -l cmd internal` | 21:54:49-21:54:49 | 0 | empty |
| `go vet ./internal/handler/` | 21:54:49-21:54:50 | 0 | clean |
| `make lint`, v2.13.1, Go 1.26.6 | 21:54:50-21:55:01 | 0 | `0 issues.` |
| `make lint`, v2.15.0, Go 1.26.6 | 21:55:01-21:55:07 | 2 | one finding, `pipeline_tools.go:161:33`, nothing else |
| `make lint`, v2.15.0, Go 1.27.4 | 21:55:07-21:55:15 | 2 | the same one finding |
| `python3 scripts/check_records.py --check-all` | 21:55:15-21:55:15 | 0 | no error |
| `markdownlint-cli2` | 21:55:15-21:55:16 | 0 | 0 issues |
| tools/list tests and `TestRegisterWhileConfigReloads`, `-race -count=20 -timeout 20m`, through the heavy-run queue | 21:55:16-21:55:43 | 0 | `ok`, 0 `DATA RACE`; L01 to L23, the F3 test and the registration test each 20 of 20 |

The changes since the previous gates are a comment in `handlers.go` (N-b), row L17's last check and row L23's comment in
the test file (N-c, N-d), and the records. The whole-package race gate and the rest of P4 still wait for 0009's fix, which
is in the working tree (staged by another change) and was not used: these gates ran on `3443524` plus the 0008 files only.

### 2026-11-18, the gate re-run on `bfcd269`, the deviation closed, and the criteria (staged, not committed)

* **The deviation is closed.** The full-package race gate had failed on a race in the store, not in this change (the P4
  entry of 2026-11-17). The owner ruled that it be fixed first under its own record (the label was "Fix that race first
  under its own short record, then re-run the gate (Recommended)"). That record is
  0009-MADR-stop-store-close-racing-background-writes.md; its fix is commit `94e1de4`, and its plan is closed at
  `bfcd269`. Every entry above that says this plan's race gate is "not met as written" or "waits for 0009's fix" (the
  P4 entry's stop, the gate-deviation bullet of the first review entry, and three light-gate notes) is superseded by this entry and is not
  rewritten.
* **What was run, and on what.** Every gate below ran on a `git archive` of `bfcd269` plus exactly the seven files of
  this change; the working tree, which holds other records' uncommitted work, was not used. None of the seven files
  changed between `3443524` and `bfcd269`, so the change applies as it was, and it builds.
* **The new restriction.** `CLAUDE.md` now carries an interim rule: three tests in `internal/client`, and a fourth that
  scans every process, run the orphan reaper against the real machine, so on a machine with a live service nothing runs
  `go test ./...`, `internal/client`, `make test`, `make race`, or a built binary. Allowed here, with a throwaway home:
  the `internal/handler`, `internal/db` and `cmd` packages, and build, vet, lint and the parse-only guard. This entry's
  runs follow it: no run on the session machine touched `internal/client`, a whole-module package list or a binary. The
  whole module ran once, under `-race`, on the Linux machine the owner confirmed has no live service (2026-11-18, the
  label "No live magictools server there: use it (Recommended)"). **Disclosure:** this plan's own P4 run of 2026-11-17,
  `go test -count=1 -timeout 20m ./...` at 16:49:57-16:50:20 on the session machine (the P4 table above), was a
  whole-module run of the class the rule now bars. It was run before the rule existed; it is recorded here so that
  whoever holds the logged service deaths of that day can compare times. This plan does not know whether it killed
  anything.
* **Gates on the new base** (start-end are local times of 2026-11-18, the exit status taken before any filter):

| Gate | Start-end | Exit | Result |
| :--- | :--- | :--- | :--- |
| `gofmt -l cmd internal` | 14:37:00-14:37:00 | 0 | empty |
| `go vet ./internal/handler/` | 14:37:00-14:37:02 | 0 | clean |
| `go build ./...`, native; `CGO_ENABLED=0 GOOS=linux`; `CGO_ENABLED=0 GOOS=windows` | 14:37:02-14:37:14 | 0, 0, 0 | compile |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 1 of 3, through the heavy-run queue | 14:37:14-14:42:35 | 0 | `ok` 316 s, 0 `DATA RACE` |
| the same, run 2 of 3 | 14:42:35-14:47:27 | 0 | `ok` 290 s, 0 `DATA RACE` |
| the same, run 3 of 3 | 14:47:27-14:53:09 | 0 | `ok` 340 s, 0 `DATA RACE` |
| tools/list and registration tests, `-race -count=50 -timeout 30m` | 14:53:09-14:53:50 | 0 | `ok`; L01 to L23, the F3 test and the registration test each 50 of 50, 0 `DATA RACE` |
| `go test -count=1 ./internal/handler/... ./internal/db/... ./cmd/...`, throwaway home | 14:53:50-14:54:17 | 0 | all three `ok` |
| `make lint`, v2.13.1, Go 1.26.6 | 14:54:17-14:54:47 | 0 | `0 issues.` |
| `make lint`, v2.15.0, Go 1.26.6 | 14:54:47-14:54:57 | 2 | one finding, `internal/handler/pipeline_tools.go:161:33`, which is record 0006's; the gocognit finding is gone |
| `make lint`, v2.15.0, Go 1.27.4 | 14:54:57-14:55:05 | 2 | the same one finding |
| `python3 scripts/check_records.py --check-all` | 14:55:05-14:55:06 | 0 | no error |
| `markdownlint-cli2` | 14:55:06-14:55:06 | 0 | 0 issues |
| the whole module, `go test -race -count=1 -timeout 40m ./...`, on the Linux machine, through `run-on-host` and its queue | 19:58:21-19:59:43 UTC | 0 | every package `ok`, 0 `DATA RACE` |

* **The Linux run.** The first line the script printed was `Linux x86_64`, so it ran on native Linux (the placement
  excluded the WSL distros for a race run, and the target's platform was linux/amd64); the marker listing printed `no
  marked process and no magictools process on this host`, and the run then went ahead. A throwaway home was used. The
  three handler race runs are the gate that failed on the store race: all three were green, none re-run to get there.
* **Plants on the new base** (scratch copies of the same tree, handler package only, throwaway home, each `gofmt`-clean):
  the 26 plants P02 to P27 of the redesigned code all failed a named row, as on `3443524`; the review pass's plants
  failed as before ("logged at warn" L17 at `list_tools_characterization_test.go:473`, "logged twice" L17 at `:470`, "a
  typed nil result is caught" L22 at `:558`, "the base slice is copied before the merge" L23 at `:542`); P28 (the F3
  lock removed) failed the F3 test under `-race`; P29 (the lock never released) hung and ended at its `-timeout`. The
  registration-lock plants under `-race -count=2`: the lock removed from `Register`'s read failed
  `TestRegisterWhileConfigReloads` (1 report); the lock removed from `addTool`'s read failed it (149 reports); the lock
  never released in `addTool` hung and ended at the timeout. **Survivors, verbatim from the runners:** `LK3 the lock held
  across the SDK's AddTool: exit=0 DATA RACE reports=0 timed_out=False failing=NONE (survived)` and `B35 read lock held
  across the build of the fallback's tools: exit=0 reports=0 timed_out=False failing=NONE (survived)`. Both are held by the doc comments, since a lock held a
  little longer than needed changes no output and `AddTool` never takes `toolsMu`; they are the reviewer's R06 and B35.
  One of these extra plant runs (the B35 one, `-race -count=2` on the handler tests) was run directly on the session
  machine instead of through the heavy-run queue, an oversight of mine; it was a short handler-only run.

* **The completion criteria, one by one.** Nothing is claimed beyond what is recorded; the status stays `in-progress`.

| Criterion | State | Where the evidence is |
| :--- | :--- | :--- |
| 1. P1's tests exist, pass on `310a541`'s code and failed under every plant | met; committed as `5e8821e` | the P1 entries; L17, L22 and L23 were added later and are in this change |
| 2. The redesign is committed; no function above 9; signature, registration and exported surface unchanged; no existing test file changed | **open: the commit.** The rest met | the P2 entry (scores: handle 3, base 4, staticInventory 3, mergeSession 9, listToolsSessionID 2); the surface and wire comparisons; `git diff` touches none of the three pre-existing test files |
| 3. Every row passes unchanged on the redesign; every plant of P3 failed a named row | met | the P3 entry; the plants and the gate table above (rows L01 to L23, E-1, E-2, the F3 test and the registration test) |
| 4. `make lint` clean for `list_tools_*` under v2.13.1 (Go 1.26.6) and v2.15.0 (Go 1.26.6 and 1.27.4); race run and cross-builds pass | met on `bfcd269` | the gate table above; the one v2.15.0 finding left is record 0006's |
| 5. The independent reviews' findings are closed or put to the owner | met | the entries on the first and second review: S1, N1 to N6, N-a to N-d answered; N2, N3's map writes, F1, F2, F4, F5 and F-new named as follow-up records, none fixed here |
| 6. The 0005 MADR's evidence row E11 points at the new location | met in the staged change; **lands with the commit** | the dated annotation in the 0005 MADR, ranges `:104-132`, `:134-142`, `:80-102` of `list_tools_handler.go` |
| 7. The owner's word for each commit and push is on record; status becomes `implemented`; execution record complete | **open: the commit and the push, and the status change after them.** The execution record is complete to this entry | P1 was committed by the owner; the coordinator's commit of this change is pending |

Remaining, and not this plan's to do: the coordinator's commit of the seven staged files and the status change after
it (criteria 2, 6 and 7), and the native CI jobs on a push. This repository's checker does not accept `complete` for a
plan; the status that follows the commit is `implemented`, as record 0009's close-out found. The index row of
`docs/README.md` needs no change until then.

### 2026-11-18, committed and pushed as `1fe5ce7`; CI green; the close-out

* **The commit.** `1fe5ce7`, "refactor(handler): split list-tools middleware under complexity limit", on `main`: the seven
  files this plan staged, 787 insertions and 78 deletions: `internal/handler/list_tools_handler.go`,
  `internal/handler/handlers.go`, `internal/handler/list_tools_characterization_test.go`,
  `internal/handler/register_inventory_lock_test.go` (new), and the three records, the 0005 MADR (the dated annotation),
  the 0008 MADR (the dated amendment) and this plan. It was committed and pushed by the coordinator, under the commit
  and push authority the owner restored on the evening of 2026-11-17.
* **The coordinator's own gate, before the commit,** on a copy of the index: on the session machine,
  `./internal/db/...` and `./internal/handler/...` under `-race -count=20`, no race reported, and `go vet` of both; on
  the Linux machine, after the read-only marker listing printed `no marked process and no magictools process on this
  host`, the whole module under `go test -race -count=1`: 19 packages `ok`. These repeat, independently, the gates
  recorded in the previous entry.
* **CI on `1fe5ce7`:** the CI run succeeded, so the native jobs of the other platforms, which no run of this plan could
  make, are green too.
* **The completion criteria, one by one.**

| Criterion | State | Where the evidence is recorded |
| :--- | :--- | :--- |
| 1. P1's tests exist, pass on `310a541`'s code and failed under every plant | met | the P1 entries (committed by the owner as `5e8821e`); the plants table of that entry |
| 2. The redesign is committed; no function above 9; signature, registration and exported surface unchanged; no existing test file changed | met | `1fe5ce7`; the P2 entry (scores handle 3, base 4, staticInventory 3, mergeSession 9, listToolsSessionID 2, `CallToolMiddleware` untouched at 50); the surface and wire comparisons in the N3 and review entries; the three pre-existing test files are not among the commit's seven files |
| 3. Every row passes unchanged on the redesign; every plant of P3 failed a named row | met | the P3 entry; the plants of the entry of 2026-11-18 on `bfcd269` (P02 to P29 and the registration-lock plants, survivors named) |
| 4. `make lint` clean for `list_tools_*` under v2.13.1 (Go 1.26.6) and v2.15.0 (Go 1.26.6 and 1.27.4); race run and cross-builds pass | met | the gate table of the entry of 2026-11-18; the one v2.15.0 finding left in the module is record 0006's; the coordinator's gate and CI above |
| 5. The independent reviews' findings are closed or put to the owner | met | the entries on the first review (S1, N1 to N6) and the second (N-a to N-d), each answered; the follow-ups below are the findings put to the owner as records still owed |
| 6. The 0005 MADR's evidence row E11 points at the new location | met | the dated annotation in the 0005 MADR, in `1fe5ce7`, with the three ranges of `list_tools_handler.go` that follow one convention |
| 7. The owner's word for each commit and push is on record; status `implemented`; execution record complete | met | the owner committed and pushed P0 and P1 themselves (`5e8821e`); the owner's approval of the whole plan and of its commits, and their restoring of the coordinator's commits and pushes, are recorded in the second Decided list of the MADR and the entries above, and `1fe5ce7` was made under it; this entry completes the execution record |

* **Status.** Every criterion is met, so the status becomes `implemented` (this repository's record check does not accept
  `complete` for a plan) and the date 2026-11-18. The MADR stays `accepted`. Nothing remains for this plan to do; what
  follows is owed by later records.
* **The follow-up records this plan names as owed.** None is drafted and none is fixed here; each is for the owner to
  commission. For a later author, the evidence is recorded in the 0008 MADR (its findings, rows and amendment) and in
  this plan, as follows. Line numbers are those of `1fe5ce7`.

| Follow-up | What it is | The rows that change by intent, and the evidence |
| :--- | :--- | :--- |
| F1, the session-id lookup | `listToolsSessionID` looks the session cache up under `""`, which is what stdio and in-memory sessions report, while the handshake and `align_tools` store it under `active-session`; so over stdio no proxied tool reaches `tools/list` | rows L10, L11 and E-1; the MADR's F1 and its section on what 0005 relies on (point 4); `safeSessionID` at `internal/handler/proxy_handlers.go:92` already maps `""`; the SDK's `ioConn.SessionID` is `""` (`go-sdk@v1.6.1/mcp/transport.go:441`); fixing it adds 20 to 30 tools to every stdio client's list, and changes the seeding key of 0005's tests |
| F2, the panic path | any panic in `tools/list`, including a typed-nil session or a wrong-typed cache, discards the tools already gathered and returns the empty list | rows L17, L18, L19, L20 and L22; `recoveredListTools`; the MADR's F2 |
| F4, the dropped cursor and metadata | the result is a new object holding only `Tools`, so a base result's `NextCursor` and `Meta` are lost; latent while the base pages at 1000 tools and 18 are registered | row L16; the MADR's F4; `go-sdk@v1.6.1/mcp/server.go:36` |
| F5, with N2 and the schema-map writes | the fallback emits name, description and schema only, and the schema is the inventory's own map, raw after a reload and sanitised for 2 of 18 tools after `Register` (16 of 18 raw, then 18 of 18 after `OnConfigReloaded`); registration's `SanitizeToolSchema` writes into those maps in place (`internal/util/schema_util.go:28`, `:49`, `:57`), which is safe only because registration finishes before serving (the doc comment of `addTool`); sanitising a copy would remove the write and make the fallback emit all 18 raw | rows L12 and L13 (and E-2 for the base path); the MADR's F5, the N2 and N3 notes and its amendment; a scratch test that served during registration gave 49 reports and a `concurrent map iteration and map write` fatal error |
| the config-reload race on the search gates, with overlapping reloads | `syncSearchGates` assigns `h.Store.SearchGates` and `h.Store.Fusion` as plain fields (`internal/handler/handlers.go:105`, `:111`) while searches read them unlocked (`internal/db/search_fusion_helpers.go:40`, `:263`, `:296-297`, `:346-352`, `:397`; `internal/handler/pipeline_swarm_helpers.go:70`); and `handleChange` (`internal/config/watcher.go:174`) is called from three places (`:69`, `:97`, `:111`) and holds its lock only around the hash check, so two reloads can overlap | not touched by this change and held by no row; shown under `-race` by the review with one writer, and seen in this plan's first probe when `OnConfigReloaded` was called concurrently (which is why the registration and fallback race tests use one writer) |
