---
status: implemented
date: 2026-11-27
associated-madr: "0016-MADR-stop-the-pid-file-cleanup-signalling-a-live-store-holder.md"
---

# Implement the pid-file cleanup that does not signal a live store holder

Associated MADR: [0016-MADR-stop-the-pid-file-cleanup-signalling-a-live-store-holder.md](0016-MADR-stop-the-pid-file-cleanup-signalling-a-live-store-holder.md)

## Goal

`cleanupStaleProcess` stops signalling a process it has not shown to be a stale instance of this binary, and never signals a process that holds the store lock.

## Scope

In scope, after the MADR is `accepted`:

* `internal/db/cleanup_unix.go` and `internal/db/cleanup_windows.go`
* the pid-file write in `internal/db/badger.go`
* `internal/db/cleanup_test.go`
* the S4 allowlist comment in `internal/testsafety/allowlist.go`
* one sentence in `docs/guides/testing-and-process-safety.md`

Out of scope: 0010's reaper, the service manager, a rebuild of the live service, and any run of a built binary beside that service.

## Grounding Reference

Re-ground every line at execution HEAD. The lines below were read at `f539892` on 2026-11-27.

| Anchor | Where | What it pins |
| :--- | :--- | :--- |
| F1 | `internal/db/badger.go:484` and `:488` | cleanup runs before the lock |
| F2 | `internal/db/badger.go:526-527` | pid file is a decimal pid |
| F3 | `internal/db/cleanup_unix.go:63-87` | signal 0, then TERM, then KILL |
| F4 | `internal/db/cleanup_windows.go:59-60` | `taskkill /F` with no liveness test |
| F5 | `internal/db/cleanup_unix.go:21-31` | exclusive non-blocking flock |

## Prerequisites

* The owner ruled S8 and S9 on 2026-11-27, both option 1, and the MADR is `accepted`.
* No built binary of this module is started on a machine that runs the live service.
* Tests use a temporary directory and a child the test started. They do not open the live store.

## Phase 1 — the check, tests first

1. Write the failing tests for S1, S3, S4, S5, S6 and S7 on Unix, and the Windows skip of `taskkill` for a dead pid (S11). The live-child cases use a child the test started.
2. Change `cleanupStaleProcess` to the MADR's rule. The lock test is a non-blocking probe of `LOCK.magic`, not a steal.
3. Write the new pid-file fields only after `AcquireLock` succeeds. An old decimal file still parses.
4. Update the allowlist comment and the one guide sentence. Do not add a signalling primitive.
5. Run `make test` for `./internal/db` and `./internal/testsafety`. The full module gate uses the fleet host, not a machine that is running the live service.

Exit: every new test was seen red, then green; no plant of "signal the lock holder" stays green.

## Rollback

Revert the cleanup change. The pid file's extra fields are ignored by the old reader, which parses an integer and stops. A new binary's pid file remains readable as a pid by an old binary only if the first field is still the decimal pid.

## Completion Criteria

* S1 to S7, S10 and S11 have a test each, or are the Windows row pinned by S11.
* S8 and S9 are ruled and tested as ruled.
* A second `NewStore` on a store whose lock is held does not signal the holder.
* No built binary was run beside the live service.
* The handoff says the running service is unprotected until the owner rebuilds it.

## Execution Record

S8 and S9 were ruled before any cleanup change.

Red, before the cleanup change, on this Mac: `go test -count=1 -run TestCleanupStaleProcess -timeout 30s .` in `internal/db` failed. The log line was `Stale instance detected; sending SIGTERM pid=74825`, then `cleanup signalled a live process whose executable is not this binary`.

Green, after the change, on this Mac, exit 0: `go test -count=1 -timeout 180s .` in `internal/db` (30s) and `go test -count=1 -timeout 120s .` in `internal/testsafety`. `TestCleanupLockHolderNotSignalled_0016` passed. `TestCleanupStaleProcess` passed, including the foreign-executable case that had been red. `TestCleanupUnreadableExecutableLeft_0016` passed. `make check-records` and `make markdownlint` exited 0.

Local gates, this Mac, 2026-11-27, all exit 0: `gofmt -l cmd internal` empty; `go mod tidy` left `go.mod` and `go.sum` unchanged; `CGO_ENABLED=0 go test ./...`; `make lint` (golangci-lint 2.15.0, 0 issues); `make vet`; `make check-records`; `make markdownlint` (61 files, 0 issues). Not run: Windows, and no built binary. The installed service is still the old binary.
