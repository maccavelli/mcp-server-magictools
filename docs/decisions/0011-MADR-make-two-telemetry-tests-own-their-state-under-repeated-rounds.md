---
status: accepted
date: 2026-11-19
decision-makers: repository owner
consulted: "the owner's ruling of 2026-11-19, relayed by the cross-repository coordinator; this repository at a1e6803, read only; Linux-machine runs of scratch copies of a1e6803 (the experiments under More Information)"
informed: "the cross-repository coordinator"
---

# Make TestProxyRelayTracker and TestProxyMetrics own their state, so the telemetry package passes more than one round in a process

## Context and Problem Statement

On the committed tree (`a1e6803`), `go test -count=1 ./internal/telemetry/` exits 0, while `go test -count=3 ./internal/telemetry/` and
`go test -race -count=3 ./internal/telemetry/` exit 1. Rounds two and three fail with:

```text
--- FAIL: TestProxyRelayTracker (0.00s)
    boot_spans_extra_test.go:32: expected 0 count initially
--- FAIL: TestProxyMetrics (1.00s)
    proxy_metrics_extra_test.go:28: expected save function to be called
```

The failure is not a data race (it is identical with and without `-race`) and not new: two tests keep package state from one round to the next.
The required gate of `0010-PLAN-make-tests-unable-to-signal-real-processes.md` (not yet committed at the time of writing; a link is added by a dated note when it lands),
phase P9, includes `go test -race -count=3 ./...`, so it cannot pass honestly until these two tests do.

### Decided (the owner, 2026-11-19, relayed by the coordinator; the label is quoted only as a label)

The owner chose the option labelled "Fix the two telemetry tests under their own short record now; 0010's fixes proceed meanwhile; P9's race run is
repeated on the tree with both (Recommended)". As put to the owner: a separate record pair for the two tests, test files only (each test gets its
own tracker and flusher state instead of the package's), gated and committed first.

### The two causes (read at `a1e6803`)

1. `TestProxyRelayTracker` (`internal/telemetry/boot_spans_extra_test.go:26`) takes the package-level `GlobalRelayTracker`
   (`internal/telemetry/boot_spans.go:79`), sets its `maxSize` to 5, asserts `stats["count"] == 0`, then records ten samples (four remain after
   rotation). Nothing resets the tracker, so in round two it already holds four samples and the first assertion fails. The test also leaves
   `maxSize` at 5 on the shared value.
2. `TestProxyMetrics` (`internal/telemetry/proxy_metrics_extra_test.go:8`) dispatches one event onto the package-level `proxyMetricsQueue`
   (`internal/telemetry/proxy_metrics.go:40`) and calls `StartMetricsFlusher` (`proxy_metrics.go:56`), which starts a goroutine ranging over that queue
   for the life of the process. The queue is never closed and the goroutine has no stop. Round one's flusher stays alive, holds round one's save
   function, and can take the event round two dispatched, so round two's save function is never called and the one-second wait expires.

The other tests that touch package state were read for the same pattern (More Information): they either reset what they use or are order independent.
`TestBootSpans` (same file as cause 1) shares `GlobalBootTracer` but calls `Start()` first, which clears the spans.

## Decision Drivers

* The gate `go test -race -count=3 ./...` must be able to pass without hiding a defect, because every later plan in this repository runs it.
* The change must be small and confined to test files: record 0010 has a large unreviewed build in the same tree, and its executor must not wait on a
  non-test change here.
* No assertion may be weakened, skipped or removed, and no run may be pinned to one round.
* A test must not leave a goroutine, a queued event or a changed global behind it.

## Considered Options

* A. Per-test state in the test files: the relay test builds its own `ProxyRelayTracker`; the flusher test installs its own queue, starts the flusher
  on it, and on cleanup closes it, waits for the goroutine to exit, and restores the package queue.
* B. A reset hook in non-test code: an exported or test-only function that clears `GlobalRelayTracker` and stops flushers (or a `Stop` on
  `StartMetricsFlusher`).
* C. Run the package with one round only (`-count=1`), or skip the two tests under `-count=N`.

## Decision Outcome

Chosen option: "A. Per-test state in the test files", because it removes the shared state at its source for the relay test, binds the flusher to a queue
the test closes so no goroutine outlives the test, needs no change to shipped code, and keeps every assertion.

## Consequences

* Good, because the relay test no longer reads or writes `GlobalRelayTracker`; it exercises the same type and methods on its own instance.
* Good, because the flusher test's goroutine ends with the test; a later round cannot receive an earlier round's event.
* Neutral, because the test now reaches the unexported `proxyMetricsQueue` variable. It is in the same package; if the variable is renamed the
  compile error points at the test.
