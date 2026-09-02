---
status: accepted
date: 2026-09-02
associated-madr: 0003-MADR-bound-the-orphan-reaper-process-walk.md
decision-makers: MagicTools maintainers
---

# Implement the bounded orphan-reaper process walk

Associated MADR: [0003-MADR-bound-the-orphan-reaper-process-walk.md](0003-MADR-bound-the-orphan-reaper-process-walk.md)

<!-- markdownlint-disable MD013 MD024 -->

## Goal

`go test ./internal/client` completes well inside the default timeout on
windows-2025, because the reaper takes one process snapshot per prune and its
parent walk cannot loop forever.

## Scope

**In scope.** `isLegitimateDescendant` and `PruneOrphans` in
`internal/client/process_reaper.go`; a process-enumeration seam for tests; new
unit tests for the walk; a regression test for the cycle case.

**Out of scope.** The gopsutil or `x/sys` versions; `health_monitor.go`'s
polling interval; any other `internal/client` behaviour; the Windows service
code in `cmd/`; anything in mcplib PLAN 0005 or 0006.

## Verified baseline

Record before changing anything, and stop if it does not reproduce:

    git rev-parse --short=12 HEAD
    git status --short
    go build ./... && go vet ./internal/client

| Fact | Evidence |
|---|---|
| The walk has no visited set and no depth cap | `internal/client/process_reaper.go:20-35` |
| Only a self-loop terminates a cycle | `process_reaper.go:31` (`ppid == currPid`) |
| A fresh `Process` is built each step, defeating gopsutil's per-value ppid cache | `process_reaper.go:26` |
| `PruneOrphans` runs the walk for every PID | `process_reaper.go:41-65` |
| Windows `Ppid` takes a full snapshot and scans it | `gopsutil/v4@v4.26.3/process/process_windows.go:885-900` |
| `PpidWithContext` discards its context on Windows | same file, `:305-320` |
| The health monitor calls `PruneOrphans` on a timer | `internal/client/health_monitor.go:52` |
| Reproducible, not a flake | run 33687818004 and its re-run of the failed job |

## Implementation Steps

1. **Add an enumeration seam.** Introduce an unexported package variable that
   returns the full pid → ppid map, defaulting to a single `process.Processes()`
   enumeration. Tests replace it; production keeps one call per prune. Do not
   export it.

2. **Make the walk pure.** Change `isLegitimateDescendant` to take the parent
   map instead of querying the OS:

       func isLegitimateDescendant(pid int32, parents map[int32]int32,
           activePIDs map[int32]bool, myPid, myPpid int32) bool

   It performs no syscalls, so it is testable on every platform.

3. **Terminate on every input.** Inside the walk keep a `map[int32]bool` of
   visited PIDs and return false on revisit. Add a constant depth cap
   (`maxParentWalkDepth`, 64) as a backstop and return false when it is
   exceeded. Retain the existing true conditions (`myPid`, `myPpid`,
   `activePIDs`) and the existing false conditions (unknown PID, ppid 0, ppid
   equal to the current PID).

4. **Build the map once.** In `PruneOrphans`, call the seam once before the PID
   loop and pass the result into every walk. Do not construct `process.Process`
   values inside the walk any more.

5. **Leave classification semantics alone.** The set of PIDs considered, the
   `EnvManaged` environment check, `killPIDGroup`, and the logging must not
   change. This plan changes how the ancestry question is answered, not which
   processes are reaped.

6. **Document the semantic change** in a comment on `PruneOrphans`: the prune
   classifies against a point-in-time snapshot, and a process that reparents
   mid-prune is picked up by the next sweep.

## Required tests

Add to `internal/client/process_reaper_test.go`:

* **Cycle terminates.** A parent map containing A → B → A. The walk must
  return, not hang. Run it under a goroutine with a short timeout so a
  regression fails fast instead of hanging the suite.
* **Depth cap.** A chain longer than `maxParentWalkDepth` returns false.
* **Self-loop.** `ppid == currPid` still returns false.
* **Positive cases.** A chain reaching `myPid`, one reaching `myPpid`, and one
  reaching a PID in `activePIDs` each return true.
* **Unknown parent.** A PID absent from the map returns false.
* **One enumeration per prune.** With the seam replaced by a counter,
  `PruneOrphans` increments it exactly once regardless of PID count.

