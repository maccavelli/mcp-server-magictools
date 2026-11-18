---
status: implemented
date: 2026-11-18
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
| `internal/db/store_close_gate_test.go` | new (P2, 2026-11-17): every write site refused while `Close` waits, a refused write not retried, a nested admission, two `Close` calls at once, reads not gated |
| `internal/db/store_close_recover_test.go` | new (P2, 2026-11-17, third ruling): the recovery past the bound on the three iterator paths and on a site without an error result; five cases in which the panic must propagate; the panic-origin test |
| `internal/db/metrics_flush_helpers.go` | the post-flush refresh starts through the gate (fourth ruling, 2026-11-17) |
| `internal/db/store_close_starts_test.go` | new (fourth ruling, 2026-11-17): `Close` against the four goroutine starts, the refused starts, a start admitted before `Close`, a started goroutine's write while closing |
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
- *Annotation, 2026-11-17 (evening):* the owner restored the coordinator's commits and pushes to the default branch.
  Agents still stage by path and do not commit; the bullet above stood until then.

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

*Annotation, 2026-11-17 (P2):* the T2 line above quotes `DB Closed`, but the library's first refusal text varies between
runs. The P2 re-run on `3443524` printed `store_close_test.go:82: iteration 0: 4 writer failures, first: refusal is not
ErrStoreClosing: Writes are blocked, possibly due to DropAll or Close`. Either text is the red.

- T6 (`TestStore_CloseDrainBoundIsHonoured`) joins P2: it needs the drain bound as a package variable, which is production
  code. Its scratch copy is kept for P2; its red is its planted break in P4.
- T1 is not red under `-race` on `5e8821e`: the detector's report is not reproducible at the gate level (MADR, R2), so
  T1's red is the refusal assertion, not a race report. Its race part is held by the whole-package gate in P6.
- Left unstaged: `internal/db/store_close_test.go`, `internal/db/store_closing_helper_test.go`,
  `internal/handler/minify_close_refused_test.go`.

### 2026-11-17, owner's commit rule restored

- As of the evening of 2026-11-17 the owner restored the coordinator's commits and pushes to the default branch (the
  Conventions bullet and the MADR's commit rule are annotated, not rewritten). Agents still stage by path only.

### 2026-11-17, P2 stopped, and the deviation the third ruling resolved

- **Stop.** Before building, P2 found that a write admitted before closing and still running when `Close`'s wait runs
  out can panic once Badger is closed: deterministically when it opens an iterator (`badger/v4@v4.9.1/iterator.go:463-464`,
  3 of 3 scratch runs), and in a narrow commit window ("send on closed channel", `badger/v4@v4.9.1/db.go:884`, `:904`
  against `:538`, `:550`). Nothing was built; three options went to the owner.
- **Resolution (the owner, 2026-11-17).** "Catch a panic only in a write that outlived the wait (Recommended)". Recorded
  in the MADR ("Decided, third ruling"). The recovery is in addition to the gate, never instead of it.
- **What it adds to this plan.** The recovery in the write path; its tests (`store_close_recover_test.go`, scope table);
  its plants (P4 below). The test seam it needs: a per-store hook, `txnStartHook`, run at the start of each transaction
  function of `UpdateWithRetryCtx` only when a test sets it (one atomic load otherwise).
- **A second departure from P2's letter, called out here.** P2 step 1 says "the drain bound as a package variable (so
  T6 can shorten it)". The change keeps the bound a constant, `storeDrainTimeout` (5 s), and gives `Store` an
  unexported field, `drainWait`, that `Close` uses when set; T6 and the recovery tests set the field on their own store.
  Same purpose; no package-level state that a test writes while another store may read it. Reversible in minutes if the
  coordinator prefers the letter.

### 2026-11-17, P2: the gate, staged, not committed

Built on a `git archive` copy of `3443524` plus the P1 files; every run below is on such a copy.

