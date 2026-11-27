---
status: accepted
date: 2026-11-27
decision-makers: repository owner
consulted: "0010-MADR-make-tests-unable-to-signal-real-processes.md Q2, accepted 2026-11-18; this repository at f539892, read 2026-11-27"
informed: "agents that run this module beside the live service"
---

# Stop the pid-file cleanup signalling a live store holder

## Context and Problem Statement

[0010-MADR-make-tests-unable-to-signal-real-processes.md](0010-MADR-make-tests-unable-to-signal-real-processes.md)
Q2 ordered the pid-file identity check as a later record, not as a phase of 0010. That record was not opened.
0011 through 0015 took the numbers in between. This is that record. It does not reopen 0010.

Re-grounded at `f539892` on 2026-11-27, not at 0010's commit `3443524`:

| ID | Fact | Where |
| :--- | :--- | :--- |
| F1 | `NewStore` calls `cleanupStaleProcess` before `AcquireLock` | `internal/db/badger.go:484`, `:488` |
| F2 | The pid file is the decimal pid only | `internal/db/badger.go:526-527` |
| F3 | On Unix, a pid that answers signal 0 is sent SIGTERM, then SIGKILL after 3 seconds. No executable check. No lock check | `internal/db/cleanup_unix.go:63-87` |
| F4 | On Windows, `taskkill /F /PID` runs with no liveness test | `internal/db/cleanup_windows.go:59-60` |
| F5 | The store lock is an exclusive non-blocking flock, or `LockFileEx`, on `LOCK.magic`. A held lock makes `AcquireLock` fail | `internal/db/cleanup_unix.go:21-31`, `internal/db/cleanup_windows.go:29-35` |
| F6 | `serviceidentity.Verify` can match pid, start token and executable. Cleanup does not call it. The pid file has no start token | `internal/serviceidentity/identity.go:60-91` |
| F7 | The allowlist still says the identity check is a later record | `internal/testsafety/allowlist.go:79` |

The hazard 0010 named is a second open of the live store: `server.pid` holds the live service's pid, signal 0 succeeds, and the cleanup kills it. An executable check does not stop that. The live service is this binary. The thing that distinguishes it from a stale instance is that it holds `LOCK.magic`.

## Decision Drivers

* A second open of the live store must not kill the live service.
* A pid reused by an unrelated process must not be signalled.
* A crashed instance must still leave the store openable. A dead pid is not a holder.
* Windows must not `taskkill` a pid it has not shown to be alive.
* 0010's test seam stays. This record does not put a process scan into a test.

## Considered Options

* Never signal a process that holds the store lock. Identity-check any other live pid before a signal. Windows tests liveness first.
* Do only the check 0010's Q2 option 2 sketched: skip the caller, and signal only if the executable is this binary.
* Never signal from this cleanup. Remove a pid file only when its process is dead.

## Decision Outcome

Chosen option: "Never signal a process that holds the store lock. Identity-check any other live pid before a signal. Windows tests liveness first", because that is the rule that stops the hazard 0010 named. The executable-only check still kills the live service. Never signalling leaves a wedged, unlocked process in place, which is the case this cleanup exists for.

The owner ruled S8 and S9 on 2026-11-27, both option 1: do not signal when the executable cannot be read, and compare the executable on an old digits-only pid file.

The concrete rule, if the open rows are ruled as recommended:

* A pid that holds `LOCK.magic` for this store is not signalled. `NewStore` then fails at `AcquireLock`, as it already does once the holder is left alive.
* The caller's own pid is not signalled.
* A dead pid is not signalled. The pid file is removed.
* A live pid that does not hold the lock is signalled only when its executable is this process's executable. If the executable cannot be read, it is not signalled.
* Windows tests liveness before `taskkill`. A dead pid is not passed to `taskkill`.
* The pid file gains the start token and the executable, so a later open can use `serviceidentity.Verify`. A file that is only a decimal pid, the form F2 writes today, is still accepted and falls back to the executable comparison.

## Consequences

* Good, because a second `NewStore` on the live store no longer kills the service. The second open fails with the lock error.
* Good, because a reused pid whose executable is not this binary is not signalled.
* Bad, because a pid file written by today's binary has no start token. Until the running service is rebuilt, a reused pid of this same binary that does not hold the lock can still be signalled. The lock holder cannot.
* Bad, because the running service is not protected until it is rebuilt with this check. 0010's binary rule stays until then.
* Neutral, because 0010's seam, marker and test guard are unchanged.

## State table

| ID | Input or state | Status and outcome |
| :--- | :--- | :--- |
| S1 | Pid file absent | decided — no signal, `NewStore` continues |
| S2 | Pid file not a decimal integer | decided — no signal, file left, `NewStore` continues. Same as today on Unix |
| S3 | Recorded pid is dead | decided — no signal, pid file removed |
| S4 | Recorded pid is the caller | decided — no signal |
| S5 | Recorded pid is alive and holds `LOCK.magic` | decided, if the chosen option stands — no signal; `AcquireLock` fails |
| S6 | Recorded pid is alive, does not hold the lock, executable matches | decided, if the chosen option stands — signal, as today's stale-instance kill |
| S7 | Recorded pid is alive, executable does not match | decided, if the chosen option stands — no signal; pid file removed so the next lock attempt is not blocked by a stranger's pid |
| S8 | Executable cannot be read | decided — no signal, pid file left, `AcquireLock` decides. Owner, 2026-11-27, option 1 |
| S9 | Old pid file, digits only, no start token | decided — executable comparison only. The new file format is written after this process holds the lock. Owner, 2026-11-27, option 1 |
| S10 | Windows, pid alive | decided, if the chosen option stands — same rules as Unix, then `taskkill /F /PID` and not a tree kill |
| S11 | Windows, pid dead or not a process | decided — `taskkill` is not run |

No row is open. The record is `accepted`.

## Confirmation

* A test whose pid file names a live child that holds the test's `LOCK.magic` does not signal that child.
* A test whose pid file names a live child with a different executable does not signal it.
* A test whose pid file names a reaped child removes the file and does not signal.
* The Windows path is not run here. Its liveness skip is pinned by a test that does not call `taskkill` when the pid is dead.
* `internal/testsafety` stays green. Cleanup gains no process enumeration.

## More Information

* 0010 Q2, option 1, is this record. Option 2's sketch is the second considered option, and it does not fix F1.
* `python3 scripts/check_records.py --next` printed `0016` before this file existed.