## Verification

Run, and record the output:

    gofmt -l internal/client
    golint internal/client/process_reaper.go internal/client/process_reaper_test.go
    go vet ./internal/client
    go test ./internal/client
    go test -race ./internal/client
    make lint
    make test

**Prove the instrument works.** The cycle test must fail against the current
implementation before the fix lands. Demonstrate it without dirtying the tree:

    go test -overlay=<overlay.json> -run TestParentWalk ./internal/client

where the overlay maps `process_reaper.go` to a scratch copy carrying the old
unbounded walk. Record that it hangs or fails, and that it passes against the
new implementation. A cycle test that has only ever been seen passing proves
nothing.

## Acceptance criteria

1. `go test ./internal/client` passes on ubuntu-24.04, macos-15 and
   windows-2025, and the Windows job finishes well inside the default timeout.
2. The cycle test fails against the pre-fix implementation and passes after.
3. `PruneOrphans` performs exactly one process enumeration per call.
4. `isLegitimateDescendant` performs no syscalls.
5. No gopsutil or `x/sys` version changed.
6. Which processes get reaped is unchanged; only the ancestry lookup changed.
7. `make lint` reports 0 issues and per-file `golint` is clean.

## Rollout and Rollback

**Rollout.** One commit in this repository. No release, tag or dependency
change. Do not push without explicit authorization in the same turn.

**Rollback.** Revert the single commit; the reaper returns to its previous
behaviour, including the defect. Do not roll back by widening the test timeout
or skipping the Windows job — that ships the hang.

**Interaction with mcplib PLAN 0005 Gate G2.** This repository must not be
tagged for a canonical release while the Windows job is failing: immutable
releases are already enabled here, and the publish job is gated behind the test
job, so a tag would create a release that cannot be completed or fixed in
place. Land this first, confirm CI is green on all three runners, then tag.

## Execution Record

Populate during execution.

| Step | Status | Commit | Evidence | Deviation |
|---|---|---|---|---|
| 1-6 implementation | complete | (this commit) | `parentMap` seam added defaulting to `osParentMap`, which enumerates once via `process.Processes()`; `isLegitimateDescendant` is now pure over a supplied `map[int32]int32` with a visited set and `maxParentWalkDepth` = 64; `PruneOrphans` calls the seam once before the PID loop. Which processes get reaped is unchanged | the `pids, err := process.Pids()` block appears twice in the file — the second in `lookupSystemProcesses`, which is out of scope. The uniqueness guard on the edit caught the ambiguity and the anchor was narrowed to `PruneOrphans`'s own signature; `lookupSystemProcesses` is untouched |
| Required tests | complete | (this commit) | 11 new tests: two cycle cases, self-loop, depth cap, three positive cases, unknown/zero/nil parent, and the one-enumeration-per-prune counter. All run under a 5s watchdog so an unbounded regression fails fast instead of hanging the suite | **I overwrote `process_reaper_test.go` instead of appending, destroying `TestLookupInternalRegistry`, `TestReportSubServerFailure` and `TestEnforceSingleInstanceSkipsSelf`. `make test` stayed green because deleted tests do not fail — coverage silently dropped. Restored from HEAD and verified byte-identical by md5 per function; the file diff is now insertions-only** |
| Negative test of the cycle assertion | complete | (this commit) | An overlay replaced `process_reaper.go` with a copy carrying the pre-fix unbounded walk (same signature, no visited set, no cap). Against it, `TestParentWalkTerminatesOnCycle` and `...OnLongerCycle` fail with `isLegitimateDescendant did not terminate: the parent walk is unbounded`; against the fix all pass. The tree was never modified | the first `TestParentWalkDepthCap` passed against **both** implementations, so it discriminated nothing: its chain simply ran out. Rewritten so the chain reaches `myPid` only *beyond* the cap, making a capped walk answer false and an uncapped one true. It now fails against the unbounded walk — 3 failing assertions, not 2 |
| Verification suite | complete | (this commit) | gofmt; per-file golint; `go vet`; `go test ./internal/client` (84 subtests); `go test -race`; `GOOS=windows build`; `make lint` 0 issues; `make test` | none |
| Windows CI green | pending | | the criterion this change exists to satisfy; awaiting push | |
