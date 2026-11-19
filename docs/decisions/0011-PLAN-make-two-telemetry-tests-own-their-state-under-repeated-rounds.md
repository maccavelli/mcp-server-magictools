---
status: implemented
date: 2026-11-19
associated-madr: "0011-MADR-make-two-telemetry-tests-own-their-state-under-repeated-rounds.md"
---

# Implement per-test state for TestProxyRelayTracker and TestProxyMetrics

Associated MADR:
[0011-MADR-make-two-telemetry-tests-own-their-state-under-repeated-rounds.md](0011-MADR-make-two-telemetry-tests-own-their-state-under-repeated-rounds.md)
(the owner decided on 2026-11-19; chosen option A).

## Goal

- `go test -count=3 ./internal/telemetry/` and `go test -race -count=3 ./internal/telemetry/` exit 0.
- No assertion is weakened, skipped or removed; no run is pinned to `-count=1`.
- Unblocks the `-race -count=3 ./...` gate of `0010-PLAN-make-tests-unable-to-signal-real-processes.md` (not yet committed at the time of writing; a link is added by a dated note when it lands),
  phase P9. No file of record 0010 is edited.

## Scope

### In scope

| File | Change |
| :--- | :--- |
| `internal/telemetry/boot_spans_extra_test.go` | `TestProxyRelayTracker` builds `&ProxyRelayTracker{maxSize: 5}` instead of using `GlobalRelayTracker` |
| `internal/telemetry/proxy_metrics_extra_test.go` | helpers `flusherRunning` and `useOwnMetricsQueue`; `TestProxyMetrics` installs its own queue, and cleanup closes it, waits for the flusher to exit and restores the package queue |
| `docs/decisions/0011-MADR-*.md`, `0011-PLAN-*.md` | these records |

### Out of scope

Any non-test change (a stop for `StartMetricsFlusher`, a reset of the globals); `docs/README.md` (index rows are added by the owner of that file);
`CLAUDE.md`; any other package; commits and pushes.

## Grounding Reference

The MADR's two causes: `internal/telemetry/boot_spans_extra_test.go:26` against `internal/telemetry/boot_spans.go:79`, and
`internal/telemetry/proxy_metrics_extra_test.go:8` against `internal/telemetry/proxy_metrics.go:40` and `:56`, all at `a1e6803`.

## Prerequisites

* A Linux machine with no live service, reached through the repository's host runner with race support (not WSL); every test run listed in this plan
  runs there, from a scratch copy, the telemetry package only. On a Mac that runs the live service only compile-only checks and `go vet` run.

## Implementation Steps

### Assertions, before and after

`TestProxyRelayTracker`:

| # | Assertion | Before | After |
| :--- | :--- | :--- | :--- |
| 1 | `stats["count"] != 0` fails with "expected 0 count initially" | on `GlobalRelayTracker` | unchanged, on a new tracker |
| 2 | `stats["count"].(int) != 4` fails after ten records with `maxSize` 5 | on `GlobalRelayTracker` | unchanged, on the new tracker |
| 3 | `avg == 0` fails with "expected non-zero avg" | on `GlobalRelayTracker` | unchanged, on the new tracker |

`TestProxyMetrics`:

| # | Assertion | Before | After |
| :--- | :--- | :--- | :--- |
| 1 | the save function is called within one second of the flusher starting, else `t.Fatal` "expected save function to be called" | package queue, flusher never stopped | unchanged, on the test's own queue |
| 2 | (new) the flusher goroutine has exited within five seconds of its queue being closed, else `t.Error` | none | added in cleanup |

### Phases

1. Write the two test-file changes. DONE 2026-11-19.
2. Run the baseline and the fixed copy on the Linux machine, then two plants. DONE 2026-11-19 (execution record).
3. Gates: gofmt, vet, compile-only for three targets, lint, record check, markdownlint. DONE 2026-11-19 except as noted in the execution record.
4. Stage the two records; the owner or coordinator commits after review. PENDING.

## Verification

* On a scratch copy of the committed tree plus only the two test files, on the Linux machine, the telemetry package only, after a listing that stops
  the run on any marked process: `-count=1`, `-count=3`, `-race -count=3`, `-race -count=10`, `-race -count=3 -shuffle=on`, all exit 0.
* Plants on copies of the fixed tree turn a named test red.
* `gofmt -l`, `go vet ./internal/telemetry/`, `go test -c -o /dev/null ./internal/telemetry/` for darwin, linux (cgo off) and windows;
  `golangci-lint` v2.15.0 `run -c .golangci.yml ./...`; `python3 scripts/check_records.py --check`; markdownlint on the two records.

## Rollback

Test-only, so there is no rollout. Rollback is reverting the two test files; the package then fails at `-count=3` again as before.

## Completion Criteria

