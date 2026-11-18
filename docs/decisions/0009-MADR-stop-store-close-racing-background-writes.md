---
status: accepted
date: 2026-11-17
decision-makers: repository owner
consulted: "the owner's two rulings of 2026-11-17, relayed by the cross-repository coordinator; this repository at 5e8821e;
github.com/dgraph-io/badger/v4 v4.9.1 and github.com/modelcontextprotocol/go-sdk v1.6.1 module source (read only); the
other plan's agent's two gate logs (read only); the experiments under More Information, run in scratch copies of
5e8821e, every store in a temporary directory the experiment made"
informed: "the cross-repository coordinator"
---

# Make the store refuse writes once it is closing, and wait for the writes it already admitted

## Context and Problem Statement

A required gate of another plan in this repository, `go test -race -count=20 -timeout 30m ./internal/handler/...`,
exited 1 on one `DATA RACE` that is outside that plan's change: a `MinifyResponse` background task writing through
`Store.SaveRawResource` races with `Store.Close` in a test's deferred cleanup (`internal/handler/minify_golden_test.go:105`).
Every later plan in this repository runs the same gate, so the gate has to be able to pass honestly before any of them
can be finished.

### Decided (the owner, 2026-11-17, relayed by the coordinator; the label is quoted only as a label)

The question put to the owner was how to resolve a required gate that fails on a pre-existing race outside the change,
given that every later plan in this repository runs the same gate. The owner chose the option labelled
"Fix that race first under its own short record, then re-run the gate (Recommended)". As put to the owner: an agent
establishes whether it is only a test defect or also a shutdown defect in the running server (a save after the store is
closed), drafts a small record for the owner's approval, fixes it, and the other plan's gate is then run again on a tree
where it can pass honestly. This record is that record. It was `proposed` until the owner's second ruling, below.

### Decided, second ruling (the owner, 2026-11-17, relayed by the coordinator; each label quoted only as a label)

An addition of 2026-11-17: the record was `proposed` and this is the owner's answer to its four open questions and to how
the work proceeds. Nothing above this heading changes. Each answer is the option this record recommended.

1. **Q1, which fix:** "A gate inside the store: close waits for writes in flight, later writes are refused (Recommended)".
   As put: it covers every writer that goes through the store, in the product and in tests. The pool drain (option B) is
   not part of this record.