* Neutral, because `StartMetricsFlusher` exposes no signal that its goroutine has exited. The test detects exit by looking for the goroutine's
  frame (`telemetry.StartMetricsFlusher.func1`) in `runtime.Stack` output, polled up to five seconds. If that function is renamed or its closure
  numbering changes, the wait ends at once (the frame is never found) and the test does not wait; it does not fail.
* Bad, because if `TestProxyMetrics` fails before the flusher goroutine has read the queue variable (its save function never signals), the cleanup's
  restore of the variable has no happens-before edge to the goroutine's read, and `-race` also reports a race on that path. That path is already a
  failing test; the passing path is synchronised through the `saved` channel.

### Confirmation

* `go test -race -count=3 ./internal/telemetry/`, `-count=10` and `-count=3 -shuffle=on` exit 0 on the Linux machine, from a scratch copy.
* Two plants on copies of the fixed tree each turn a named test red: the relay test's expected count changed by one, and the flusher never started.
* The unfixed copy is red at `-count=3` and `-race -count=3` in the same dispatch series.

## Pros and Cons of the Options

### A. Per-test state in the test files

* Good, because it is test-only and independent of record 0010.
* Good, because the relay test's assertions run unchanged against its own instance.
* Bad, because the flusher cannot be stopped through its API; the test closes the channel it injected and detects exit from the stack, which is
  indirect.

### B. A reset hook in non-test code

* Good, because exit could be signalled directly (a done channel or a `Stop`), without stack inspection.
* Bad, because it changes shipped code for the sake of two tests, and the production flusher would need a lifecycle it does not have today.
* Bad, because a reset of `GlobalRelayTracker` only makes the first assertion true; the flusher would still leak unless the stop were also added.
* Bad, because it enlarges the diff in a tree where another record's reviewed work is in flight.

### C. Run the package with one round only

* Good, because it costs nothing.
* Bad, because it hides the defect: the tests are order- and round-dependent, and the gate that needs `-count=3` would be silently weakened.
* Bad, because the tests stay wrong for any other runner (`-shuffle`, `-count`, a watch mode). Rejected.

## More Information

### Experiments (2026-11-19, scratch copies of `a1e6803` plus only the two test files; the Linux machine, telemetry package only)

Each run came after a listing that stopped the run on any process carrying `MCP_ORCHESTRATOR_OWNED=true` or named `mcp-server-magictools`
(none was found), with a throwaway home and temp directory. See the plan's execution record for the exit statuses.

* Unfixed `a1e6803`: `-count=3` and `-race -count=3` exit 1 with the two failures above.
* Fixed: `-count=1`, `-count=3`, `-race -count=3`, `-race -count=10`, `-race -count=3 -shuffle=on` exit 0 (seeds in the plan).
* Plant 1 (relay count expectation changed from 4 to 5): `TestProxyRelayTracker` fails. Plant 2 (`StartMetricsFlusher` not called): `TestProxyMetrics`
  fails at `proxy_metrics_extra_test.go:58`.

### Other tests that share package state

Read: every `*_test.go` in `internal/telemetry/` was searched for `GlobalRelayTracker`, `GlobalBootTracer`, `proxyMetricsQueue`,
`DispatchProxyMetric`, `StartMetricsFlusher` and `t.Parallel`. Only the two named tests touch the relay tracker, the queue or the flusher.
`TestBootSpans` shares `GlobalBootTracer` but resets it with `Start()`. Other package-level trackers (`GlobalDAGTracker`, `Collisions`,
`GlobalToolTracker` and so on) were not audited line by line; `-race -count=10` and three `-shuffle=on` orders passed on the fixed copy, which is
evidence, not proof, that none of them is order dependent.

### Not verified

* Order independence beyond the orders run (three shuffle seeds at `-count=3`, ten ordered rounds).
* Any other package's behaviour under `-count=3`; this record covers `internal/telemetry` only.
* Windows and macOS runs of the package; only compile checks were made there.
* That the stack-frame name used to detect flusher exit survives a refactor of `proxy_metrics.go` (see Consequences).

### Relation to other records

It unblocks `0010-PLAN-make-tests-unable-to-signal-real-processes.md` (not yet committed at the time of writing; a link is added by a dated note when it lands), phase P9, whose race
run is repeated on a tree that has both. It edits none of that record's files and does not depend on them.