* The Verification runs above are green, and the plants are red, on the Linux machine.
* The gates pass and the two test files and two records are committed by the owner or coordinator after review (pending).
* The `-race -count=3 ./...` gate of record 0010's phase P9 has been repeated on a tree holding these test changes (that record's step, not this one).

## Execution record

### 2026-11-19, baseline (unfixed `a1e6803`, Linux machine)

| Command (telemetry package only) | Exit | Package line |
| :--- | :--- | :--- |
| `go test -count=3` | 1 | `FAIL ... internal/telemetry 3.695s`; rounds two and three: `boot_spans_extra_test.go:32: expected 0 count initially`, `proxy_metrics_extra_test.go:28: expected save function to be called` |
| `go test -race -count=3` | 1 | `FAIL ... internal/telemetry 9.416s`; same two failures |

### 2026-11-19, fixed copy

| Command (telemetry package only) | Exit | Package line |
| :--- | :--- | :--- |
| `go test -count=1` | 0 | `ok ... 0.775s` |
| `go test -count=3` | 0 | `ok ... 1.639s` |
| `go test -race -count=3` | 0 | `ok ... 6.808s` |
| `go test -race -count=10` | 0 | `ok ... 15.792s` |
| `go test -race -count=3 -shuffle=on` | 0 | `ok ... 7.860s` (seed not printed without `-v`) |
| `go test -race -count=3 -shuffle=on -v`, three runs | 0, 0, 0 | `ok ... 5.595s`, `5.673s`, `5.511s`; seeds `1795107694637936892`, `1795107700852652262`, `1795107707178615113` |

Every listing before a run printed "no marked process and no magictools process on this host".

### 2026-11-19, plants (copies of the fixed tree, `-race -count=1`)

* Plant 1, expected count changed from 4 to 5: exit 1, `--- FAIL: TestProxyRelayTracker`, `boot_spans_extra_test.go:41`.
* Plant 2, `StartMetricsFlusher(save)` replaced by `_ = save`: exit 1, `--- FAIL: TestProxyMetrics`,
  `proxy_metrics_extra_test.go:58: expected save function to be called`.
* A first form of plant 2 (the save function kept but its signal removed) also failed `TestProxyMetrics`, and `-race` additionally reported the
  failure-path race named in the MADR's Consequences. That is why the plant that never starts the flusher is the one recorded.

### 2026-11-19, gates

gofmt clean on the two files; `go vet ./internal/telemetry/` exit 0; compile-only: darwin exit 0, windows exit 0, linux exit 0 with
`CGO_ENABLED=0` (a cross-compile with cgo on fails in `runtime/cgo` C sources on this Mac, unrelated to the change); golangci-lint v2.15.0
`run -c .golangci.yml ./...` on the fixed scratch copy: "0 issues", exit 0. The record check and markdownlint results are in the hand-back.

### Deviations

None.

### 2026-11-19, close-out (added; no earlier line rewritten, apart from the status line)

Phase 4 above and the second completion criterion say "pending"; they are superseded by this entry.

* The first gate run by the coordinator reported the record check exit 1: the three links to the then-uncommitted 0010 PLAN were broken in the
  committed state. They were changed to plain code text with a "not yet committed" note before the commit, as it happened.
* Commits on `main`: the records `82769dd`, the two test files `472c38b`.
* Coordinator gate, on a copy of the index plus the two test files (record 0010's uncommitted files not in the copy): `go build ./...` exit 0;
  `go vet ./internal/telemetry/` exit 0; `make lint` with golangci-lint v2.15.0 exit 0 ("0 issues"); gofmt clean; `check_records.py --check-all` exit 0 on a
  fresh copy of the index after the link fix.
* On the Linux machine through `run-on-host --need linux --race`, after the read-only listing printed "no marked process and no magictools process
  on this host" and pid 999999 was not alive: `go test -race -count=3 -shuffle=on -v ./internal/telemetry/` exit 0, shuffle seed
  `1795117978750856749`; then `go test -race -count=3 -timeout 40m ./...` on the whole committed module exit 0, every package ok, no data race. That
  last command is the one that failed before this record.
* CI on `472c38b`, run 44603457405, attempt 1, success: `Go (linux/amd64; build on tag)` 17 steps, `Go (darwin/arm64)` 10 steps,
  `Go (windows/amd64)` 10 steps; the smoke and two publish jobs skipped (no tag). The two tests therefore also pass on the hosted macOS and Windows
  runners, one round each, which the MADR's Not verified list had left open for those two systems at `-count=1`.
* Completion criteria: (1) met, Linux runs and plants as recorded above; (2) met, gates passed and the records and test files are committed;
  (3) met as a command: `go test -race -count=3 ./...` exits 0 on the committed module holding these tests. Record 0010's own phase P9 run on its
  own tree remains that record's step and is not claimed here.
* Not done: `-count=3` on macOS and Windows (CI runs one round there); order independence beyond the orders run.