2. **Q2, what a late write gets:** "A named error, \"store closing\"; callers log it and carry on (Recommended)". As put:
   one new exported error in the database package, `db.ErrStoreClosing`, with the text `store closing`; callers can tell a
   shutdown refusal from a real failure, and nothing panics. (The prototype's text was `store is closing`; the measured
   tallies below quote the prototype's text as measured. The change uses the ruled text.)
3. **Q3, the close wait:** "Five seconds, then a warning and close (Recommended)". As put: a stuck writer cannot hang
   shutdown; a write still running after five seconds may then hit the closed database, as any write can today.
4. **Q4, the gate's scope:** "All 18 write sites in the database package; reads left alone (Recommended)".
5. **How it proceeds:** "Approve the whole plan; unstage 0008's files meanwhile (Recommended)". As put: this record pair is
   marked accepted and handed to the owner to commit first; then the tests, the gate and its review; then the 0008 gate is
   re-run. Nothing is installed or restarted on the owner's running server.

**The commit rule.** The owner makes every commit and push, from the coordinator's staged list. Agents and the
coordinator stage by path and do not commit.

*Annotation, 2026-11-17 (evening):* the owner restored the coordinator's commits and pushes to the default branch.
Agents still stage by path and do not commit; the rule above stood until then.

### Decided, third ruling: a write that outlived the close wait (the owner, 2026-11-17, relayed by the coordinator; the label quoted only as a label)

An addition of 2026-11-17, made during P2. Nothing above this heading changes.

**The stop.** Building the gate, the P2 agent asked whether a write admitted before closing and still running when
`Close`'s five-second wait runs out can panic once `Close` has closed Badger. It can. P2 stopped and put three options
to the owner before building anything.

**The evidence** (badger/v4 v4.9.1 module source, read; scratch runs on `3443524`, the commit of this record pair,
with the investigator's counter-gate prototype and its wait shortened to 200 ms):

* **The iterator path, deterministic.** `txn.NewIterator` panics with `badger.ErrDBClosed` on a closed database
  (`badger/v4@v4.9.1/iterator.go:463-464`). A write parked inside its transaction function until `Close` had returned,
  then let go on: opening an iterator panicked `DB Closed` in 3 of 3 runs; `txn.Get` returned the error `DB Closed`;
  `txn.Set` and commit returned `Writes are blocked, possibly due to DropAll or Close` (`demo_pastbound.log`, scratch).
  Three gated write paths open an iterator inside their transaction: `PurgeStaleServerTools`, `PruneOrphanedIntelligence`
  and `PurgeServerIntelligence` (`internal/db/badger.go:1703`, `:1880`, `:1924`). The third is reached from the orphan
  sweep, a bare goroutine with no `recover` (`internal/client/sync_manager.go:70-78`).
* **The commit window, narrow.** A commit that passes the `blockWrites` check (`badger/v4@v4.9.1/db.go:884`) before
  `DB.Close` sets it (`:538`) and sends (`:904`) after `close(db.writeCh)` (`:550`) panics with "send on closed channel".
* Nothing bounds either except recovery: the pool recovers its tasks (`internal/handler/background_pool.go:37-44`); the
  bare goroutines of E13 do not (F4).

**The ruling.** "Catch a panic only in a write that outlived the wait (Recommended)". As put to the owner: every gated
write ends through one small deferred check; if the wait had already timed out, a panic from the closed database
becomes the "store closing" error with a logged warning; any other panic is re-raised unchanged; it covers every
caller, bare goroutines included; a write hung inside the library stays blocked until the process exits; it costs a
deferred call per write and an amendment to the record.

**How it sits with the earlier rulings.**

* Q3 ("Five seconds, then a warning and close"): unchanged. `Close` still waits at most five seconds, warns and closes;
  the recovery acts only on a write that outlived that wait.
* The PLAN's Rollback note forbids catching the panic in the writers *instead of* the gate. This recovery is *in
  addition to* the gate: the gate keeps every write admitted before the wait ran out off the closed database, and the
  recovery acts only past the bound. It never replaces the gate.
* The decision driver "No crash and no hang ... it never panics" is made precise: no panic that the closed database
  raises in a gated write reaches that write's caller. A write that blocks inside the library after the close is not
  covered: its goroutine stays blocked until the process exits, and `Close` has already returned.

**What "a panic from the closed database" means in the change.** The deferred check converts a panic only when all
of these hold: `Close`'s wait ran out with writes in flight (the gate records it); Badger reports itself closed
(`DB.IsClosed`); and the panic was raised inside Badger, that is, the first frame below the runtime's panic frames
belongs to `github.com/dgraph-io/badger/v4`. That last test is by the panic's origin, not its value alone: the value
`badger.ErrDBClosed` identifies the iterator path, but "send on closed channel" is a runtime error any code can raise,
and only its origin (`badger/v4.(*DB).sendToWriteCh`) tells it apart. Every admitted write was admitted before closing,
by construction. A panic that fails any test is not recovered at all, so it propagates with its own value and stack;
nothing is re-raised.

**A late write that does not panic.** A write that outlived the wait and then calls into the closed database without
a panic gets the library's own error ("DB Closed" or "Writes are blocked ..."), not `ErrStoreClosing`. The ruling covers
panics only; this is recorded, not changed.

### Decided, fourth ruling: the store's own goroutine starts go through the gate (the owner, 2026-11-17, relayed by the coordinator; the label quoted only as a label)

An addition of 2026-11-17, made after P2. Nothing above this heading changes.

**The finding (F9).** Four places start one of the store's own goroutines with `bgWg.Add` or `bgWg.Go` and nothing
orders them against `Close`'s `bgWg.Wait()`: `RecordSynergy`'s flush (`internal/db/badger.go:234` at `3443524`), the
re-index after `SaveIntelligence` (`:917`), the L2 response-cache write (`internal/db/cache.go:234`) and the refresh
after a metrics flush (`internal/db/metrics_flush_helpers.go:130`). A start counted while `Wait` is returning makes
`Close` panic with `sync: WaitGroup is reused before previous Wait has returned`; a start counted after `Wait` has
returned runs against a closed store.

**The evidence.** A handler-level probe with `RecordSynergy` running against `Close` panicked in 2 of 3 runs of 100
iterations on `3443524`, and in 3 of 3 on the change as first staged (R14). A store-level test that keeps all four
sites starting while `Close` runs at a random point, `TestStore_CloseWhileStoreGoroutinesStart` (100 iterations per
run), on the change as first staged: 10 runs of 10 failed, `Close panicked: sync: WaitGroup is reused before previous
Wait has returned`, at iterations 0 to 43 (10 panics in 135 iterations, about one in 14).

**The ruling.** "In 0009, by amendment: those four starts go through the gate (Recommended)". As put to the owner: each
of the four sites asks the gate before starting its goroutine, so none can start once `Close` has begun; four sites,
one test seen failing first, the gates re-run, then the independent review covers the whole change once; the record's
claim (shutdown no longer panics) becomes true.

**The change.** Each of the four sites starts its goroutine through `Store.goBackground`. It takes the gate's mutex,
refuses the start if the gate is shut, and otherwise calls `bgWg.Add(1)` before it releases the mutex.

**The ordering argument.** `Close` shuts the gate (sets the closed flag under the gate's mutex) as its first step, and
reaches `bgWg.Wait()` only later, in the same goroutine. A start checks the flag and calls `bgWg.Add(1)` while holding
the same mutex. So every `Add` that happens at all happens inside a critical section that precedes the one in which
`Close` sets the flag, and therefore precedes `Wait`; every start whose critical section comes after sees the flag and
does not `Add`. A start admitted in the instant before the gate shuts holds the mutex until its `Add` is done, so the
gate cannot shut, and `Wait` cannot run, until that start is counted. No `Add` can follow the `Wait`.

**The drain.** These goroutines are not admitted writes: they are not counted in the five-second drain, and `Close`
waits for them through `bgWg.Wait`, without a bound, as before. Their own writes go through the gate like any other
write, so after the gate shuts those writes are refused at once and the goroutines finish quickly.

**What a refused start drops.** Nothing that a caller was told had been stored:

* `RecordSynergy`'s flush: the in-memory weights are already updated; the disk copy misses the last increments, and the
  next start seeds the weights from disk without them. `RecordSynergy` returns nothing.
* The re-index after `SaveIntelligence`: the intelligence is saved (that write was admitted and returned nil); the search
  index keeps the tool's old fields. The next start does not repair it: its drift check compares counts, not contents.
  The entry is refreshed the next time that tool is re-indexed (its next intelligence or tool save, a sync, or a full
  re-index).
* The L2 response-cache write: the persistent copy of a cached response is not written; the in-memory copy is unaffected
  and the persistent cache misses that entry after a restart.
* The refresh after a metrics flush: the metrics are already flushed; the index keeps the tool's old figures until the
  tool is next re-indexed, and the explicit `Sync` is skipped (Badger's own close flushes).

The first refused start of a store logs one line at info level naming the work; later ones are silent.

**Other users of the same machinery.** `startBackgroundReindexIfNeeded` (`internal/db/gc_helpers.go:91`) also uses
`bgWg.Go`, but only from `NewStore`, before the store is returned to any caller, so it precedes every `Close`. The
`closing` channel is closed once (`Close` is idempotent); `cancel` is idempotent; the gate is under its mutex. Not
covered here and recorded as F11: `WipeAll` and `WipeDatabase` replace `s.Index` after their `DropAll`, outside the
gate, so a wipe running while `Close` closes the index can race on that pointer. A wipe runs only from the CLI's
`db wipe` (`WipeDatabase` has no caller outside tests), not at shutdown.

### Independent review and the decisions after it (2026-11-18, by addition)

An addition of 2026-11-18. Nothing above this heading changes.

* **The review.** An independent reviewer reviewed the staged change (15 paths at `3443524`): no blocker; 41 plants; the
  WaitGroup panic of F9 reproduced on the base and gone on the change; the real `Stop` driven in process, 1 panic in 16
  on the base and 0 in 16 after; the benchmark reproduced. Two should-fix findings. **S1:** the plant that moves
  `bgWg.Add` outside the gate's mutex survived every test without `-race` and was caught only under `-race`. **S2:** no
  test caught `Store.updateOrWarn` swallowing a panic that is not the library's. Survivors, verbatim:
  "add-outside-mutex: survives without `-race`", "noerror-site-swallows-panic: survives",
  "close-signals-goroutines-before-drain: survives" (informational: no record claims that ordering). S1 and S2 are
  answered in the PLAN's entry of 2026-11-18.
* **F10, the coordinator's decision (the owner told):** accepted as recorded (see F10).
* **F11, the owner's ruling (2026-11-18; the label quoted only as a label):** "Accept it here and record the panic; fix
  it with the raw-handle follow-up (Recommended)" (see F11).
* **A restriction on test runs (the owner's ruling, 2026-11-18; the label quoted only as a label):** "Fix the hazard
  first, under its own short record; until then run only the safe packages here (Recommended)". Three tests in
  `internal/client` run the orphan reaper for real and can kill the owner's live sub-servers. Until that record's fix
  lands, only `./internal/db/...`, `./internal/handler/...` and `./cmd/...` run on this machine. The whole-module gate
  of Confirmation item 5 is owed (PLAN, deviation of 2026-11-18).

### The answer in one paragraph

It is both a test defect and a product defect. The test closes a store while a background save the test itself started
may still be queued, which a test should not do. But the running server does the same thing: `Stop` closes the store
without draining the background pool, the pool's tasks write to the store, and seven other goroutine starts write to
it with nothing waiting for them (E3, E4, E13). What the detector reports is inside Badger: `close(db.writeCh)` in
`DB.Close` against the send on the same channel in a concurrent `Update` (R3, E11). The same window, with the detector
off, produces a panic ("send on closed channel") and a writer that never returns: on `5e8821e`, 12 panics and 10 hung
writers in 1200 iterations of a hostile loop (R5). No acknowledged write was lost and the store reopened every time (R5),
so what a user can lose is small: a cache entry, a counter, or, in a bare goroutine without `recover`, the process at the
moment it is already shutting down (Findings F4). A fix in the tests alone (option D) would turn the gate green and leave
that window open in the product. The recommended fix is in the store: `Close` refuses new writes with a documented error
and waits, bounded, for the writes it already admitted (option A).

What this record did and did not reproduce. It did not reproduce the report at the level the gate sees it, a
`MinifyResponse` task on the pool against a closing store: 5 of 5 whole-package runs of the gate on `5e8821e` were green and 0 were red. Three forced-overlap tests, 30 rounds
each, were green too (R1, R2). It did reproduce the same report at the store level, with a probe that calls
`SaveRawResource` from four goroutines while `Close` runs: one report in 600 iterations under `-race`, with the same
Badger frames on both sides (R4). The report is intermittent and the window is real: without the detector, the same
window produced panics and hung writers (R5). The deterministic tests of this record pin its cause, not its symptom
(Confirmation).

### The reported observations, each checked

The other plan's agent reported the following; they were claims until checked.

| Reported | Checked |
| :--- | :--- |
| The gate exited 1 on one `DATA RACE` outside that plan's change | Confirmed from its two logs: each carries one report, in a `handler` test, with no frame in that plan's files (R3) |
| A `MinifyResponse` task writing through `Store.SaveRawResource` (`proxy_service.go:461`) races with `Store.Close` in a test's deferred cleanup (`minify_golden_test.go:105`) | Confirmed from the logs (R3); the cleanup line is `:64` in the pristine archive's failing test and `:105` in the other tree's |
| It also appeared on a pristine archive of `5e8821e`, with the same stacks | Confirmed: I compared the two frame lists (R3) |
| Three earlier whole-package runs on earlier commits were clean | Not verified: I did not run earlier commits |
| It is intermittent, roughly once per whole-package run | Not confirmed: 5 of 5 whole-package runs of the gate on `5e8821e` were green and 0 were red (R1) |
| A targeted `-race -count=300` of `TestSubmitBackground_RunsTask` and `TestMinifyResponse_PreMarshalParity` did not reproduce it | Not re-run; consistent with my forced-overlap tests, which did not reproduce the gate-level form either (R2) |

### Conflict of evidence on how often the gate fails (stated, not resolved)

* **Reported red.** The other plan's agent's whole-package gate was red twice: once on its own tree (failing test
  `TestMinifyResponse_PreMarshalParity`) and once on a pristine archive of `5e8821e` (failing test
  `TestMinifyResponse_Golden`), each with one `DATA RACE` report and the same two stacks. Its logs that I could read also
  hold two whole-package gate logs that were green (I did not establish on which tree or commit); the agent reports
  three earlier green runs on earlier commits, which I did not see.
* **Mine, green.** Five whole-package runs of the same gate on a `git archive` copy of `5e8821e`, one after another through
  the heavy-run queue: five green, no report (R1).
* **What follows.** Both are recorded with their counts. They do not agree on a rate, and the rate is not established: a
  race seen twice by one agent and not in five runs by another on the same commit is consistent with a rare,
  load-dependent window (the queue shares the host with other jobs, so load differed), and equally with an effect of the
  other tree's state; I cannot tell which. This record's decision does not depend on the rate: the window is real and
  measured without the detector (R5), and the tests it adds pin its cause.
* **A third set of runs.** An independent reviewer is running the gate on `5e8821e`; its result will be added here by
  annotation. (2026-11-17: pending.)
  *Annotation, 2026-11-17:* the third source. The independent reviewer of record 0008 ran the whole-package handler
  race gate three times on `5e8821e` and three times on the 0008 tree: six green of six, and could not reproduce the
  race; its own probe saw about 20,000 writes during close refused cleanly, with no panic. So the counts stand at: red
  twice (the 0008 author), green five of five (the investigator, `5e8821e`), green six of six (that reviewer), and green
  five of five on this record's change (P6). The rate is not established, and the decision does not rest on it.
  *A further line, 2026-11-18:* the handler race gate on this record's change has now run 16 times, 16 green: P6 twice
  (10 of 10), the independent reviewer of this change 3, the coordinator 3. Against: red twice (the 0008 author), and on
  the base green 5 of 5 (the investigator) and 6 of 6 (the 0008 reviewer). The rate is still not established; the
  decision does not rest on it.

### Evidence index

All paths and lines are at `5e8821e` unless a module path says otherwise. "read" is by reading the source; "run" is a
measurement in a scratch copy (R-rows, More Information).

| ID | Fact | Where | Kind |
| :--- | :--- | :--- | :--- |
| E1 | `MinifyResponse` queues the raw-response save on the background pool; the task returns early if the request context is cancelled, calls `SaveRawResource`, and only logs an error | `internal/handler/proxy_service.go:441`, `:456-464` (guard `:457-459`, save `:461`) | read |
| E2 | The footer that advertises `mcp://magictools/raw/<id>` is appended whether or not the save landed | `internal/handler/proxy_service.go:481`, `:500`, `:535` | read |
| E3 | The pool: process-global, 8 workers, queue of 2048, created once, never closed, drained or cancelled; a full queue sheds the task; a panicking task is recovered | `internal/handler/background_pool.go:13-16`, `:18-35`, `:37-44`, `:49-55` | read |
| E4 | The pool's other users: `UpdateToolUsage` (a Badger write), `IncrementToolCalls` and `RecordToolError` (memory only) | `internal/handler/proxy_handlers.go:241`, `:352`; `internal/handler/proxy_pipeline_helpers.go:173`; `internal/db/badger.go:995-1010`, `:1478-1480` | read |
| E5 | `Store.Close` closes `closing`, cancels the store context, waits for `bgWg`, removes the PID file, releases the lock, closes the index, then `DB.Close`; nothing waits for a caller's write; a second call panics on `close(s.closing)` | `internal/db/badger.go:558-581` (`:560`, `:564`, `:580`) | read, run (R11) |
| E6 | `SaveRawResource` compresses and calls `DB.Update`; the entry is the `raw:` key with a one-hour TTL | `internal/db/badger.go:1442-1453` (TTL `:1450`) | read |
| E7 | The store's own goroutines already register with `bgWg` and test `closing`; a comment records that without this `Close` "can close the badger DB while this goroutine is mid-transaction, causing 'send on closed channel' panics during test teardown" | `internal/db/badger.go:231-243`; `internal/db/cache.go:231-247`; `internal/db/gc_helpers.go:91`; `internal/db/metrics_flush_helpers.go:130` | read |
| E8 | The two tests the gate reported: each builds a handler over a real store, defers `store.Close()`, calls `MinifyResponse` with `context.Background()` (so the `ctx.Err()` guard never fires) | `internal/handler/minify_golden_test.go:61-66`, `:102-107`; `internal/handler/common_test.go:13-45` | read |
| E9 | Stdio shutdown: `Run` returns, `app.cancel()`, return to `Start`, whose deferred `Stop` closes the store last; concurrently a goroutine started in `startStdioMode` waits for the same context and then calls `exitFunc(2)`, which is `os.Exit` | `cmd/mcp-server-magictools/main.go:458-477` (`:476`), `:47`, `:485-497`, `:324-364` (`:362`), `:368-369` | read |
| E10 | Service-mode shutdown: gate new requests, drain the IDE listener (5 s), `app.cancel()`, drain sub-servers, remove the state file, stop telemetry, shut the IPC server; the store is closed later by the deferred `Stop`; the pool is not drained | `cmd/mcp-server-magictools/main.go:1014-1060` (`:1016`, `:1028`), `:368-369` | read |
| E11 | Badger: `Update` and `View` refuse a closed handle on entry only; `Close` sets `isClosed` and `blockWrites`, stops the writers, then `close(db.writeCh)`; a commit sends on `db.writeCh` after an atomic `blockWrites` check; the package says "A DB instance should not be used after closing it" | `badger/v4@v4.9.1/txn.go:788-791`, `:806-809`; `badger/v4@v4.9.1/db.go:518-550`, `:526-528`, `:883-905` | read |
| E12 | The SDK cancels a request's context when it has sent the response; the tool handler's `ctx` is that context | `go-sdk@v1.6.1/internal/jsonrpc2/conn.go:524`, `:696` | read, run (R7) |
| E13 | Other goroutines that write to the store with nothing waiting for them: `FlushProxyCall` (per proxied call), `FlushHFSCTrace`, the proxy-metrics flusher (`SaveWithTTL`), auto-heal reindex, the sync-time prune and hydrate goroutines | `internal/client/manager.go:295`; `internal/client/lifecycle.go:824`; `internal/telemetry/proxy_metrics.go:56-78` with `internal/db/badger.go:2250`; `internal/handler/proxy_handlers.go:76-87`; `internal/client/sync_save_helpers.go:164`, `:309`, `:326` | read |
| E14 | The store's write sites: `DB.Update` at eight places in `badger.go` and one in `historical.go`, `DB.NewWriteBatch` at three, `DB.DropAll` at two, `updateOrWarn` at four more (three in `historical.go`, one in `cache.go`): eighteen | `internal/db/badger.go:245`, `:592`, `:1023`, `:1448`, `:2119`, `:2244`, `:2251`, `:2328`; `:365`, `:637`, `:1632`; `:2476`, `:2522`; `internal/db/historical.go:28`, `:93`, `:119`, `:142`; `internal/db/cache.go:242` | read |
| E15 | A raw `*badger.DB` is handed to the prompt cache, which writes through it | `internal/handler/handlers.go:76`; `internal/prompt_registry/cache.go:35`, `:139`, `:158` | read |
| E16 | Who reads the `raw:` space: the resource template; pipeline artifacts and the sync-failure message write the same space synchronously and publish the URI only when the save succeeded | `internal/handler/resource_handlers.go:11-22` (`:17-21`); `internal/handler/execute_pipeline.go:1021-1043`; `internal/client/sync_helpers.go:310` | read |
| E17 | No record or comment says why the save is off the request path; the pool's comment says only that a `go` per call used to pile up; history is one squashed commit (`6e65cdb`) | `internal/handler/background_pool.go:8-12` | read |
| E18 | Reads through a raw handle outside the store: `viewOrWarn(store.DB, ...)` | `internal/intelligence/hydrator.go:112`, `:262`; `internal/intelligence/hydrator_sweep_helpers.go:44` | read |
| E19 | Closures passed to the store's update entry points call no other update entry point, so a gate held across one write cannot nest | `scan_closures.txt`, scratch | run (R10) |
| E19, annotated 2026-11-18 | E19 holds for store calls only. In production the global logger routes every `slog` record to `Store.SaveLog` (`internal/logging/logging.go:108`, `:134-141`), a gated write, so every `slog` call inside an admitted write's closure is a nested admission: directly in `PurgeStaleServerTools` (three, from `internal/db/badger.go:1703` at `3443524`), `PruneOrphanedIntelligence` (`:1880`), `PurgeServerIntelligence` (`:1924`) and `ReconcileMetrics` (`:2001`), and through the `...OrWarn` helpers, which log on error. The counter makes it harmless: a nested admission is admitted or refused at once, never blocked (`TestStore_NestedAdmissionCannotDeadlockClose`); a read-write lock would have deadlocked against a waiting `Close` | `internal/logging/logging.go`; `internal/db/badger.go` | read |
| E20 | Tests: 31 call sites of `newTestHandler`, 30 `defer store.Close()` lines, seven test files that reach a pool-using path; `t.Cleanup` functions run after the deferred calls of the test body, so a cleanup registered by `newTestHandler` would run after `store.Close()` | `internal/handler/common_test.go:13-45`; `internal/handler/*_test.go` | read |
| R1 | Baseline: the whole-package gate on `5e8821e`, every run | More Information, `results/baseline.*`, scratch | run |
| R2 | Forced-overlap tests under `-race` on `5e8821e` | `results/det-head-*`, scratch | run |
| R3 | The detector's report: the two stacks (the other agent's log) | More Information | read |
| R4 | The probe under `-race` on `5e8821e` | `results/det-head-v4-race.log`, `results/det-head-v5-probe-race.log`, scratch | run |
| R5 | The product-level probe: a real store, four writers, `Close` at a random point, reopen and read back, on `5e8821e` and on the prototype | `results/probe3-*`, scratch | run |
| R6 | The options prototyped: contract tests, probe, whole-package gate | `results/gate*`, `results/det-*`, scratch | run |
| R7 | The SDK cancels the handler context after the response | `results/probe_ctx.txt`, scratch | run |
| R8 | The cost of a save and of the gate | `results/bench_save_raw.txt`, `results/bench_guard.txt`, scratch | run |
| R9 | The whole module under `-race` on the prototype | `results/module-A-c3.log`, scratch | run |
| R10 | The nesting scan | `results/scan_closures.txt`, scratch | run |
| R11 | The new store tests on `5e8821e` (with a one-line stub for the new identifier) and on the prototype; the planted breaks | `results/T_on_head.txt`, `results/T_on_protoA.txt`, `results/T1_refused.txt`, `results/plants_A.txt`, scratch | run |
| R12 | The read probe: four readers against `Close`, 200 iterations | `results/probe_read_head.txt`, scratch | run |
| E21 | Considered, left ungated: `RunValueLogGC` also writes through Badger (value-log GC rewrites live entries through `batchSet`, which sends on `db.writeCh`); Badger orders it against its own `Close` (`waitOnGC` takes the GC slot before `Close` goes on), and the GC loop runs in the errgroup that `Stop` waits for before `Store.Close` | `internal/db/gc_helpers.go:39`; `badger/v4@v4.9.1/value.go:234`, `:316`, `:1066-1074`; `badger/v4@v4.9.1/db.go:542-544` | read |
| E21, annotated 2026-11-18 | Also considered, left ungated: `DB.Sync()` (`syncOrWarn`), a write API missing from E21 above. It runs only inside the post-flush refresh, a `goBackground` goroutine that `Close`'s `bgWg.Wait` orders before Badger closes, so it cannot meet a closed database | `internal/db/io_helpers.go:80`; `internal/db/metrics_flush_helpers.go:134` | read |
| E22 | Considered, left ungated: the index writes go to bleve, which refuses a closed index under its own mutex (`ErrorIndexClosed`), with no panic and no race | `internal/db/index.go:516`, `:524`, `:568`, `:701`, `:729`, `:742`; `bleve/v2@v2.6.0/index_impl.go:318-326`, `:409-417`, `:429-434`, `:1336-1342` | read |
| E23 | On a closed database `txn.NewIterator` panics with `badger.ErrDBClosed`; `txn.Get` and a commit return errors | `badger/v4@v4.9.1/iterator.go:463-464`; `badger/v4@v4.9.1/db.go:749-752`, `:883-886` | read, run (R13) |
| E24 | Four store paths call `bgWg.Add` or `bgWg.Go` with nothing ordering them against `Close`'s `bgWg.Wait` (F9) | `internal/db/badger.go:234`, `:917`, `:564`; `internal/db/cache.go:234`; `internal/db/metrics_flush_helpers.go:130` | read, run (R14) |
| R13 | The write that outlived the close wait, on the prototype: iterator, `Get`, commit | `demo_pastbound.log`, scratch of P2 | run |
| R14 | The handler-level shutdown probe (P2): pool tasks, bare-goroutine flushes, synergy records and usage counts against `Close`, on `3443524` and on the change | `hprobe-*.log`, scratch of P2 | run |

### The race, as reported (R3)

The other plan's agent's gate logs (a pristine archive of `5e8821e`, failing test `TestMinifyResponse_Golden`; and a
second tree, failing test `TestMinifyResponse_PreMarshalParity`) carry one report each, with the same two stacks. I
compared their frame lists: they differ only in the test name and line (`minify_golden_test.go:64` and `:105`). I did not
produce either of these two reports; I produced the same report from a store-level probe (below). Paths shortened,
addresses omitted:

```text
WARNING: DATA RACE
Read at <addr> by goroutine <pool worker>:
  runtime.chansend1()                       runtime/chan.go:162
  badger/v4.(*DB).sendToWriteCh()           badger/v4@v4.9.1/db.go:904
  badger/v4.(*Txn).commitAndSend()          badger/v4@v4.9.1/txn.go:593
  badger/v4.(*Txn).Commit()                 badger/v4@v4.9.1/txn.go:663
  badger/v4.(*DB).Update()                  badger/v4@v4.9.1/txn.go:820
  internal/db.(*Store).SaveRawResource()    internal/db/badger.go:1448
  internal/handler.(*ProxyService).MinifyResponse.func1()   internal/handler/proxy_service.go:461
  internal/handler.runBackgroundTask()      internal/handler/background_pool.go:43
  internal/handler.bgPool.func1.1()         internal/handler/background_pool.go:29

Previous write at <addr> by goroutine <the test>:
  runtime.recvDirect()                      runtime/chan.go:405
  badger/v4.(*DB).close()                   badger/v4@v4.9.1/db.go:550
  badger/v4.(*DB).Close.func1()             badger/v4@v4.9.1/db.go:521
  sync.(*Once).doSlow()                     sync/once.go:78
  internal/db.(*Store).Close()              internal/db/badger.go:580
  internal/handler.TestMinifyResponse_PreMarshalParity.deferwrap2()   internal/handler/minify_golden_test.go:105
  runtime.deferreturn() / testing.tRunner()
```

My own reproduction (R4): the probe of R5 under `-race`, 600 iterations, produced one report whose Badger frames are
identical, line for line, to the two above: the send at `badger/v4@v4.9.1/db.go:904`, reached through
`badger/v4@v4.9.1/txn.go:593`, `:663` and `:820` and `internal/db/badger.go:1448`, against the close at
`badger/v4@v4.9.1/db.go:550`, `badger/v4@v4.9.1/db.go:521` and `internal/db/badger.go:580`. Only the frames above
`SaveRawResource` and `Store.Close` differ, being the probe's own goroutine and test.

Which memory: the channel `db.writeCh` inside Badger. `badger/v4@v4.9.1/db.go:550` is `close(db.writeCh)` and `badger/v4@v4.9.1/db.go:904` is
`db.writeCh <- req`; the detector treats a channel close and a send with no ordering between them as a race on the
channel. So the report is about Badger's own code, reached through this repository's calls to `Store.Close` and
`Store.SaveRawResource`; this repository's code is the cause because it provides no ordering between the two calls. The pool worker
in the first log was created by `TestSubmitBackground_RunsTask` (the pool is global and outlives every test), which is why the
race can appear in a test that does not mention the pool.

### Who starts the background writers, and what stops them (E3, E4, E5, E7, E13)

Every goroutine or task that can touch the store after `Close` has begun, at `5e8821e`:

| Task | Started where | Writes what | Bounded by | Stopped by |
| :--- | :--- | :--- | :--- | :--- |
| raw-response save | `MinifyResponse`, `proxy_service.go:456` (pool) | `raw:<id>` (Badger, 1 h TTL) | the pool's queue of 2048 (shed when full); the request context, tested once at start (F1) | nothing: the pool is never drained or closed |
| tool-usage count | `CallProxy`, `proxy_handlers.go:241` (pool) | `usage:<urn>` (Badger, `UpdateWithRetry`) | same | nothing |
| call count, error class | `proxy_handlers.go:352`, `proxy_pipeline_helpers.go:173` (pool) | memory only (`metricsBuf`), flushed later | same | nothing; a count added after the last flush is lost |
| proxied-call record | `manager.go:295` (bare `go`) | `proxy:call:*` (Badger, 7 d TTL) | nothing | nothing; no `recover` |
| model-trace record | `lifecycle.go:824` (bare `go`) | `telemetry:trace:*` (Badger, 30 d TTL) and the index | nothing | nothing; no `recover` |
| proxy-metrics flusher | `StartMetricsFlusher`, `main.go:1083` | `proxy_metrics:*` (Badger, 30 d TTL) | a queue it ranges over for ever | nothing; no `recover` |
| auto-heal reindex | `triggerAutoHeal`, `proxy_handlers.go:76` | the search index, reads Badger | one in flight (`IsHealing`) | nothing |
| sync-time prune and hydrate | `sync_save_helpers.go:164`, `:309`, `:326` | Badger and the index | the sync context, tested once at start | the sync context |
| tool-metric flush, synergy flush, L2 cache write, reindex, GC | inside `internal/db` | Badger | `bgWg` and `closing` | `Close` (`badger.go:560-564`) |
| the background GC loop | `StartBackgroundGC`, `main.go:1110` | flushes metrics on exit | the errgroup context | `eg.Wait` in `Stop` |

The last two rows are the store's own or ordered by the errgroup and are already ordered against `Close`; the first
eight are not, and `Close` cannot see them.

### The product's shutdown, and what a write that meets `Close` does (E5, E9, E10, E11, R5)

Stdio mode (the IDE-managed child): `Run` returns when the IDE closes the pipe or the context is cancelled, `Start`
returns, and its deferred `Stop` disconnects the sub-servers, waits for the errgroup, and closes the store last (E9).
Service mode: `performGracefulShutdown` gates new requests, drains the IDE listener for up to 5 s, cancels, drains
sub-servers, then `Stop` closes the store (E10). Neither mode drains the pool, and `Close` does not wait for anything
but its own goroutines (E5). A tool call being minified when shutdown starts can therefore submit a save that runs
before, during or after `Close`.

What Badger does with a write that meets `Close` (module source, v4.9.1, E11): `DB.Update` returns `ErrDBClosed` when
the handle is already closed ("DB Closed"); a commit returns `ErrBlockedWrites` ("Writes are blocked, possibly due to
DropAll or Close") when `Close` has begun. Both checks are made before the write starts. A write that has passed them
when `Close` begins is in a window Badger does not protect: `DB.Close` stops the writer goroutine and then closes
`db.writeCh` (`badger/v4@v4.9.1/db.go:538-550`), and a commit sends on that channel (`badger/v4@v4.9.1/db.go:904`). The package's own words: "A DB instance
should not be used after closing it" (`badger/v4@v4.9.1/db.go:526-528`), and `Close` is "crucial ... to ensure all the pending updates make
their way to disk" (`badger/v4@v4.9.1/db.go:515-517`).

Measured on `5e8821e` (R5): a real store, four goroutines calling `SaveRawResource` in a loop, `Close` at a random
point up to 6 ms in, then the directory reopened and every acknowledged save read back.

* 1200 iterations: 12 panics, 10 writers that never returned, 1200 of 1200 reopens ok, 0 iterations with an acknowledged save missing; refusals: 1807 `DB Closed`, 2966 `Writes are blocked`, 0 `store is closing`; 568244 saves ok.
* An earlier series, without the reopen check: 930 iterations: 12 panics, 9 writers that never returned.
* No acknowledged write was lost and the store opened every time. The hostile duty cycle (four writers that never pause)
  is far above any real rate: these are the existence and kinds of outcomes, not a rate for the product.

So the answer to "can the product do what the test does" is yes, in a narrow window. By reading, the pool is usually
idle at shutdown (its depth then was not measured), and a save that starts after the response was sent is skipped by the
`ctx.Err()` guard in any case (F1), so the window for `MinifyResponse` is the few milliseconds around the last response. The window for the
bare goroutines (E13) has no such guard.

### What a write after `Close` costs a user (E2, E6, E16, R5)

The `raw:` space is a one-hour cache of full tool output, read through `mcp://magictools/raw/{id}` (E16). Losing an
entry costs a client the full text behind a summary: `GetRawResource` fails and the resource read reports "resource not
found". It holds no state anything else depends on. A lost usage or call counter skews a ranking slightly. The panic in
a pool worker is recovered (E3) and logged; the same panic in a bare goroutine has no `recover` (F4) and ends the
process, but only while it is shutting down. A store that will not reopen after such a shutdown was not seen: 1200 of
1200 reopens succeeded on `5e8821e` (R5), including iterations in which a panic and a hung writer occurred.

### Findings beyond the race (not fixed here; each is a candidate for its own record)

* **F1. The `ctx.Err()` guard guards the wrong thing.** `proxy_service.go:457-459` skips the save when the request
  context is cancelled, with the comment "store may be closing". The SDK cancels that context as soon as it has written
  the response (E12), and a probe (R7) shows a task that starts 2 ms or more after the handler returns sees it
  cancelled and does not save, while the response already carries the `mcp://magictools/raw/<id>` footer (E2). A save
  that waits behind a busy pool is therefore dropped silently and the link dangles. The guard says nothing about the
  store.
* **F2. Stdio shutdown may never reach `Store.Close`.** `startStdioMode` starts a goroutine that waits for the
  application context and then calls `os.Exit(2)` (E9). `app.cancel()` is what starts it, and `Stop`, which closes the
  store last, runs after the sub-servers are disconnected. By reading, the process can exit before the store is closed.
  Not verified by running the server (the rules forbid starting it).
* **F3. Raw Badger handles leave the store.** The prompt cache writes through a `*badger.DB` it was handed (E15), and the
  intelligence package reads through `store.DB` (E18). A gate in `Store` cannot cover them.
* **F4. Bare goroutines have no `recover`.** The pool recovers a panicking task (`background_pool.go:37-44`); the
  proxied-call record, the trace record and the metrics flusher (E13) do not.
* **F5. A second `Close` panics** (`close(s.closing)`, `badger.go:560`; shown by T4 on `5e8821e`, R11).
* **F6. Reads share the window in principle.** `View` has the same entry-only check (E11). Measured on `5e8821e` (R12):
  four readers calling `GetRawResource` in a loop against `Close`, 200 iterations: 325,224 reads ok, 800 refused with
  Badger's `DB Closed` (every reader ended that way), no panic, no hang. That is no evidence of a fault, and no proof
  that none exists (Open question Q4).
* **F7. The pool is dropped at exit.** Tasks still queued when the process ends never run (E3), so the last calls'
  usage counters are lost; the loss is as old as the pool.
* **F8. The `internal/telemetry` tests do not survive `-count=3` (R9).** `TestProxyRelayTracker`
  (`internal/telemetry/boot_spans_extra_test.go:32`, "expected 0 count initially") and `TestProxyMetrics`
  (`internal/telemetry/proxy_metrics_extra_test.go:28`, "expected save function to be called") fail on the second and
  third round of the whole-module run. They are not data races and not this defect. By reading, the second is the
  flusher of E13 again: `StartMetricsFlusher` starts a goroutine that ranges over a package-level queue for ever
  (`internal/telemetry/proxy_metrics.go:56-78`), so the first round's goroutine, holding the first round's `save`
  function, can take the second round's event. The cause of the first was not examined. On `5e8821e`, `go test -race -count=3 ./internal/telemetry/` exits 1 with failing tests TestProxyMetrics, TestProxyRelayTracker, and `-count=1` exits 0, so the failures are older than the prototype and independent of it.
* **F9. `Close` can panic on its own WaitGroup (added 2026-11-17, during P2; not fixed here).** `RecordSynergy`
  (`internal/db/badger.go:234`), the async re-index in `SaveIntelligence` (`:917`), the L2 response-cache write
  (`internal/db/cache.go:234`) and the post-flush refresh (`internal/db/metrics_flush_helpers.go:130`) call
  `bgWg.Add` or `bgWg.Go` with nothing ordering them against `Close`'s `bgWg.Wait()` (`internal/db/badger.go:564`).
  A handler-level probe (R14) that keeps `RecordSynergy` running while `Close` runs panicked with `sync: WaitGroup is
  reused before previous Wait has returned` inside `Store.Close` in 2 of 3 runs of 100 iterations on `3443524` (the
  third run ended with a pool worker that never returned, the hung writer of R5), and in 3 of 3 runs of 100 iterations
  on the change. It is not a write and not in the 18 sites; the gate neither causes nor fixes it. Put to the owner.
  *Annotation, 2026-11-17:* fixed in this record by the fourth ruling ("Decided, fourth ruling" under Context): the four
  starts go through the gate.
* **F10. A write that outlived the close wait and meets the closed database without a panic gets the library's error**
  ("DB Closed" or "Writes are blocked, possibly due to DropAll or Close"), not `ErrStoreClosing`: the third ruling
  converts panics only. Measured on the change with the wait cut to 1 ns (P2): 164 such errors in 2400 iterations.
  *Annotation, 2026-11-18 (the coordinator's decision; the owner told):* accepted as recorded. The independent reviewer
  checked that no caller branches on `ErrStoreClosing`, and judged that the second ruling's "named error" governs the
  refusal of a write that starts after `Close` has begun, while the third ruling covers panics only; a late write that
  gets an error from the closed database keeps the library's error.
* **F11. A wipe can race `Close` on the index pointer (added 2026-11-17; not fixed here).** `WipeAll` and `WipeDatabase`
  (`internal/db/badger.go:2472-2501`, `:2520-2543` at `3443524`; the assignments at `:2494`, `:2535`) replace `s.Index` after their `DropAll`, outside the
  gate, while `Close` closes `s.Index`. `WipeAll` runs only from the CLI's `db wipe` (`cmd/mcp-server-magictools/main.go:157`);
  `WipeDatabase` has no caller outside tests. Not a shutdown path.
  *Annotation, 2026-11-18 (the independent review; the owner's ruling "Accept it here and record the panic; fix it with
  the raw-handle follow-up (Recommended)"):* F11 is worse than recorded above. A `WipeAll` concurrent with `Close` makes
  `Close` panic with "close of closed channel" at `bleve/v2@v2.6.0/index/scorch/scorch.go:443` (`close(s.closeCh)`),
  reached through `SearchIndex.Close` (`internal/db/index.go:763`) and `Store.shutdown` (`internal/db/badger.go:795` in
  the change); the reviewer saw it with and without `-race`, on the first iteration. The close-once is then spent, so
  Badger is never closed and a later `Close` returns nil (`Close`'s doc comment says so). It is not reachable in the
  server: the only callers are the command-line `db wipe`, which runs by itself, and tests. Accepted here; it joins the
  follow-up record for raw handles, with F3 (the prompt cache's writes through the raw database handle,
  `internal/prompt_registry/cache.go:139`, `:158`).

No other `DATA RACE` was reported by the whole-module run (R9): nothing else in the packages that have tests is waiting
behind this one. The one package without test files, `internal/prompt_registry`, is not covered, and it holds the raw
handle of F3.

## Decision Drivers

* **The gate must be able to pass honestly.** No suppression, no skipped test, no widened timeout, no sleep that
  hides an ordering.
* **No data lost or corrupted at shutdown.** Every write acknowledged before `Close` returns is readable after the
  directory is reopened; the store opens next time.
* **No crash and no hang.** A write that meets shutdown returns an error; it never panics, and nothing waits for ever.
  *Made precise by the third ruling (2026-11-17):* no panic that the closed database raises in a gated write reaches
  that write's caller; a write that blocks inside the library after the close wait is not covered (its goroutine stays
  blocked until the process exits).
  *A further line, 2026-11-17 (fourth ruling):* nor does `Close` itself panic on the store's own goroutine starts: none
  can start once `Close` has begun, and every start counted before is counted before `Close` waits (F9).
* **No unbounded shutdown.** Whatever waits for in-flight work has a bound, and a task that never returns cannot hold
  the process.
* **Smallest change that closes the product's window,** not only the test's.
* **No collision with the plans in flight** (the files named in the PLAN's scope).
* **A refused write is visible,** as an error a caller can test for, not a silent drop.

## Considered Options

* **A. The store's `Close` waits for the writes it admitted and refuses later ones.** A write gate inside `Store`: a
  counter and a closed flag under a mutex; every write entry point of `internal/db` passes through it; `Close` closes
  the gate first and waits, bounded, for the counter to reach zero.
* **B. The owner of the pool drains it before the store closes.** A pending counter and a bounded `DrainBackground` in
  the handler package, called from `Stop` before `app.store.Close()`, and from the tests.
* **C. The minifier's save is made synchronous,** bounded by the request: the pool is not used for the raw save.
* **D. Tests only.** The tests drain the pool before they close the store; the product is untouched.
* **E. Do nothing and mark the race as known.**

## Decision Outcome

Chosen option: **A**, the store gate (the owner's ruling on Q1, with the answers to Q2 to Q4 above), because it is the only option that makes the window
safe for every writer that goes through `Store` at once, the pool's and the seven bare goroutines' (E13), without touching `background_pool.go`,
`main.go` or any handler file; it turns the panic, the hung writer and Badger's two error texts into one documented
error (R5, R11); its wait is bounded; and it follows a pattern the store already uses for its own goroutines (E7).

Sketch (the prototype, R6, is in the scratch folder; the plan has the full change):

```go
// ErrStoreClosing is returned by a write that starts after Close has begun.
var ErrStoreClosing = errors.New("store closing")

type writeGate struct {
    mu     sync.Mutex
    closed bool
    n      int           // admitted, unfinished writes
    idle   chan struct{} // closed when closed && n == 0
}

func (s *Store) writing() (end func(), err error) // refuses with ErrStoreClosing once closed

func (s *Store) Close() error {
    first, drained := s.gate.closeAndWait(storeDrainTimeout) // refuse new writes, wait up to 5 s
    if !first { return nil }                                  // a second Close is a no-op (F5)
    // ... the existing steps, unchanged
}
```

The prototype's error text was `store is closing`; the ruled text is `store closing`.

The gate is a counter, not a read-write lock, on purpose. A first prototype used `sync.RWMutex`: it passed the same
probe (1200 iterations: 0 panics, 0 writers that never returned, 1200 of 1200 reopens ok, 0 iterations with an acknowledged save missing; refusals: 0 `DB Closed`, 0 `Writes are blocked`, 4800 `store is closing`; 457852 saves ok), but a write admitted inside another admitted write would deadlock against a `Close` waiting for
the write lock, and a read-write lock cannot give `Close` a deadline. The scan of E19 found no such nesting today; the
counter makes it impossible by construction.

### Measured results (R5, R6, R8, R9, R11)

Every row is a prototype in a scratch copy of `5e8821e`. "Gate" is `go test -race -count=20 -timeout 30m
./internal/handler/...`, run through the shared heavy-run queue; the baseline row (option E) is `5e8821e` itself, run
5 times.

| Option | Contract tests (new store tests T1 to T5) | Product probe, 4 writers, `Close` at a random point | Whole-package gate | Wait or lock risk | Visible to a client | Files touched |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| A, store gate | fail on `5e8821e`, pass on the prototype (R11) | 900 iterations: 0 panics, 0 writers that never returned, 900 of 900 reopens ok, 0 iterations with an acknowledged save missing; refusals: 0 `DB Closed`, 0 `Writes are blocked`, 3600 `store is closing`; 363852 saves ok | 5 of 5 green | counter gate: a nested admission cannot block; `Close` waits at most 5 s for admitted writes | a save refused at shutdown returns `ErrStoreClosing`, logged at warn | `internal/db/badger.go`, `cache.go`, `historical.go`, `io_helpers.go`; new tests |
| B, drain the pool | unchanged by construction: `Store.Close` is not edited, so T1 to T5 fail as on `5e8821e`; the shutdown-order test: not run (the shared heavy-run queue did not reach it) | unchanged by construction (the store is not edited); the bare goroutines (E13) are not covered | not run (the shared heavy-run queue did not reach it) | a drain waits for tasks that may not return; bounded by a timer, after which the old race is back | none | `internal/handler/background_pool.go`, `cmd/mcp-server-magictools/main.go`, tests in two or more files |
| C, synchronous save | the save cannot be queued behind `Close`; T1 does not apply; T3 to T5 fail as on `5e8821e` | unchanged by construction | 2 of 2 green | none new | each large response waits for its save: 0.1 to 0.2 ms at 1 KB, 0.4 to 1 ms at 100 KB, about 2.5 ms at 1 MB (R8); the footer can no longer dangle for a save the pool dropped | `internal/handler/proxy_service.go` |
| D, tests only | T1 to T5 fail as on `5e8821e` | unchanged: the window stays open in the product | not run (the shared heavy-run queue did not reach it) | none | none | `internal/handler/minify_golden_test.go` and up to six more test files (E20) |
| E, do nothing | fail | unchanged | the baseline: 5 of 5 whole-package runs of the gate on `5e8821e` were green and 0 were red | none | none | none |

Cost of the gate itself (R8): a counter increment and decrement under a mutex is about 20 ns uncontended and about
100 ns when ten cores hammer it, against a save that takes 100 microseconds to 2.5 milliseconds. The save benchmark on
the shared host is too noisy to show a difference between the trees (the medians moved by -31 % to +7 % between sizes);
the guard cost is below that noise.

Forced-overlap and detector runs on `5e8821e` (R2, R4), each under `-race`:

* the three forced-overlap tests, 30 rounds each: exit 0, 0 `DATA RACE` report(s), 0 panic line(s), failing: none;
* the probe of R5, 40 iterations: exit 0, 0 `DATA RACE` report(s), 0 panic line(s), failing: none;
* the probe of R5, 600 iterations: exit 1, 1 `DATA RACE` report(s), 0 panic line(s), failing: TestProbe_WriteDuringClose.

No reproduction of the report is deterministic: the window is inside Badger, between the `blockWrites` check at
`badger/v4@v4.9.1/db.go:884` and the send at `badger/v4@v4.9.1/db.go:904`, and no seam in this repository can hold a
writer there. What is deterministic is its cause: `Close` returns while an admitted write is in flight (T5, fails on
`5e8821e` every time) and a save that runs after `Close` is not refused with a documented error (T1, T3, fail every
time).

The whole module under `-race` on prototype A (R9), `go test -race -count=3 -timeout 40m ./...`: exit 1, 0 `DATA RACE` report(s), 0 panic line(s), failing: TestProxyMetrics, TestProxyRelayTracker; 18 packages ok, 1 FAIL (internal/telemetry), 1 without test files.
The one failing package is `internal/telemetry` (F8). Nothing else was reported, so nothing else waits behind this
defect in the packages that have tests.

## Consequences

* Good, because the gate can pass honestly: no test is changed to hide an ordering, and the window the detector reports
  is closed for every writer in `internal/db`, including the seven that nothing waits for today (E13).
* Good, because a write that meets shutdown returns `ErrStoreClosing` instead of panicking, hanging or returning one of
  Badger's two texts; the existing `...OrWarn` helpers already log an error at warn level, so no caller changes.
* Good, because `Close` becomes safe to call twice (F5), which `Stop`, the signal handler and the tests may do.
* Neutral, because a save refused at shutdown is lost; today it is lost or crashes. The data involved is a one-hour
  cache and counters.
* Bad, because every write entry point in `internal/db` takes a mutex for a few nanoseconds; and a new rule enters the
  package: a write must go through `writing()`. A write added later that bypasses it is outside the gate; the plan adds a
  `grep` to the review for it, not a lint rule.
* Bad, because the gate cannot cover a raw `*badger.DB` that leaves the store (F3), or reads (F6, Q4).
* Bad, because `Close` can now wait up to 5 s at shutdown for a write that is slow; the bound exists so that a stuck
  disk cannot hold the process (Q3).
* Bad, because F1 and F2 stay: a dropped save still leaves a dangling footer, and stdio shutdown may still never close
  the store. Each needs its own record.

### Confirmation

1. T1 to T5 fail on `5e8821e` (they compile there through a text-matching helper, since the sentinel does not exist yet; T6, the
   bound, joins the change) and pass on the prototype; T2 fails
   there on the first iteration (Badger's error text is not `ErrStoreClosing`) and, when the window is hit, by a panic or
   a hang (R11). The plan records each failure's first lines.
2. A planted break per new assertion on a scratch copy of the change, each seen to fail the named tests: six plants
   on the prototype (R11).
3. The probe of R5 on the change: no panic, no hung writer, every acknowledged save present after reopen.
4. `go test -race -count=20 -timeout 30m ./internal/handler/...`, run the same number of times as the baseline
   (5), every run exit 0 with no `DATA RACE`.
5. `go test -race -count=3 -timeout 40m ./...`, exit 0, or each failure named and shown to be outside this change.
   *Annotation, 2026-11-18:* owed. The whole-module run is not allowed on this machine until the orphan-reaper hazard
   is fixed under its own record (the owner's ruling, under "Independent review and the decisions after it"); it is run
   after that fix lands. The earlier whole-module runs (P6, 2026-11-18 02:52-03:36 and 04:49-05:45 UTC) predate the
   edits of 2026-11-18.
6. `make lint` under golangci-lint v2.13.1 and v2.15.0, `go vet`, `gofmt`, per-file `golint`, `make check-records`,
   `make markdownlint`.
7. An independent review of the diff before it is staged (the change alters shutdown).

## Pros and Cons of the Options

### A. The store gate

* Good, because it closes the window for every writer that goes through `Store`, whoever starts it and whether or not
  anything waits for it (E13).
* Good, because it touches `internal/db` only; no handler file, `main.go` or file any plan in flight changes.
* Good, because a refused write is an error with a name a test can assert (Q2).
* Neutral, because the prototype guards three entry points (`SaveRawResource`, `UpdateWithRetry*`, `SaveWithTTL`); the
  change guards all eighteen (E14, Q4).
* Bad, because it does not cover a raw handle (F3) or reads (F6).
* Bad, because the bound (Q3) is a number someone must pick and defend.

### B. Drain the pool before the store closes

* Good, because it lets the last queued tasks finish at shutdown instead of being refused.
* Bad, because it covers the pool's tasks only: the seven bare goroutines (E13) are outside it, and the race is still
  there for them.
* Bad, because the tests cannot be fixed by it from `newTestHandler` alone: a `t.Cleanup` runs after the test body's
  `defer store.Close()` (E20), so every one of up to seven test files must drain before it closes, or `Store` must
  carry a hook that `NewHandler` registers (which is option A's lifecycle with extra moving parts).
* Bad, because a drain waits for arbitrary tasks; a task that never returns holds shutdown until the timer fires, and
  then the old race is back.
* Bad, because by F2 the benefit in stdio mode is shallow: the process can exit before `Stop` runs.

### C. Synchronous save

* Good, because the save can no longer be queued behind `Close`, and the footer can no longer advertise an entry the
  pool dropped (F1).
* Bad, because it adds the save's latency to every large response (R8) for a cache write nothing waits on; and no
  record says why it was asynchronous (E17), so the cost of undoing that is unknown.
* Bad, because it covers one writer of the nine: `UpdateToolUsage` and the bare goroutines stay in the window.
* Bad, because the request can still be in flight when the store closes (HTTP mode, E10).

### D. Tests only

* Good, because it is the smallest diff and cannot change the product.
* Bad, because the product can do what the test does (R5): this turns the gate green and leaves a panic and a hung
  writer possible at shutdown. The brief's own condition for D, that the product cannot do it, is not met.
* Bad, because every present and future test that reaches the pool (seven files today, E20) must remember to drain; one
  that does not is a new flake.

### E. Do nothing, mark the race as known

* Good, because nothing changes.
* Bad, because every later plan's required gate carries a known intermittent red, which the owner's ruling has just
  declined to accept.
* Bad, because the product window stays.

## More Information

### Open questions for the owner

In the order to answer them. Each has three options, the recommended one first. **All four were answered on 2026-11-17
(second ruling under Context): option 1 of each, as recommended. The text below is as asked.**

**Q1. Which fix?** Answered: option 1.

1. **A: the store gate (recommended).** Cost: edits to four files in `internal/db` and two new test files; a refused write
   returns an error; no handler or `main.go` change; no collision with the plans in flight.
2. **A plus B: the store gate, and the pool drained in `Stop` before the store closes.** Cost: A, plus
   `background_pool.go` and `main.go`; the last queued tasks finish at shutdown instead of being refused. By F2 the
   benefit is shallow in stdio mode.
3. **D: tests only.** Cost: the gate turns green and the product keeps the window of R5 (a panic and a hung writer
   at shutdown, measured); the record must then carry the product defect as known. Not recommended.

**Q2. What does a writer do when the store is closing?** Answered: option 1.

1. **Return `db.ErrStoreClosing`, an exported sentinel; callers log it and carry on (recommended).** Cost: none for
   existing callers, whose `...OrWarn` helpers already log an error at warn level; a caller that cares tests
   `errors.Is`.
2. **Return nil and count the drop in a metric.** Cost: a caller cannot tell a refused write from a stored one, so a
   test cannot assert that nothing was written; the count is the only trace.
3. **Wait, bounded, for the close to finish and then return Badger's `ErrDBClosed`.** Cost: a writer blocks at
   shutdown for no gain; the error text is Badger's, not ours.

**Q3. What bounds a shutdown drain?** Answered: option 1.

1. **A fixed bound of 5 seconds, then warn and continue (recommended).** Cost: it is the figure the IDE listener
   already uses (`main.go:1028`); after the bound the race of E11 is possible again, only under a stuck disk or a
   writer that never returns. With the gate, the admitted work is one Badger transaction, so the bound is a backstop.
2. **No bound: wait until every admitted write returns.** Cost: simplest code; a stuck disk holds shutdown for ever,
   the thing the decision drivers rule out.
3. **The remaining part of the existing 30-second shutdown deadline** (`main.go:1016`). Cost: it ties the store to
   the service-mode path only; stdio has no such deadline (F2).

**Q4. Which store accesses does the gate cover?** Answered: option 1.

1. **Every write in `internal/db` (E14), reads left alone (recommended).** Cost: eighteen call sites in four files;
   reads in flight at `Close` keep their window (F6: no fault in 200 measured iterations, R12).
2. **Writes and reads.** Cost: about forty more call sites (every `View` and `viewOrWarn`), a wider review, a larger
   diff; closes F6.
3. **Only the paths the pool reaches (`SaveRawResource`, `UpdateWithRetry`, `SaveWithTTL`).** Cost: smallest, and what
   the prototype does; leaves `FlushProxyCall`, `FlushHFSCTrace`, the batch and wipe paths in the window.

### Experiments (2026-11-17, scratch copies of `5e8821e`, nothing in the working tree)

How the real store is never opened: every `NewStore` call in an experiment takes a path made under the system temporary
directory (the handler tests' `os.MkdirTemp`, the db tests' and probes' `t.TempDir`, the benchmarks' `b.TempDir`), and
none takes a configured data path; the server was never started; no credential file was read. The existing handler
tests do read the owner's `servers.yaml` (the other plan's log shows the load line), as every run of the gate does; that
file's modification time (2026-09-08) and the data directory's (2026-11-17, hours before this work began) were
unchanged after my runs.

* **R1, baseline.** The gate on a copy of `5e8821e`, one run after another, each through the heavy-run queue (the
  seconds are the run alone, without the queue wait):

| Run | Exit | Seconds (run only) | `DATA RACE` reports | Failing tests |
| :--- | :--- | :--- | :--- | :--- |
| 1 | 0 | 301 | 0 | none |
| 2 | 0 | 357 | 0 | none |
| 3 | 0 | 304 | 0 | none |
| 4 | 0 | 296 | 0 | none |
| 5 | 0 | 285 | 0 | none |

  5 of 5 whole-package runs of the gate on `5e8821e` were green and 0 were red. The other agent saw the race on two trees and estimated it at roughly once per whole-package
  run; these runs do not show that rate. The queue shares the host with other agents' jobs, so load differed between
  its runs and mine.
* **R2, R4, forced overlap under `-race` on `5e8821e`.** Three tests (a save queued behind parked workers and released
  once `Close` had removed the PID file; 400 saves released as `Close` starts; a feeder submitting saves continuously
  while `Close` runs), 30 rounds each; the first of them alone at 50 rounds (exit 0, 0 `DATA RACE` report(s), 0 panic line(s), failing: none); and the probe of R5 under
  `-race`. The results are listed under Measured results.
* **R5, the probe.** `internal/db` test, scratch only: a real `Store`, four goroutines calling `SaveRawResource` of a
  4 KB value in a loop until refused, `Close` after a random 0 to 6 ms, each writer's outcome tallied (nil, error text,
  panic recovered, or no return within 10 s), then the same directory reopened with `NewStore` and every acknowledged
  id read back with `GetRawResource`. `5e8821e`: 1200 iterations: 12 panics, 10 writers that never returned, 1200 of 1200 reopens ok, 0 iterations with an acknowledged save missing; refusals: 1807 `DB Closed`, 2966 `Writes are blocked`, 0 `store is closing`; 568244 saves ok. Prototype A (counter gate): 900 iterations: 0 panics, 0 writers that never returned, 900 of 900 reopens ok, 0 iterations with an acknowledged save missing; refusals: 0 `DB Closed`, 0 `Writes are blocked`, 3600 `store is closing`; 363852 saves ok.
  First prototype (read-write lock): 1200 iterations: 0 panics, 0 writers that never returned, 1200 of 1200 reopens ok, 0 iterations with an acknowledged save missing; refusals: 0 `DB Closed`, 0 `Writes are blocked`, 4800 `store is closing`; 457852 saves ok.
* **R7, the request context.** An in-memory MCP client and server; a tool handler that submits four pool tasks delayed
  0, 2, 20 and 200 ms and records `ctx.Err()`: nil for the first, "context canceled" for the other three, in three runs
  out of three.
* **R8, cost.** `SaveRawResource` of 1 KB, 100 KB and 1 MB of pseudo-random text, six alternating runs per tree: HEAD
  medians 150 us, 695 us, 2449 us; prototype 103 us, 608 us, 2632 us (noise, not a speed-up); the gate alone 20 ns serial,
  92 to 115 ns with ten goroutines (Apple M1 Pro).
* **R10.** A scan of every closure passed to an update entry point in `internal/db` found no call to another update entry
  point; the one store method called inside, `flushLoadedMetrics`, takes the transaction it is given.
* **R11, the contract tests.** On `5e8821e` with a stub for `ErrStoreClosing`: T2 fails on its first iteration ("refusal
  is not ErrStoreClosing: DB Closed"), T3 fails for all three entry points, T4 fails with "close of closed channel",
  T5 fails with "Close returned (<nil>) while an admitted write was still in flight", T1 fails with "the queued save failed
  with DB Closed". On the prototype all pass; T6 (the bound) passes in 0.37 s. Six planted breaks of the prototype, each
  seen to fail the tests the plan names for it (always admit; no wait; no bound; `Close` skips the gate; no
  second-call guard; `SaveRawResource` skips the gate).
* **R6, R9.** The prototypes B, C and D differ from `5e8821e` in one place each (the pool, `MinifyResponse`, two tests);
  the whole-package gate and the forced-overlap tests ran on each, and the whole module under `-race -count=3` on A.
  Results are in the table above and the paragraph after it.

### Not verified

* The gate-level report (a pool task against a closing store) was not reproduced by any of my runs on `5e8821e` (R1, R2);
  the two stacks of that form are quoted from the other plan's agent's logs, and that it is intermittent at roughly one
  per whole-package run is that agent's observation. The store-level form was reproduced once in 600 iterations (R4). I
  did not re-run the three earlier clean runs on earlier commits or the `-count=300` targeted run it mentions.
* Whether the send in the report also panicked in those runs: the logs show no "send on closed channel" line, and the
  pool recovers and logs such a panic.
* F2: that stdio shutdown can exit before `Store.Close`: read, not run (the rules forbid starting the server).
* Whether a tool call in service mode can outlive `ideSrv.Shutdown`'s five seconds and `app.cancel()` and still submit
  a save: not established.
* F6: that reads at `Close` cannot fault: 200 iterations (R12) saw none. `bgWg.Add` concurrent with `bgWg.Wait` in the
  store's own goroutines: not examined by a run.
* The prototypes were not built or linted for linux/amd64 and windows/amd64 and not linted under either
  golangci-lint version; the plan makes those gates.
* **Gates not run.** The shared heavy-run queue was the limit: one whole-package gate takes about 5 minutes alone and
  waited 15 to 25 minutes behind other agents' jobs. Option A: 5 gate runs. Option C: 2 gate runs, and its forced-overlap
  run was cancelled unrun. Options B and D: no gate run and no forced-overlap run; their rows say "not run", and
  their prototypes (in the scratch folder) compile and vet. The plan's gate count follows the baseline, not these.
  So "how many gate runs would each of B, C and D pass" is not established; their cons rest on reading the code and on
  the product probe, which does not depend on them.
* The independent reviewer's gate runs on `5e8821e`: pending; their result will be added by annotation (2026-11-17).
* The real server was never started; nothing here says how the product behaves under a real IDE session.

### Relation to other records

* [0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md](0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md)
  and its plan run `go test -race -count=20 -timeout 20m ./internal/handler/...` as a gate (its Confirmation, item 6);
  this record is why that gate has to be re-run on a tree that has this change.
* [0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md](0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md)
  moves the Go pins and the CI linter; the plan here runs lint under both linters. Nothing here touches `go.mod`.
* [0005-MADR-make-execute-pipeline-schema-and-handler-agree.md](0005-MADR-make-execute-pipeline-schema-and-handler-agree.md):
  its code phases come after this change; it touches `execute_pipeline*.go`, which this change does not.
* The implementation is
  [0009-PLAN-stop-store-close-racing-background-writes.md](0009-PLAN-stop-store-close-racing-background-writes.md).