- **The write sites: 18, as in the MADR (E14).** All 18 go through the gate: `DB.Update` at `internal/db/badger.go:245`
  (`RecordSynergy`'s flush), `:592` (`UpdateWithRetryCtx`, one admission per attempt), `:1023`, `:1448`, `:2119`,
  `:2244`, `:2251`, `:2328`, `internal/db/historical.go:28`; `NewWriteBatch` at `badger.go:365`, `:637`, `:1632` (one
  admission spans the batch to its `Flush` or `Cancel`); `DropAll` at `badger.go:2476`, `:2522`; the four
  `updateOrWarn` callers (`cache.go:242`, `historical.go:93`, `:119`, `:142`), which now call a store method
  (`Store.updateOrWarn`) instead of the package helper, as P2 step 3 asked. Considered, left ungated (MADR E21, E22):
  `RunValueLogGC` (`gc_helpers.go:39`) and the six index writes. No raw `s.DB.Update`, `NewWriteBatch` or `DropAll`
  remains outside the gate (checked by `grep` over `internal/db/*.go`, tests excluded).
- **The gate.** `writeGate`: a mutex, a closed flag, a counter of admitted writes, an idle channel, and a flag that
  records that `Close`'s wait ran out. `Store.write(fn)` admits, runs `fn`, and ends the write in a deferred function;
  `Store.update(fn)` is `write` around one `DB.Update`.
- **`Close`.** Shut the gate (later writes get `ErrStoreClosing`); wait up to 5 s for admitted writes; if any remains,
  log one warning naming how many and go on; then the old sequence (signal and wait for the store's goroutines, PID
  file, lock, index, Badger). It returns Badger's close error; a wait that ran out is the warning, not an error. Once:
  a second or concurrent `Close` waits for the first and returns its result (T4, `TestStore_ConcurrentCloseIsSafe`).
- **Nesting.** No write path in `internal/db` admits a write inside another today (the investigator's scan, MADR E19,
  re-checked: the only store call inside a write's closure is `flushLoadedMetrics`, which takes the transaction it is
  given, and `PurgeStaleServerTools` calls the index, which is not gated). With a counter, nesting is safe by
  construction: a nested admission is admitted or refused at once, never blocked; `TestStore_NestedAdmissionCannotDeadlockClose`
  fails if admission ever waits for `Close`.
  *Annotation, 2026-11-18 (review note N1):* "no write is admitted inside another" holds for store calls only. In
  production every `slog` call inside an admitted write's closure is a nested admission, because the global logger
  writes each record through `Store.SaveLog` (MADR, E19 annotated). The counter makes it harmless, as stated above.
- **The recovery (third ruling).** In `write`'s deferred function: only when the wait ran out, Badger is closed, and the
  first frame below the runtime's panic frames is a `badger/v4` function, the panic is recovered and becomes an error
  wrapping `ErrStoreClosing` with the panic's text, with one warning. Otherwise `recover` is not called, so the panic
  propagates with its own value and stack; nothing is re-raised. A late write that gets an error, not a panic, keeps the
  library's error (MADR F10).
- **Callers** (scan of every non-test call of the gated entry points): pool tasks (`MinifyResponse`, usage counts) log
  at warn; bare goroutines (`FlushProxyCall`, `FlushHFSCTrace`, the orphan sweep) log at warn, and the proxy-metrics
  flusher logs each failed save at error level (as it did for Badger's errors before); the sync goroutines and the
  request-path callers log at warn or return the error to their caller; `RecordSynergy`'s own goroutine logs at error
  level once per write in flight; the log writer returns it to `slog`, which drops it; only the CLI `db wipe` returns
  it as a failure, and it never runs while closing. No caller treats it as fatal; none loops on it: `UpdateWithRetryCtx`
  retries conflicts only, and a refusal is returned at once (`TestStore_RefusedWriteIsNotRetried`). No caller changed.
- **Tests, green on the change:** T1 to T6, and the 15 tests of the two new files; `-count=20` of the 20 store tests:
  exit 0. One of the new tests (`TestStore_Recovery_ClosedDatabaseWithoutTimeoutPropagates`) was flaky as first
  written: it closed Badger behind the store's back while the store's own background re-index could still be reading,
  and that read panicked (first P4 pass, eleven plants' runs ended in that panic). It now waits for the store's
  goroutines first; the second P4 pass and 20 rounds are clean.
- **Product probe** (the MADR's R5 on the change, scratch only): 900 iterations, 0 panics, 0 writers that never
  returned, 900 of 900 reopens ok, 0 iterations with an acknowledged save missing; 3600 refusals, all `ErrStoreClosing`;
  0 `DB Closed`, 0 `Writes are blocked`; 342438 saves ok; the wait never ran out. With the wait cut to 1 ns, 2400
  iterations: 0 panics reached a writer, 0 hangs, 2400 reopens ok, 0 missing; one commit-window panic ("send on closed
  channel") was converted; 164 late writes got the library's "Writes are blocked" error (F10).
- **Handler-level probe** (scratch only): pool saves, bare-goroutine flushes, usage counts and proxy-metric saves against
  `Close` at a random point, 300 iterations: 0 panics, 0 hangs, the pool drained every time. With `RecordSynergy` also
  running, `Close` panicked on its own WaitGroup (MADR F9), on `3443524` as on the change: put to the owner, not fixed.
- **Benchmark** (Apple M1 Pro, six alternating runs of a small `SaveWithTTL`): `3443524` median 5506 ns/op, the change
  5551 ns/op, both 40 allocs/op and about 1.68 KB/op; the gate alone 28 ns/op serial and 250 ns/op with ten cores
  contending, 0 allocs.
- **Not verified:** the commit-window panic was not forced on demand (no seam inside the library); it was seen once in
  2400 iterations and converted. A write hung inside the library cannot be tested without a hang.

### 2026-11-17, P4: planted breaks, staged, not committed

Each plant through `go test -overlay` on the copy (the files are never edited); each failed a named test with a test
failure, not a build failure. First failing test and line:

| Plant | Fails (first) |
| :--- | :--- |
| the gate never refuses | `TestStore_EveryWriteSiteIsRefusedWhileClosing`, `store_close_gate_test.go:148`; also T2, T3, T1 |
| `Close` does not wait | `TestStore_CloseDrainBoundIsHonoured`, `store_close_bound_test.go:35`; also T5 |
| the bound ignored (an hour) | `TestStore_CloseDrainBoundIsHonoured`, `store_close_bound_test.go:38` |
| the bound too eager (none) | `TestStore_CloseDrainBoundIsHonoured`, `store_close_bound_test.go:35`; also T5, T2 |
| `SaveRawResource` ungated | `TestStore_EveryWriteSiteIsRefusedWhileClosing`, `store_close_gate_test.go:148`; also T3, T2, T1 |
| `FlushProxyCall` ungated (a bare goroutine's write) | `TestStore_EveryWriteSiteIsRefusedWhileClosing`, `store_close_gate_test.go:170` |
| `RecordSynergy`'s flush ungated (a store goroutine) | same test, `store_close_gate_test.go:170` |
| the L2 cache write ungated (a store goroutine) | same test, `store_close_gate_test.go:170` |
| `BatchSaveTools` ungated (a write batch) | same test, `store_close_gate_test.go:148` |
| `WipeAll` ungated (`DropAll`) | same test, `store_close_gate_test.go:148` |
| the sentinel replaced by the library's error | `TestStore_EveryWriteSiteIsRefusedWhileClosing`, `store_close_gate_test.go:148`; also T2, T3, T1 |
| a refused write retried | `TestStore_RefusedWriteIsNotRetried`, `store_close_gate_test.go:201` |
| the second `Close` unguarded | `TestStore_ConcurrentCloseIsSafe`, `store_close_gate_test.go:285`; also T4 |
| a nested admission waits for `Close` | `TestStore_NestedAdmissionCannotDeadlockClose`, `store_close_gate_test.go:240` |
| the recovery converts while the store is open | `TestStore_Recovery_LibraryPanicWhileOpenPropagates`, `store_close_recover_test.go:239` |
| the recovery converts within the bound (no wait-ran-out check) | `TestStore_Recovery_ClosedDatabaseWithoutTimeoutPropagates`, `store_close_recover_test.go:289` |
| the recovery converts before Badger is closed (no closed check) | `TestStore_Recovery_TimedOutButDatabaseOpenPropagates`, `store_close_recover_test.go:315` |
| the recovery does not re-raise (recovers every panic) | `TestStore_Recovery_LibraryPanicWhileOpenPropagates`, `store_close_recover_test.go:239`; also the non-library case |
| the recovery missing from `PurgeServerIntelligence` | `TestStore_PastBound_PurgeServerIntelligenceReturnsErrStoreClosing`, `store_close_recover_test.go:168` |
| the wait-ran-out flag never set | `TestStore_CloseDrainBoundIsHonoured`, `store_close_bound_test.go:41`; also every past-bound test |

Survivors: none.

### 2026-11-17, P5: docs, staged, not committed

- `docs/guides/services-and-transports.md`, "Readiness and shutdown": one paragraph (the gate, the five-second wait, the
  refusal, the recovery past the bound, the hung write, reads not gated).
- Doc comments on `ErrStoreClosing`, `writeGate`, `Store.write`, `Store.Close`.
- `docs/architecture.md` does not describe the store's lifecycle: unchanged. `README.md` says nothing that the change
  makes false.

### 2026-11-17, P6: the gates, staged, not committed

On a `git archive` copy of `3443524` plus the staged files; heavy runs through the host's heavy-run queue, one at a
time; every Go run with `HOME` and the configuration directory moved to a temporary directory. Start and end UTC;
exit status taken before any filter.

| Gate | Start | End | Exit | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `gofmt -l` on the ten changed Go files | 2026-11-18T02:51:51Z | 2026-11-18T02:51:51Z | 0 |  |
| `golint`, one call per changed Go file | 2026-11-18T02:51:51Z | 2026-11-18T02:51:51Z | 0 | nine files silent; `internal/db/badger.go`: the same five findings as on `3443524` (exported-name comments at lines 45, 47, 49, 924, 975, none in changed code) |
| `go vet ./...` | 2026-11-18T02:51:51Z | 2026-11-18T02:51:54Z | 0 |  |
| `go mod tidy -diff` | 2026-11-18T02:51:54Z | 2026-11-18T02:51:54Z | 0 |  |
| `make lint`, golangci-lint v2.13.1 built with Go 1.26.6 | 2026-11-18T02:51:54Z | 2026-11-18T02:52:14Z | 0 | 0 issues |
| `make lint`, golangci-lint v2.15.0 built with Go 1.26.6 | 2026-11-18T02:52:14Z | 2026-11-18T02:52:23Z | 2 | 2 issues, both present on `3443524` and in other plans' files: `list_tools_handler.go:19:1` gocognit, `pipeline_tools.go:161:33` gocritic |
| `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...` | 2026-11-18T02:52:23Z | 2026-11-18T02:52:27Z | 0 |  |
| `GOOS=windows GOARCH=amd64 go build ./...` | 2026-11-18T02:52:27Z | 2026-11-18T02:52:32Z | 0 |  |
| `GOOS=darwin GOARCH=arm64 go build ./...` | 2026-11-18T02:52:32Z | 2026-11-18T02:52:34Z | 0 |  |
| `python3 scripts/check_records.py --check-all` | 2026-11-18T02:54:13Z | 2026-11-18T02:54:13Z | 0 |  |
| `markdownlint-cli2@0.23.2` (the `make markdownlint` command) | 2026-11-18T02:54:13Z | 2026-11-18T02:54:14Z | 0 |  |
| `go test -count=1 -timeout 20m ./...` | 2026-11-18T02:52:49Z | 2026-11-18T02:59:52Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 1 | 2026-11-18T02:59:52Z | 2026-11-18T03:05:15Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 2 | 2026-11-18T03:05:15Z | 2026-11-18T03:10:17Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 3 | 2026-11-18T03:10:17Z | 2026-11-18T03:17:45Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 4 | 2026-11-18T03:17:45Z | 2026-11-18T03:25:42Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 5 | 2026-11-18T03:25:42Z | 2026-11-18T03:30:32Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/db/...` | 2026-11-18T03:30:33Z | 2026-11-18T03:34:55Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=3 -timeout 40m ./...` | 2026-11-18T03:34:55Z | 2026-11-18T03:36:14Z | 1 | DATA RACE 0, panic lines 0; failing: 2 TestProxyMetrics; 2 TestProxyRelayTracker |
| `go test -race -count=1 -timeout 10m ./internal/telemetry/` (F8 check) | 2026-11-18T03:36:26Z | 2026-11-18T03:36:30Z | 0 | DATA RACE 0, panic lines 0 |

- The start times of the heavy runs include the wait in the queue.
- The whole-module run under `-race`: 18 packages ok, 1 FAIL (`internal/telemetry`): `TestProxyMetrics` and
  `TestProxyRelayTracker` failed on two of three rounds (`proxy_metrics_extra_test.go:28`, `boot_spans_extra_test.go:32`),
  the MADR's F8, older than this change; the same package at `-count=1` under `-race` exits 0. No `DATA RACE` anywhere.
- The five whole-package handler gates: 5 of 5 exit 0, 0 `DATA RACE`.
- `check_records` and `markdownlint` were run again on the final records, with this entry in place: exit 0 both.
- Go for the tests: the host's default toolchain (go1.27.4), as in the MADR's baseline; the linters were built with
  Go 1.26.6 and run with `GOTOOLCHAIN=go1.26.6`.
- Staged by path, not committed: the ten Go files, the guide, and these two records.

### 2026-11-17, F9: the store's goroutine starts through the gate (fourth ruling), staged, not committed

- **Ruling.** "In 0009, by amendment: those four starts go through the gate (Recommended)" (MADR, "Decided, fourth
  ruling"). Built on a `git archive` copy of `3443524` plus the staged files.
- **Sites, re-derived** (`grep` for `bgWg.Add` and `bgWg.Go` in `internal/db`, tests excluded; lines at `3443524`):
  `badger.go:234` (`RecordSynergy`), `badger.go:917` (`SaveIntelligence`'s re-index), `cache.go:234` (the L2 cache
  write), `metrics_flush_helpers.go:130` (the post-flush refresh); and `gc_helpers.go:91`, reached only from `NewStore`
  before the store is returned, left as it is. The only `bgWg.Wait` is `Close`'s.
- **Red first, on the change as first staged.** `TestStore_CloseWhileStoreGoroutinesStart` (all four sites starting
  while `Close` runs at a random point, 100 iterations a run): 10 runs of 10 failed, `store_close_starts_test.go:75:
  iteration N: Close panicked: sync: WaitGroup is reused before previous Wait has returned`, at iterations 0 to 43
  (10 panics in 135 iterations, about one in 14). `TestStore_BackgroundStartsAreRefusedOnceClosing`: all four subtests
  failed (no refusal; the synergy flush and the L2 write still ran).
- **The fix.** `Store.goBackground(work, fn)`: under the gate's mutex, refuse the start if the gate is shut, else
  `bgWg.Add(1)`; then start the goroutine. The four sites use it. The first refused start of a store logs one info line
  naming the work. Two test seams, each one atomic load when unset: `startHook` (inside the admission, just before
  `bgWg.Add`) and `runHook` (first on each started goroutine).
- **Green.** The stress test: 10 runs of 10 passed (1000 iterations, 0 panics, 0 hangs); every store test (24 top-level
  tests) passes 3 rounds; T1 passes.
- **Two new tests the plants asked for.** With the starts gated, the plants that un-gate the write inside the synergy
  flush and the L2 write survived the earlier tests (the goroutine is no longer started while closing).
  `TestStore_StartedGoroutineWriteIsRefusedWhileClosing` parks a goroutine started before `Close` and lets it write
  while `Close` waits: both plants fail it. `TestStore_StartAdmittedBeforeCloseIsCountedFirst` parks a start inside its
  admission: `Close` must not return before the start is counted.
- **Probes, on the final code** (scratch only). The handler-level probe with all four start sites and `RecordSynergy`
  running against `Close` (the probe that panicked in 2 of 3 runs on `3443524` and 3 of 3 on the change as first
  staged): 300 iterations, 0 panics, 0 hangs, the pool drained every time. The store-level probe of the MADR's R5:
  900 iterations, 0 panics, 0 writers that never returned, 900 of 900 reopens ok, 0 iterations with an acknowledged
  save missing, 3600 refusals (all `ErrStoreClosing`), 252670 saves ok.
- **Docs.** The guide's "Readiness and shutdown" paragraph gains one sentence (the store's own background work is not
  started once closing has begun; one info line says so; the close waits for work already started). Doc comment on
  `goBackground`; comments at the four sites say what a refused start drops.

Plants, all on the final code (26: the 20 of P4 and six for this amendment); each failed a named test with a
test failure. The six:

| Plant | Fails (first) |
| :--- | :--- |
| `RecordSynergy`'s start un-gated | `TestStore_CloseWhileStoreGoroutinesStart`, `store_close_starts_test.go:75` ("Close panicked: sync: WaitGroup is reused …") |
| the re-index start un-gated | `TestStore_BackgroundStartsAreRefusedOnceClosing/SaveIntelligence`, `:252` |
| the L2 cache write start un-gated | `TestStore_CloseWhileStoreGoroutinesStart`, `:75` |
| the post-flush refresh start un-gated | `TestStore_BackgroundStartsAreRefusedOnceClosing/FlushMetrics`, `:273` |
| the admission released before `bgWg.Add` | `TestStore_StartAdmittedBeforeCloseIsCountedFirst`, `:106` |
| a refused start that still runs | `TestStore_BackgroundStartsAreRefusedOnceClosing/RecordSynergy`, `:219` |

Of the 20 earlier plants, the two that un-gate the writes inside the synergy flush and the L2 write now fail
`TestStore_StartedGoroutineWriteIsRefusedWhileClosing` (`:168`); the other 18 fail as recorded under P4. Survivors:
none.

*Annotation, 2026-11-18 (review, S1):* the row "the admission released before `bgWg.Add`" above is the plant that
moved the test hook outside the mutex together with the `Add`; that one fails at `:106`. The reviewer's plant moved
only the `Add` and left the hook inside the mutex: it survived every test without `-race` and was caught only under
`-race` ("race detected during execution of test" in `TestStore_StartAdmittedBeforeCloseIsCountedFirst`). Answered in
the entry of 2026-11-18 below.

### 2026-11-17, P6 again, after the fourth ruling: the gates, staged, not committed

On a `git archive` copy of `3443524` plus the staged files; heavy runs through the host's heavy-run queue, one at a
time; every Go run with `HOME` and the configuration directory moved to a temporary directory. Start and end UTC;
exit status taken before any filter.

| Gate | Start | End | Exit | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `gofmt -l` on the twelve changed Go files | 2026-11-18T04:48:28Z | 2026-11-18T04:48:28Z | 0 |  |
| `golint`, one call per changed Go file | 2026-11-18T04:48:28Z | 2026-11-18T04:48:28Z | 0 | eleven files silent; `internal/db/badger.go`: the same five findings as on `3443524` (exported-name comments, none in changed code) |
| `go vet ./...` | 2026-11-18T04:48:28Z | 2026-11-18T04:48:30Z | 0 |  |
| `go mod tidy -diff` | 2026-11-18T04:48:30Z | 2026-11-18T04:48:30Z | 0 |  |
| `make lint`, golangci-lint v2.13.1 built with Go 1.26.6 | 2026-11-18T04:48:30Z | 2026-11-18T04:48:41Z | 0 | 0 issues |
| `make lint`, golangci-lint v2.15.0 built with Go 1.26.6 | 2026-11-18T04:48:41Z | 2026-11-18T04:48:56Z | 2 | 2 issues, both on `3443524` and in other plans' files: `list_tools_handler.go:19:1` gocognit, `pipeline_tools.go:161:33` gocritic |
| `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build ./...` | 2026-11-18T04:48:56Z | 2026-11-18T04:49:08Z | 0 |  |
| `GOOS=windows GOARCH=amd64 go build ./...` | 2026-11-18T04:49:08Z | 2026-11-18T04:49:19Z | 0 |  |
| `GOOS=darwin GOARCH=arm64 go build ./...` | 2026-11-18T04:49:19Z | 2026-11-18T04:49:23Z | 0 |  |
| `go test -count=1 -timeout 20m ./...` | 2026-11-18T04:49:40Z | 2026-11-18T04:54:34Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 1 | 2026-11-18T04:54:34Z | 2026-11-18T04:59:45Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 2 | 2026-11-18T04:59:45Z | 2026-11-18T05:05:13Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 3 | 2026-11-18T05:05:13Z | 2026-11-18T05:21:33Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 4 | 2026-11-18T05:21:33Z | 2026-11-18T05:30:34Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/handler/...`, run 5 | 2026-11-18T05:30:34Z | 2026-11-18T05:36:33Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=20 -timeout 30m ./internal/db/...` | 2026-11-18T05:36:33Z | 2026-11-18T05:43:46Z | 0 | DATA RACE 0, panic lines 0 |
| `go test -race -count=3 -timeout 40m ./...` | 2026-11-18T05:43:47Z | 2026-11-18T05:45:07Z | 1 | DATA RACE 0, panic lines 0; failing: 2 TestProxyMetrics; 2 TestProxyRelayTracker |
| `go test -race -count=1 -timeout 10m ./internal/telemetry/` (F8 check) | 2026-11-18T05:45:07Z | 2026-11-18T05:45:09Z | 0 | DATA RACE 0, panic lines 0 |

- Run after the last Go change of the amendment. The start times of the heavy runs include the wait in the queue.
- The five whole-package handler gates: 5 of 5 exit 0, 0 `DATA RACE`.
- The whole-module run under `-race`: 18 packages ok, 1 FAIL (`internal/telemetry`): `TestProxyMetrics` and
  `TestProxyRelayTracker` on two of three rounds (`proxy_metrics_extra_test.go:28`, `boot_spans_extra_test.go:32`),
  the MADR's F8; the package at `-count=1` under `-race` exits 0. No `DATA RACE` anywhere.
- `check_records` and `markdownlint` were run again on the final records, with this entry in place: exit 0 both.
- Staged by path, not committed: the twelve Go files, the guide, and these two records.

### 2026-11-18, deviation: the whole-module gate is owed

- **What.** The PLAN's whole-module gate (P6 item 4; the MADR's Confirmation item 5), `go test -race -count=3 -timeout
  40m ./...`, is not run for this pass, and no `go test ./...`, `go test ./internal/client/...`, `make test`, `make race`
  or built binary of this module runs on this machine.
- **Why.** Three tests in `internal/client` (`TestExtraManagerMethods`, `TestPruneOrphansEnumeratesExactlyOnce`,
  `TestHealthMonitor_Lifecycle`) run the orphan reaper for real: `PruneOrphans` (`internal/client/process_reaper.go:89-149`)
  signals every process of the user whose environment carries the marker the owner's live service puts on its
  sub-servers. A throwaway `HOME` does not prevent it.
- **The owner's ruling (2026-11-18; the label quoted only as a label):** "Fix the hazard first, under its own short
  record; until then run only the safe packages here (Recommended)". Allowed here: `./internal/db/...`,
  `./internal/handler/...`, `./cmd/...`, with a throwaway `HOME`.
- **Consequence.** The whole-module gate is owed. It is run after that record's fix lands. The earlier whole-module
  runs of this record (P6, 2026-11-18 02:52 to 03:36 UTC and again 04:49 to 05:45 UTC: 18 packages ok, only F8's
  `internal/telemetry` failing, 0 `DATA RACE`) predate the edits of 2026-11-18 below and do not meet the gate for them.
  *Annotation, 2026-11-18:* met. After this entry was written, the owner confirmed (the label quoted only as a label:
  "No live magictools server there: use it (Recommended)") that the fleet's Linux machine runs no live magictools
  service, and the coordinator ran the gate there before committing. See the entry "2026-11-18, the commit, the owed
  gate met, and the close-out" below.

### 2026-11-18, the review's findings and notes, staged, not committed

- **Review.** No blocker; S1 and S2 should-fix; survivors as recorded in the MADR ("Independent review and the
  decisions after it").
- **S2.** Two tests in `store_close_recover_test.go`: a panic that is not the library's, at a site with no error result
  (`updateOrWarn`), propagates with its own value past the bound
  (`TestStore_Recovery_NonLibraryPanicAtSiteWithoutErrorResultPastTheBoundPropagates`) and within it
  (`TestStore_Recovery_NonLibraryPanicAtSiteWithoutErrorResultWithinTheBoundPropagates`). Under the reviewer's plant
  (a `recover()` that logs, in `Store.updateOrWarn`) both fail; see the plants below.
- **S1.** The count and the test hook now live together in `countStart` (`bgWg.Add`, then the hook), which
  `goBackground` calls while holding the gate's mutex. `TestStore_StartAdmittedBeforeCloseIsCountedFirst` checks, from
  the hook, that the mutex is held (`TryLock` fails), deterministically and without `-race`; its comment says what it
  proves. Limit: a plant that separates the `Add` from `countStart` (the old shape: the hook inside the mutex, the `Add`
  after it) is again visible only under `-race`; it is listed below with what catches it.
- **F11.** `Close`'s doc comment now says that if the teardown panics the once is spent and a later call returns nil
  while Badger stays open.
- **Docs (N4).** The guide's list of what can be lost at shutdown now also names a refused intelligence record or synced
  batch of tool records, the last learned-weight updates and a prompt-cache entry, and says the base lost the same writes
  to the library's closed-database error.
- **A run cut short.** The first plant run of this pass was stopped by a pause (the owner's usage quota) after two or
  three plants, with no summary; nothing from it counts. It was run again in full.
- **Plants, in full on this pass's code** (28: the 20 of P4, the six of the fourth ruling with the counting plant in
  its new form, and two from the review). 27 failed a named test with a test failure:

| Plant | Fails (first) |
| :--- | :--- |
| a `recover()` that logs in `Store.updateOrWarn` (the review's "noerror-site-swallows-panic") | `TestStore_Recovery_NonLibraryPanicAtSiteWithoutErrorResultPastTheBoundPropagates` and `…WithinTheBoundPropagates`, `store_close_recover_test.go:235` ("the panic did not propagate: the write returned <nil>") |
| the gate's mutex released before `countStart` | `TestStore_StartAdmittedBeforeCloseIsCountedFirst`, `store_close_starts_test.go:121` ("a background start was counted in bgWg outside the gate's mutex"), without `-race` |
| `bgWg.Add` split from `countStart` and moved after the mutex, the hook left inside (the review's "add-outside-mutex" in its original shape) | survives without `-race`; under `-race`, `-count=20`, `TestStore_StartAdmittedBeforeCloseIsCountedFirst` fails with "race detected during execution of test" (1 `DATA RACE`) |

  The other 25 fail as recorded above (P4 and the fourth ruling); the counting plant's line moved to `:121`. Why the
  split plant cannot be caught without `-race`: with the hook left inside the mutex and the `Add` after it, the only
  difference is the order of an `Add` and an `Unlock` a few instructions apart, with no seam between them; a
  deterministic check needs the check to travel with the `Add`, which is what `countStart` does.

| Gate (safe packages only) | Start (UTC) | End (UTC) | Exit | Notes |
| :--- | :--- | :--- | :--- | :--- |
| `gofmt -l` on the twelve changed Go files | 2026-11-18T18:25:56Z | 2026-11-18T18:25:56Z | 0 | |
| `go vet ./internal/db/ ./internal/handler/` | 2026-11-18T18:25:56Z | 2026-11-18T18:25:57Z | 0 | |
| `make lint`, golangci-lint v2.13.1 built with Go 1.26.6 | 2026-11-18T18:25:57Z | 2026-11-18T18:26:37Z | 0 | 0 issues |
| `make lint`, golangci-lint v2.15.0 built with Go 1.26.6 | 2026-11-18T18:26:37Z | 2026-11-18T18:26:48Z | 2 | 2 issues, both on `3443524` and in other plans' files: `list_tools_handler.go:19:1` gocognit, `pipeline_tools.go:161:33` gocritic |
| `go test -race -count=20 -timeout 30m ./internal/db/...` | 2026-11-18T18:26:48Z | 2026-11-18T19:00:31Z | 0 | 0 `DATA RACE`; start includes the queue wait |
| `go test -race -count=20 -timeout 30m ./internal/handler/...` | 2026-11-18T19:00:31Z | 2026-11-18T19:09:34Z | 0 | 0 `DATA RACE` |

- `check_records` and `markdownlint` were run on the final records, with this entry in place: exit 0 both.
- Not run, by the owner's ruling: the whole module and `./internal/client/...`. Owed: the whole-module gate (the
  deviation above). Cross-builds were not among this pass's gates.
- Staged by path, not committed: the same 15 paths.

### 2026-11-18, the commit, the owed gate met, and the close-out

- **The commit.** The coordinator committed and pushed the change as `94e1de4` ("fix(db): gate store writes and
  background starts during Close") on `main`, after its own gate on a copy of the index. It holds the 15 staged paths
  and nothing else: the two 0009 records, `docs/guides/services-and-transports.md`, `internal/db/badger.go`,
  `cache.go`, `historical.go`, `io_helpers.go`, `metrics_flush_helpers.go`, the six `internal/db` test files of this
  plan and `internal/handler/minify_close_refused_test.go` (15 files, 2260 insertions, 87 deletions).
- **The owed whole-module gate, met (run by the coordinator, 2026-11-18, on the fleet's Linux machine).** From a copy of
  the index (`3443524` plus the 15 staged paths, nothing else), through the fleet's `run-on-host --need linux --race`.
  First a read-only listing: no process of the user carrying `MCP_ORCHESTRATOR_OWNED=true` and no process named
  `mcp-server-magictools` ("no marked process and no magictools process on this host"). Then
  `go test -race -count=1 -timeout 40m ./...` with a throwaway home: exit 0, 19 packages ok, no `DATA RACE`, no failure
  (13:50 to 13:52 local time). The run used `-count=1`, not P6's `-count=3`: at more than one round the known F8
  failures of `internal/telemetry` appear, and the earlier `-count=3` runs of this change showed only those. On the
  development machine, on the same copy: `go test -race -count=20 -timeout 30m ./internal/db/...` exit 0 and
  `./internal/handler/...` exit 0, both with no `DATA RACE`; `go vet` of both packages exit 0.
- **The race-gate count.** On the change the whole-package handler race gate is green in every run since the
  amendment: 16 as last recorded (P6 twice, 10; the reviewer 3; the coordinator 3), plus the review pass of 2026-11-18
  once and the coordinator once more: 18 runs, 18 green, 0 red.

**The completion criteria, one by one.**

| Criterion | Met | Evidence |
| :--- | :--- | :--- |
| 1. Every phase done, or skipped by the ruling on Q1 (P3) | yes | P0, P1: entries of 2026-11-17; P2: "P2: the gate", the third and fourth rulings' entries; P3: skipped (annotation under P3); P4: "P4: planted breaks" and the later plant runs; P5: "P5: docs" and the docs bullets of later entries; P6: "P6", "P6 again", the review pass's gates, and the whole-module gate above; P7: the independent review (MADR, "Independent review and the decisions after it"); P8: staged and handed over, committed as `94e1de4` |
| 2. T1 to T6 in the tree; T1 to T5 red on `5e8821e`; T6 red under its plant; every plant seen failing its tests | yes | T1 to T5: "P1"; T6: P4 table ("the bound ignored"); the plants: the P4 table, the fourth ruling's table, and the review pass's 28 plants, all seen failing; the one that survives without `-race` (`bgWg.Add` split from `countStart`) was seen failing under `-race` |
| 3. P6.3 passed 5 of 5; the module-wide run clean or each finding named | yes | the handler race gate: 18 of 18 (above); the module-wide run: clean on the Linux machine (above); the earlier `-count=3` runs: F8 only, named |
| 4. The independent review's findings closed | yes | S1, S2: the entry "the review's findings and notes"; F10: the coordinator's decision; F11: the owner's ruling, accepted here, to the raw-handle follow-up; the third survivor informational (MADR, the review section); the change with these answers committed after the coordinator's gate |
| 5. The docs step done; the record check and markdownlint pass | yes | P5 and the docs bullets of later entries; `check_records` and markdownlint exit 0 after this entry |
| 6. Staged for the owner; the MADR `accepted`; this plan `implemented` only after the commit of the finished change | yes | staged by path in each pass; the MADR's status is `accepted`; committed as `94e1de4` under the commit rule as restored on 2026-11-17 (Conventions annotation) |

All six are met, so this plan's status becomes `implemented`, the word criterion 6 names and the record check's PLAN
vocabulary holds (a scratch run with `complete` fails that check: "'complete' is not a PLAN status").

Not criteria of this plan, recorded for whoever reads on: CI on `94e1de4` (the coordinator watches it); the 0008
redesign's gate re-run, which belongs to
[0008-PLAN-redesign-the-list-tools-middleware-under-the-complexity-limit.md](0008-PLAN-redesign-the-list-tools-middleware-under-the-complexity-limit.md).
The follow-ups named in the MADR: the raw-handle writers (F3, with F11's wipe-versus-close panic) and the telemetry
second-round failures (F8).
