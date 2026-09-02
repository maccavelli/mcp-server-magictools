---
status: accepted
date: 2026-09-02
decision-makers: MagicTools maintainers
consulted: CI run 33687818004 (windows-2025), gopsutil v4.26.3 source
informed: MagicTools contributors and operators
---

# Bound the orphan reaper's parent-process walk with one snapshot, a visited set, and a depth cap

<!-- markdownlint-disable MD013 MD024 -->

> Paired with [0003-PLAN-bound-the-orphan-reaper-process-walk.md](0003-PLAN-bound-the-orphan-reaper-process-walk.md).

## Context and Problem Statement

`Go (windows/amd64)` fails. CI run
[33687818004](https://github.com/maccavelli/mcp-server-magictools/actions/runs/33687818004)
timed out with:

```text
panic: test timed out after 10m0s
running tests:
    TestExtraManagerMethods (9m45s)
FAIL  github.com/maccavelli/mcp-server-magictools/internal/client  600.159s
```

The failure is **reproducible**: re-running the failed job on the same commit
failed identically. It is not a flake.

Two goroutines are stuck in the same call path, one from the test and one from
the background health monitor:

```text
client.PruneOrphans          internal/client/process_reaper.go:65
  client.isLegitimateDescendant  internal/client/process_reaper.go:30
    gopsutil/v4 process.Ppid
      getFromSnapProcess         process_windows.go:894
```

`TestExtraManagerMethods` (`manager_extra_test.go:36`) calls `PruneOrphans`
directly; `TestHealthMonitor_Lifecycle` reaches it through
`StartHealthMonitor` (`health_monitor.go:52`).

### Why it hangs

`isLegitimateDescendant` (`internal/client/process_reaper.go:20-35`) walks a
process's parent chain:

```go
for {
    if currPid == myPid || currPid == myPpid || activePIDs[currPid] { return true }
    p, err := process.NewProcess(currPid)
    if err != nil { return false }
    ppid, err := p.Ppid()
    if err != nil || ppid == 0 || ppid == currPid { return false }
    currPid = ppid
}
```

Two independent defects compound.

**F1 — every step costs a full process-table enumeration.** On Windows,
`Ppid()` resolves through `getFromSnapProcess`
(`gopsutil/v4@v4.26.3/process/process_windows.go:885-900`), which takes a
`CreateToolhelp32Snapshot` and scans it linearly with `Process32Next` until it
finds the PID. gopsutil does cache the parent — but **per `Process` value**, and
this loop constructs a fresh `process.NewProcess(currPid)` on every iteration,
so the cache never hits. `PruneOrphans` (`process_reaper.go:41-65`) runs this
for every PID returned by `process.Pids()`. The result is on the order of
O(pids² × chain depth) full process-table snapshots. A CI runner has hundreds
of processes, which is tens of thousands of enumerations.

**F2 — the walk is unbounded.** There is no visited set and no depth cap. The
only cycle it detects is a self-loop (`ppid == currPid`). Windows recycles PIDs
aggressively, so a stale parent pointer can produce a genuine A → B → A cycle,
and the loop then spins forever. This matches the evidence: 9m45s elapsed,
still `running`, no progress.

On Linux and macOS the chain reliably terminates at PID 1, which masks F2 and
makes F1 merely slow rather than fatal. Windows has no equivalent guarantee.

### What is not the cause

The first failing run is the commit that raised `golang.org/x/sys` from v0.45.0
to v0.47.0 (transitively, through `mcplib` v1.4.0), and `x/sys/windows` supplies
the `Process32First`/`Process32Next` calls in this hot path. That correlation
was the initial hypothesis. It is at most a trigger: both defects predate the
bump and are visible in the source. Fixing the algorithm removes the failure
whichever library version is selected, so this record does not treat the
dependency as the problem.

### A trap for the obvious fix

gopsutil's `PpidWithContext` on Windows is declared
`func (p *Process) PpidWithContext(_ context.Context) (int32, error)` — it
**discards the context**. Threading a bounded context through the existing walk
therefore changes nothing on the platform that is failing. Any option that
relies on cancelling the current call shape is illusory.

## Decision Drivers

* The Windows job must pass deterministically, not by widening a timeout.
* A parent-chain walk must terminate on any input, including a PID cycle.
* Reaping orphans must cost work proportional to the process count, once.
* The fix must hold regardless of which `x/sys` or gopsutil version resolves.
* Reaper behaviour must stay observable and testable without a real Windows
  service manager.

## Considered Options

* Take one process snapshot, then walk an in-memory parent map with a visited
  set and a depth cap
* Add a visited set and depth cap to the existing per-step walk
* Thread a bounded `context.Context` through the walk
* Raise the Go test timeout for the `internal/client` package
* Skip the reaper tests on Windows

## Decision Outcome

Chosen option: "Take one process snapshot, then walk an in-memory parent map
with a visited set and a depth cap", because it is the only option that fixes
both defects at once: it removes the quadratic syscall cost by construction and
makes a cycle impossible to hang on, without depending on library behaviour
this repository does not control.

`PruneOrphans` builds `map[int32]int32` of pid → ppid once per invocation, from
a single enumeration. `isLegitimateDescendant` then becomes a pure function over
that map plus a visited set, with a hard depth cap as a backstop. It performs no
syscalls, so it is directly testable on every platform.

### Consequences

* Good, because the Windows job stops timing out for a structural reason rather
  than a tuning reason.
* Good, because the walk terminates on any input, including a PID cycle that no
  current test can produce on Linux or macOS.
* Good, because reaping becomes one enumeration per prune instead of tens of
  thousands, which also removes a real cost from the running health monitor,
  not only from tests.
* Good, because a pure in-memory walk is unit-testable with a fabricated
  parent map, including the cycle case, on any OS.
* Neutral, because the snapshot is a point-in-time view: a process that changes
  parent mid-prune is classified from the snapshot. The previous code had the
  same property with less consistency, since each step re-snapshotted.
* Bad, because the reaper no longer observes live changes during a single
  prune. This is accepted: a prune is a periodic sweep, and the next one sees
  the new state.

### Confirmation

* A test builds a parent map containing a cycle (A → B → A) and requires
  `isLegitimateDescendant` to return rather than hang. It must fail against the
  current implementation on any platform.
* A test asserts the walk stops at the depth cap for a pathologically deep
  chain.
* `PruneOrphans` performs exactly one process enumeration per call, asserted
  through an injected enumerator seam.
* `go test ./internal/client` passes on ubuntu-24.04, macos-15 and
  windows-2025, and the windows job completes well inside the default timeout.

## Pros and Cons of the Options

### One snapshot plus an in-memory walk with a visited set and depth cap

* Good, because it removes the cost defect and the termination defect together.
* Good, because it makes the hot path independent of gopsutil and `x/sys`
  behaviour.
* Good, because the walk becomes pure and testable on any platform.
* Bad, because it changes reaper semantics slightly, from live to point-in-time.

### Add a visited set and depth cap to the existing per-step walk

* Good, because it fixes termination with a very small diff.
* Bad, because it leaves the O(pids² × depth) snapshot cost in place. On a busy
  runner the job may still exceed the timeout, so the failure would return
  intermittently and look like a flake.

### Thread a bounded context through the walk

* Neutral, because it reads like the idiomatic Go answer.
* Bad, because `PpidWithContext` ignores the context on Windows, so it cannot
  interrupt the call that is actually blocking. It would appear to fix the
  problem while changing nothing on the failing platform.

### Raise the test timeout

* Good, because it is a one-line change.
* Bad, because it does not bound an unbounded loop: with a genuine PID cycle
  any timeout is exceeded.
* Bad, because it hides a cost defect that also affects the running health
  monitor in production, not just tests.

### Skip the reaper tests on Windows

* Bad, because the defect is Windows-specific, so this deletes the only
  coverage of the platform where it occurs and ships it.

## More Information

* CI run [33687818004](https://github.com/maccavelli/mcp-server-magictools/actions/runs/33687818004),
  and its re-run of the failed job, which reproduced the timeout on the same commit.
* `internal/client/process_reaper.go:20-35` and `:41-65`.
* `internal/client/health_monitor.go:52` and `:78`.
* `internal/client/manager_extra_test.go:36`.
* `github.com/shirou/gopsutil/v4@v4.26.3/process/process_windows.go:305-320`
  (`PpidWithContext` discarding its context) and `:885-900`
  (`getFromSnapProcess`).
* [Windows CreateToolhelp32Snapshot](https://learn.microsoft.com/en-us/windows/win32/api/tlhelp32/nf-tlhelp32-createtoolhelp32snapshot)
