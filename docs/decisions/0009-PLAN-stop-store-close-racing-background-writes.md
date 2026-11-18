---
status: in-progress
date: 2026-11-17
associated-madr: "0009-MADR-stop-store-close-racing-background-writes.md"
---

# Implement the store write gate: Close refuses new writes and waits for admitted ones

Associated MADR:
[0009-MADR-stop-store-close-racing-background-writes.md](0009-MADR-stop-store-close-racing-background-writes.md)
(the owner answered Q1 to Q4 on 2026-11-17 with option 1 of each, as recommended; see the MADR's second ruling).

## Goal

- `Store.Close` can no longer overlap a write the store admitted: new writes are refused with a documented error,
  admitted writes finish first (bounded), and a second `Close` is a no-op.
- `go test -race -count=20 -timeout 30m ./internal/handler/...` passes 5 times out of 5 on a
  tree with this change, run as many times as the baseline in the MADR (R1).
- The contract the race breaks is pinned by tests that fail deterministically on `5e8821e` and pass after: `Close`
  waits for an admitted write (T5), a write after `Close` is refused with `ErrStoreClosing` (T1, T3), `Close` twice is
  safe (T4), and a stress of writers against `Close` sees no panic, no hang and no lost acknowledged write (T2).
- No behaviour a client can see changes, except that a save that meets shutdown now returns an error instead of racing.

## Scope

### In scope

| File | Change |
| :--- | :--- |
| `internal/db/badger.go` | the write gate type, `ErrStoreClosing`, the drain bound, `Store.writing`; `Close` calls the gate first and is idempotent; every `s.DB.Update`, `s.DB.NewWriteBatch` and `s.DB.DropAll` site goes through the gate (sites in the MADR, E14) |
| `internal/db/cache.go`, `internal/db/historical.go`, `internal/db/io_helpers.go` | the four `updateOrWarn(store.DB, ...)` write sites and `store.DB.Update` at `historical.go:28` go through the gate |
| `internal/db/store_close_test.go`, `internal/db/store_closing_helper_test.go`, `internal/db/store_close_bound_test.go` | new: T2 to T5 (P1, written), the helper they share (P1), and T6 (joins P2) |
| `internal/handler/minify_close_refused_test.go` | new: T1 below (P1, written) |
| `docs/guides/services-and-transports.md` | one paragraph in "Readiness and shutdown" |
| `docs/README.md` | the two index rows for this pair (the coordinator adds them) |

### Out of scope

- `internal/handler/background_pool.go` and `cmd/mcp-server-magictools/main.go`: the pool drain is option B in the MADR;
  the owner's ruling on Q1 leaves it out of this record. Phase P3 is therefore not in scope.
- Reads (`DB.View`): Q4 option 1 leaves them alone (MADR, F6).
- The raw `*badger.DB` handed to `internal/prompt_registry/cache.go` through `internal/handler/handlers.go:76`: the gate
  cannot cover a handle that does not go through `Store` (MADR, F3).
- The `ctx.Err()` guard at `internal/handler/proxy_service.go:457-459` and the dangling raw-output footer (MADR, F1).
- Stdio mode's `os.Exit(2)` at `cmd/mcp-server-magictools/main.go:476` (MADR, F2).
- Every file other plans are changing: `internal/handler/list_tools_handler.go`, `handlers.go`, `pipeline_tools.go`,
  `execute_pipeline*.go`, `inventory.go`, `schema_util.go`, `prompts.go`, `go.mod`, `go.sum`, `Makefile`,
  `.github/workflows/ci.yml`. None is touched here, so the change collides with none of them.

## Grounding Reference

- [0009-MADR-stop-store-close-racing-background-writes.md](0009-MADR-stop-store-close-racing-background-writes.md):
  the evidence index (E1 to E20), the measured runs (R1 to R12), the findings (F1 to F8), the options and the open
  questions. Every `path:line` below is at `5e8821e`.
- `internal/db/badger.go:558-581` (`Close`), `:1442-1453` (`SaveRawResource`), `:586-613` (`UpdateWithRetryCtx`),
  `:231-243` (the store's own goroutines already register with `bgWg` and test `closing`).
- Badger v4.9.1 `badger/v4@v4.9.1/db.go:518-550` (`Close`), `badger/v4@v4.9.1/txn.go:806-809` (`Update` refuses a closed handle on entry only),
  `badger/v4@v4.9.1/db.go:883-905` (`sendToWriteCh`).
- `CLAUDE.md`: records, pre-add checks, commits.

## Prerequisites

1. The owner has answered the MADR's open questions (done, 2026-11-17) and the MADR is `accepted` (done); this plan is
   `in-progress`. P0 and P1 are done (Execution Record); P2 onward wait for the owner to commit the record pair and say
   `proceed`.
2. `git rev-parse --short HEAD` is `5e8821e` or a descendant; `git status --short` shows no change to any file in the
   in-scope table other than the owner's own.
3. The 0008 redesign's staged files and the unstaged files of the other plans are left alone. Records are staged by
   path; nothing else.
4. Heavy runs go through `~/.agents/skills/available-dev-hosts/scripts/run-heavy -- <command>`, one at a time.
5. If `HEAD` has moved, re-run the MADR's baseline (at least four whole-package runs) before P1 and amend the MADR.

## Conventions

- Models and effort are priced from the bottom: the cheapest class that can do the phase, and where the strongest tier
  is warranted the phase says why.
- A new test is not trusted until it has been seen to fail on the code it guards (P1) and on a planted break (P4).
- Every edit is made by a script or the edit tool that asserts the old text occurs exactly once, and is read back.
- Nothing committed names a host, an account or a machine path.
- **Commits.** Since 2026-11-17 the owner makes every commit and push. Agents and the coordinator stage only, by path;
  each phase says what to stage. No agent runs `git commit` or `git push`.

## Implementation Steps

### P0, place the records (cheapest class, low effort; the coordinator) DONE 2026-11-17

Add the two index rows to `docs/README.md`, run `python3 scripts/check_records.py --check-all` and `make markdownlint`,
stage the two records and the index by path. The owner commits (first, before P2).

### P1, tests first, seen failing on `5e8821e` (mainstream class, high effort)

Why mainstream and high: the tests must be deterministic and must assert outcomes, not timing. A cheaper class tends to
write a sleep. The strongest tier is not needed: the contract is stated in the MADR and the tests follow from it.

Write, without touching any non-test file:

| ID | Test | File | Asserts | Fails on `5e8821e` because |
| :--- | :--- | :--- | :--- | :--- |
| T1 | `TestMinifyResponse_SaveAfterStoreCloseIsRefused` | `internal/handler/minify_close_refused_test.go` | the pool is parked, `MinifyResponse` queues its save, the store is closed, the pool is released: the save task logs a failure whose error satisfies `errors.Is(err, db.ErrStoreClosing)` | the save returns Badger's `DB Closed` (R11) |
| T2 | `TestStore_WriteDuringCloseNeverPanicsOrHangs` | `internal/db/store_close_test.go` | four writers call `SaveRawResource` in a loop, `Close` runs at a random point, 25 iterations; no panic, no writer outlasts 10 s, every refusal satisfies `errors.Is(err, ErrStoreClosing)`, every nil-returned save is present after reopening the directory | the first refusal is `DB Closed`; and, when the window is hit, a panic or a hang (R5) |
| T3 | `TestStore_WriteAfterCloseReturnsErrStoreClosing` | same | after `Close` returns, `SaveRawResource`, `SaveWithTTL` and `UpdateWithRetry` return an error satisfying `errors.Is(err, ErrStoreClosing)` | they return `DB Closed` |
| T4 | `TestStore_CloseIsIdempotent` | same | a second `Close` returns nil and does not panic | `close(s.closing)` panics on the second call (`badger.go:560`) |
| T5 | `TestStore_CloseWaitsForAnAdmittedWrite` | same | a write blocked inside its transaction function holds `Close` (it has not returned 750 ms after the write was admitted), `Close` returns after the write is released, the write returns nil, and its key is present after reopening | `Close` returns in about 50 ms while the write is in flight |
| T6 | `TestStore_CloseDrainBoundIsHonoured` | `internal/db/store_close_bound_test.go` | a write that never returns does not hold `Close` past the bound (the test shortens an unexported variable to 300 ms); `Close` returns after the bound, not before | not applicable on `5e8821e` (the bound is new); its planted break in P4 is its proof |

The new identifier `ErrStoreClosing` does not exist on `5e8821e`, and a test that names it does not build, which is a
build failure and not a red. So T1 to T5 are written to compile today: the refusal is recognised by one helper in each
package (`isStoreClosing` in `internal/db/store_closing_helper_test.go`, `isStoreClosingRefusal` in
`internal/handler/minify_close_refused_test.go`), which matches the text `store closing` (the text the owner ruled; no
Badger error contains it). P2 changes each helper's body to `errors.Is(err, ErrStoreClosing)`, a one-line edit per
package, and that edit is part of P2's change. T6 joins P2: it shortens the drain bound through a package variable,
which is production code that P2 adds, so it cannot be built or seen failing before P2; the scratch copy of T6 is
kept for P2.

T1 to T5 were run on a `git archive` copy of `5e8821e` with only the new test files added; each fails there (Execution
Record, P1).

Not in this plan, and why: a forced-overlap test that is red under `-race` on `5e8821e`. The window is inside Badger,
between its `blockWrites` check and its send on `db.writeCh`; three such tests and the probe ran 30 or more rounds each
on `5e8821e` without the detector reporting (MADR, R2, R4), so none has been seen to fail and none belongs in the tree.

### P2, the gate (strongest tier, high effort)

Why the strongest tier: what the gate holds across (the transaction function, one attempt, the whole retry loop), what
happens when a write is admitted inside a write, and what bounds `Close` are locking decisions, and a wrong one is a
deadlock at shutdown, which is worse than the race.

1. In `internal/db/badger.go` add `ErrStoreClosing`, the `writeGate` type (a mutex, a closed flag, a counter and an
   idle channel: admission is a counter increment, so a write admitted inside an admitted write cannot deadlock against
   `Close`), `Store.writing()`, and the drain bound as a package variable (so T6 can shorten it).
2. `Close` calls `gate.closeAndWait(bound)` first; a second call returns nil; a timed-out wait logs a warning and
   continues (the old behaviour, now only under a stuck disk).
3. Route every write site in the MADR's E14 through `writing()`: one attempt per admission in `UpdateWithRetryCtx` (the
   gate is released between retries, so a backoff sleep never holds `Close`), `defer end()` elsewhere. The helpers
   `updateOrWarn` and `viewOrWarn` take a `*badger.DB`, not the store: change the four write callers to go through a
   store method rather than widening the helper's signature.
4. Under Q2 (owner: option 1) a refused write returns `ErrStoreClosing`, whose text is `store closing`, wrapped with `%w`
   where a caller adds context. The drain bound is 5 seconds (Q3): a timed-out wait logs a warning and closes.
   Also in P2: change the two helpers of P1 to `errors.Is`, add T6, and move the error-text assertions from the helper's
   text match to the sentinel.
5. `gofmt`, per-file `golint`, `go vet ./internal/db/ ./internal/handler/`; T1 to T6 pass; nothing else changes.

### P3, ~~only if the owner answers Q1 with option 2: drain the pool in `Stop`~~ NOT IN SCOPE (owner's ruling on Q1, 2026-11-17)

Annotation, 2026-11-17: the owner chose the store gate alone; the pool drain is not part of this record. The text below is
kept as written and is not to be executed under this plan. (It was priced strongest tier, medium effort.)

`internal/handler/background_pool.go` gains a pending counter and `DrainBackground(ctx)`;
`cmd/mcp-server-magictools/main.go` `Stop()` calls it, with the bound from Q3, before `app.store.Close()`. A test for the
order (drain, then close, with parked workers). ~~Skipped under Q1 options 1 and 3.~~ Skipped by the ruling.

### P4, a planted break for every new assertion (mainstream class, medium effort)

On a scratch copy of the P2 tree, with `go test -overlay` so the tree is never edited, one break at a time, each seen to
fail the named tests (the prototype's six plants did, MADR R11):

| Plant | Must fail |
| :--- | :--- |
| `enter` never refuses (always admits) | T3, T2 |
| `closeAndWait` returns without waiting | T5 |
| `closeAndWait` waits without a bound (an hour) | T6 |
| `Close` does not call the gate | T2, T3, T5 |
| the second `Close` is not guarded | T4 |
| `SaveRawResource` skips the gate | T3, T1 |

### P5, the docs step, in the same change (cheapest class, low effort)

`docs/guides/services-and-transports.md`, "Readiness and shutdown": one paragraph saying that the store refuses writes
once it is closing, waits (up to the bound) for writes it already accepted, and that a write refused this way is an
error logged at warn level, never a crash. `docs/architecture.md`: check whether it describes the store's lifecycle; if
it does, change it in the same way. `docs/README.md`: the two rows. `make check-records`.

### P6, the gates (cheapest class that can read an exit code, low effort; any agent)

Through `run-heavy`, one at a time, on the tree with P1, P2 and P5:

1. `gofmt -l`, per-file `golint` on every changed Go file, `go vet ./...`.
2. `make lint` with golangci-lint v2.13.1 (CI's current pin) and with v2.15.0 (the version
   [0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md](0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md)
   moves CI to); no new finding.
3. `go test -race -count=20 -timeout 30m ./internal/handler/...` run 5 times, the same number as the MADR's
   baseline, every run exit 0 with no `DATA RACE`.
4. `go test -race -count=3 -timeout 40m ./...`, exit 0, or each failure named and shown to be outside this change.
5. Cross-builds for linux/amd64, darwin/arm64 and windows/amd64 compile.
6. `make check-records`, `make markdownlint`.

### P7, independent review before staging (strongest tier, high effort; a fresh agent that did not write P2)

The change alters shutdown. The reviewer is given the diff, not this plan's conclusions, and is asked: can any path hold
the gate while calling back into the store (a deadlock); can `Close` return while a write is in flight; does a refused
write ever panic; is the drain bound honest; does anything in `internal/db` still reach `DB.Update`, `NewWriteBatch` or
`DropAll` without the gate (a `grep`, listed in the review); do T1 to T5 assert outcomes rather than timing. Findings are
fixed or put to the owner before P8.

### P8, stage and hand over (cheapest class, low effort; the coordinator)

Stage by path: the two records, `docs/README.md`, the files in the scope table. No commit and no push; the owner
commits from the coordinator's staged list. Sequence: this change lands before the 0008 redesign's gate is re-run, then
the Go-pins change
([0006-PLAN-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md](0006-PLAN-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md)),
then the code phases of
[0005-PLAN-make-execute-pipeline-schema-and-handler-agree.md](0005-PLAN-make-execute-pipeline-schema-and-handler-agree.md).

## Verification

- T1 to T5 fail on `5e8821e` (P1) and pass on the P2 tree; each planted break in P4 fails its named tests.
- The whole-package race gate is green 5 of 5 (P6.3); the baseline on `5e8821e` was
  5 of 5 green, so a green gate is evidence only together with the deterministic tests.
- The whole module under `-race -count=3` reports nothing new (P6.4).
- The probe of the MADR's R5 (four writers, `Close` at a random point, reopen and read back) on the P2 tree shows no
  panic, no hung writer and no lost acknowledged write over at least 900 iterations.

## Rollout

One commit by the owner (two if the owner commits the records first). No release, no tag, no restart of any running
server. The change is additive inside `internal/db`.

## Rollback

Revert the commit(s). `Close` returns to its present behaviour, including the race and the second-call panic. Do not roll
back by loosening a test, skipping T1, or catching the panic in the writers: that ships the defect with the gate
silenced.

## Completion Criteria

1. Every phase above is done, or named as skipped by the owner's ruling on Q1 (P3).
2. T1 to T6 are in the tree; T1 to T5 were seen to fail on `5e8821e` (done, P1) and T6 fails under its planted break; every planted break was seen to fail its tests.
3. The gate of P6.3 passed 5 of 5, and the module-wide run is clean or each finding is named.
4. The independent review's findings are closed.
5. The docs step is done and `make check-records` and `make markdownlint` pass.
6. The records are staged for the owner; the MADR's status is `accepted`, and this plan's becomes `implemented` only
   after the owner's commit of the finished change.

## Execution Record

### 2026-11-17, owner's rulings and P0

- The owner answered Q1 to Q4 with option 1 of each and approved the whole plan; the pool drain (P3) is out of scope. The
  coordinator unstaged 0008's files; the index was empty at the start of this step (checked: no staged path).
- P0 done. MADR: status `accepted`; "Decided, second ruling" section with the five labels and the commit rule; Q1 to Q4
  marked answered; "Decision Outcome" now says "Chosen option"; the error text is `store closing` (the prototype's was
  `store is closing`; the measured tallies keep the prototype's text); a section "Conflict of evidence on how often the
  gate fails" records the two red gate runs of the other agent and my five green, with the rate left unestablished, and a
  dated placeholder for the independent reviewer's runs. PLAN: status `in-progress`; P3 annotated out of scope; P1 text
  amended for how T1 to T5 compile today. `docs/README.md` gets the two rows (statuses accepted and in-progress).
- Staged by path: the two 0009 records and `docs/README.md`. Not staged: the P1 test files.

### 2026-11-17, P1: the tests, seen failing on `5e8821e`

Run on a `git archive` copy of `5e8821e` with only the new test files added (`go vet` of `./internal/db/` and
`./internal/handler/` clean there). Failing lines, verbatim:

```text
--- FAIL: TestStore_WriteDuringCloseNeverPanicsOrHangs (T2)
    store_close_test.go:82: iteration 0: 4 writer failures, first: refusal is not ErrStoreClosing: DB Closed
--- FAIL: TestStore_WriteAfterCloseReturnsErrStoreClosing (T3)
    store_close_test.go:115: SaveRawResource after Close returned DB Closed, want an error satisfying isStoreClosing(err)
    store_close_test.go:115: SaveWithTTL after Close returned DB Closed, want an error satisfying isStoreClosing(err)
    store_close_test.go:115: UpdateWithRetry after Close returned DB Closed, want an error satisfying isStoreClosing(err)
--- FAIL: TestStore_CloseIsIdempotent (T4)
    store_close_test.go:128: second Close panicked: close of closed channel
--- FAIL: TestStore_CloseWaitsForAnAdmittedWrite (T5)
    store_close_test.go:155: Close returned (<nil>) while an admitted write was still in flight
--- FAIL: TestMinifyResponse_SaveAfterStoreCloseIsRefused (T1)
    minify_close_refused_test.go:113: the queued save failed with DB Closed, want the store-closing refusal (error text "store closing")
```

- T6 (`TestStore_CloseDrainBoundIsHonoured`) joins P2: it needs the drain bound as a package variable, which is production
  code. Its scratch copy is kept for P2; its red is its planted break in P4.
- T1 is not red under `-race` on `5e8821e`: the detector's report is not reproducible at the gate level (MADR, R2), so
  T1's red is the refusal assertion, not a race report. Its race part is held by the whole-package gate in P6.
- Left unstaged: `internal/db/store_close_test.go`, `internal/db/store_closing_helper_test.go`,
  `internal/handler/minify_close_refused_test.go`.
