---
status: implemented
date: 2026-11-18
associated-madr: "0010-MADR-make-tests-unable-to-signal-real-processes.md"
---

# Implement the process-table seam and the per-instance marker: no test enumerates or signals a real process

> **ABSOLUTE RULE FOR EVERY EXECUTOR OF THIS PLAN.** Until phase P9 has passed on a host the owner has confirmed has no
> live service, you run no test of `internal/client`, no whole-module test (`go test ./...`, `make test`, `make race`,
> any `-race` or fuzz run of the whole module), and no binary of this module (`make run`, `serve`, a built
> `mcp-server-magictools`), on any machine that has the live service. You never start, stop, signal or call the
> running service or any process you did not start yourself. Allowed on such a machine: reading, `grep`, `gofmt`,
> `go build`, `go vet`, `go list`, lint, `go test -c -o /dev/null <one package>` (compiles a test binary and runs
> nothing), `go test ./internal/testsafety/` (parses source only; red on purpose until P5), the tests of
> `internal/handler`, `internal/db` and `cmd/mcp-server-magictools` as before, per the owner's ruling, and, from P6a,
> `go test -run '^TestUDPServerStopIsIdempotent$' ./internal/telemetry/` (named test only). If you are unsure whether a
> command runs a test of `internal/client`, it does: do not run it.
>
> *Added by the third ruling (2026-11-18):* the Linux machine is confirmed to have no live service. Tests of
> `internal/client` run **only** there, from a scratch copy (`git archive HEAD` plus this plan's files), through
> `run-on-host --need linux --race`, behind a read-only listing that stops the run if any process carries
> `MCP_ORCHESTRATOR_OWNED=true` or is named `mcp-server-magictools`, and, until the guard's rule C list for that package
> is empty, with `-run` naming only the fake-based tests. The tests of `internal/proctable` (the real table) are first
> run in P9, after P8. The same applies to `internal/proctable` on every machine as to `internal/client` here.

Associated MADR:
[0010-MADR-make-tests-unable-to-signal-real-processes.md](0010-MADR-make-tests-unable-to-signal-real-processes.md)
(the decision to fix is accepted; the owner's answers to its four open questions are recorded below and in the MADR's
second ruling).

## Decided (2026-11-18)

The owner's answers, relayed by the coordinator; each label is quoted only as a label, and what was asked is put in my
own words beside it. The same section is in the MADR.

1. **Q1, which design:** "That, plus a per-instance marker so two orchestrators cannot reap each other's children". The
   registry-held interface with a constructor that refuses to run in a test binary, fakes and the parse-only guard
   (A+G), plus the per-instance marker (C). It changes which processes are reaped in production and protects nothing
   until the owner's running server is rebuilt with the new marker. **Not the option the MADR recommended** (A+G only).
2. **Q2, the pid-file cleanup (S4):** "A record of its own, straight after this one (Recommended)". Not in this plan.
3. **Q3, where the first run and the owed whole-module gates happen:** "On the Linux machine, after you confirm no live
   magictools server runs there (Recommended)". **The owner's confirmation about that host has not been given.** The
   coordinator asks for it before P9; nothing runs there until then. *Note, 2026-11-18:* the owner gave it the same day
   ("No live magictools server there: use it (Recommended)", relayed by the coordinator; the third ruling, item 6).
4. **Q4, the side findings:** "Fix the UDP double close here too". Phase P6a. The dead kill helper and `make run` are for
   later records.

**The coordinator's reading,** recorded as the coordinator's and told to the owner: these answers approve the plan to
proceed phase by phase, each committed by the coordinator after its own gate; P3's design still stops for the
coordinator's approval before any code; the independent review (P8) precedes any first execution; P9 waits for the
owner's confirmation about the host.

**Third ruling, 2026-11-18** (the owner's answers to the design phase's five questions and to the Linux host; labels in
the MADR's "Decided, third ruling"): P3 Q1 keep `=true` for the sub-servers and add a new owner stamp, R3 amended; P3
Q2 the store directory is the instance; P3 Q3 never touch old-stamped processes, with a one-time read-only check in the
guide; P3 Q4 S2's look-alike rules removed; P3 Q5 the child-only OS test may run on the macOS machine, that package
only, after the Linux run (P9) and the review (P8); **the Linux machine has no live magictools server and may be used**,
behind the read-only marker listing before every run. The coordinator approved the P3 design (the MADR's "Design as
approved") as the basis for the build.

**Placement note, 2026-11-18.** The index holds another record's staged change (0009's). This pair therefore stays
untracked and uncommitted until that change is committed; the coordinator commits this pair right after. P1 and P2 are
working-tree edits, unstaged. P2 (the guard) is red by design and is **committed together with P4 and P5, never
alone, and never pushed alone** (see "What CI does with the red guard", under P2).

## Goal

* No test of this module can enumerate the machine's processes or signal a pid it did not start, with no flag, tag or
  environment variable, on any machine.
* The reaper still reaps its own predecessor's orphans in the shipped binary; it no longer reaps another live
  instance's sub-servers (R1 to R6 of the MADR), and an old running service and a new binary leave each other's
  children alone.
* The reaper's logic keeps its tests, over a fake process table.
* `UDPServer.Stop` is safe to call twice: a clean shutdown no longer panics.
* The fix is built and checked in an order that never runs the three tests against a real process table on a machine
  that has the live service, and its first run is on a host the owner has confirmed.

## Scope

### In scope

| File | Change |
| :--- | :--- |
| `internal/client/process_reaper.go` | the scan and the foreign-pid signalling go through the interface; `parentMap` is removed; the selection applies the marker rules (MADR D4) |
| `internal/client/reaper_harvest_unix.go`, `reaper_harvest_windows.go` | *(P3)* deleted: `harvestProcess` becomes generic code over the table |
| `internal/client/process_table.go`, `owner_stamp.go`, `escalate.go` (new) | *(P3)* the interface and binding, the owner stamp's pure functions, the TERM-wait-KILL policy over an injected clock |
| ~~`internal/client/process_os.go` and build-tagged twins (new)~~ `internal/proctable/` (new package) | *(P3, replaces the struck row)* the OS implementation behind the interface, moved from the code above (S1's `killPIDGroup` for foreign pids, `osParentMap`, the harvest signals); its unexported type, its constructor `NewOSProcessTable`, its twins, and its one child-only test; `killProcessGroup` (S3, own child) stays direct in `internal/client` |
| `internal/client/process_unix.go`, `process_linux.go`, `process_windows.go`, `connector.go`, `server_entity.go` | *(P3)* S3 split from `killPIDGroup` and gated on the child's own exit (MADR D3) |
| `internal/client/lifecycle.go` | the spawn path's stamp (`internal/client/lifecycle.go:721`) names the owner instance; the constant `EnvManagedValue` stops being the selection (it stays as the sub-server protocol value, MADR F1); the Wait goroutine closes the child's `exited` channel |
| `internal/client/manager.go` | the registry's table binding and clock; `NewWarmRegistry` unchanged in signature, leaves the table nil |
| `cmd/mcp-server-magictools/main.go` | one call in `ServeFunc`, after `NewOrchestratorApp` and before `PreDrive`, that gives the registry the real table; `PreDrive` refuses to start without one; the S5 call removed |
| `cmd/mcp-server-magictools/infra_unix.go`, `infra_windows.go` | ~~S5's sweep applies the same marker rules (R6); no seam, no test~~ *(P3, replaces the struck text)* `InitialProcessSweep` removed: R6 is met by deletion (MADR D6) |
| `internal/telemetry/udp_server.go`, `internal/telemetry/udp_server_stop_test.go` (new) | `Stop` made idempotent; the test |
| `internal/client/fake_process_table_test.go` (new) | the fake table |
| `internal/client/manager_extra_test.go`, `process_reaper_test.go`, `health_monitor_test.go` | the three tests, and the S2 test, move to the fake |
| ~~`internal/client/process_os_test.go` (new)~~ `internal/proctable/table_test.go`, `childonly_test.go` (new) | one test that scans and signals only a child it started, selected by a marker random per test *(P3: moved into the new package)* |
| `internal/client/*_test.go` (new files) | the marker cases R1 to R5 over the fake, and the stamp as a pure function |
| `internal/testsafety/` (new package) | the parse-only guard, its allowlist and its tests: written (P2) |
| `internal/db/cleanup_test.go`, `internal/db/check_badger_test.go` | the dead pid from a reaped child; the debug harness behind a build tag |
| `CLAUDE.md` (`AGENTS.md` is its symlink, checked) | the interim rule, added in P1 and removed in P10 |
| `docs/guides/testing-and-process-safety.md` (new), `docs/README.md` | the guide and its index rows (P1); the two record rows (the coordinator adds them) |

### Out of scope

* The S4 pid-file identity check: **the next record, not this one** (Q2). The dead `killStaleProcess` and `make run`:
  later records (Q4). Moving S5's scan onto the seam (S5 takes the marker rules only). Windows beyond compiling the twins
  (`GOOS=windows go vet`).
* Every file the plans in flight hold: `internal/db/badger.go`, `cache.go`, `historical.go`, `io_helpers.go`,
  `metrics_flush_helpers.go`, `docs/guides/services-and-transports.md` and the 0009 records (0009); `internal/handler/*`
  and the 0008 records (0008, 0005); `go.mod`, `go.sum`, `Makefile`, `.github/workflows/ci.yml`, `README.md`,
  `docs/guides/platform-installation.md` (0006). None is edited here. `README.md` gets its one pointer line only after
  the plan that holds it has landed.

## Grounding Reference

* The MADR's evidence index (E1 to E77), inventory (S1 to S8), rules R1 to R6 and the production table under C. Every
  `path:line` in this plan is at `3443524`.
* `internal/client/process_reaper.go:89-149` (`PruneOrphans`), `:153-275` (`enforceSingleInstance` and
  `lookupSystemProcesses`), `:27` (`parentMap`), `internal/client/lifecycle.go:482` (the spawn path's call into S2) and
  `internal/client/lifecycle.go:721` (the stamp), `internal/client/health_monitor.go:52` and `:78` (the monitor's immediate first tick).
* `internal/telemetry/udp_server.go:114` (`Stop`), `cmd/mcp-server-magictools/main.go:351`, `:1049`, `:369`, `:1004`
  (the callers and the paths that reach them).
* `internal/config/main_test.go:26`: the existing guard that fails a test binary that touched the operator's real state;
  the same discipline, applied before the touch.
* `CLAUDE.md`: records, pre-add checks, commits. Global rules: heavy runs through `run-heavy` or `run-on-host`.

## Prerequisites

1. The owner's answers are in (above). This plan is written for them: Q1 option 2, Q2 option 1, Q3 option 1, Q4 option 2.
2. `git rev-parse --short HEAD` is `3443524` or a descendant; `git status --short` shows no change to any file in the
   in-scope table other than this plan's own.
3. The index holds another plan's staged change (0009's). Agents stage this plan's paths by name and never unstage,
   restore or reset anything. The coordinator commits this pair after 0009's change is committed, and commits the
   code phases after its own gate.
4. Heavy runs go through `~/.agents/skills/available-dev-hosts/scripts/run-heavy -- <command>` on the session machine,
   or `run-on-host` where the plan says so, one at a time per host.
5. For P9: the owner's statement, in the owner's own words and dated, that no magictools service or process runs on the
   Linux host(s) the dispatcher may pick. **Not yet given.** An agent's belief is not that statement. *Given
   2026-11-18* (the third ruling, item 6: the Linux machine, with the read-only marker listing before every run;
   relayed by the coordinator; label in the MADR). `run-on-host --need linux --race` keeps the run off the WSL
   distributions, so it lands on that machine; every run prints `uname -a`.

## Conventions

* Models and effort are priced from the bottom: the cheapest class that can do the phase; where the strongest tier is
  warranted the phase says why.
* A new gate, assertion or test is unused until it has been seen to fail on a deliberately broken input, run on a
  temporary copy (never by dirtying the tree), and never by a real process: the failure is shown with a fake table, with
  source text, or by a constructor panic.
* Every edit lands through the edit tool or a script that asserts the old text occurs exactly once, and is read back.
* Nothing committed names a host, an account, a real pid or a machine path.
* **Commits.** Agents stage this plan's paths by name and do not commit. The coordinator commits after its own gate,
  with `git commit --no-edit`, after the checks of that phase pass.

## Implementation Steps

### P0, place the records (cheapest class, low effort; the coordinator)

Add the two index rows to `docs/README.md` (the MADR `accepted`, this plan `in-progress`), run
`python3 scripts/check_records.py --check-all` and `make markdownlint`, stage the two records and the index by path,
**after** 0009's staged change is committed. Docs step: the index is the docs step.

### P1, the interim rule and the guide (cheapest class, low effort) DONE 2026-11-18, working tree, unstaged

Why the cheapest class: two documents and no code.

1. `CLAUDE.md` (`AGENTS.md` is a symlink to it, checked): the section "Interim rule: tests and commands that can kill the
   live service", which P10 removes.
2. `docs/guides/testing-and-process-safety.md` (new): what the reaper, the enforcer and the pid-file cleanup do, the
   eight sites, which commands are safe on a machine with the live service, the guard and why it is red, what this record
   changes.
3. `docs/README.md`: the guide's two index entries (the guides line and an "I want to" row).
4. `make check-records` and `make markdownlint`: see the Execution Record.

### P2, the guard, red first (mainstream class, high effort) DONE 2026-11-18, working tree, untracked, RED BY DESIGN

Why mainstream and high: the guard states an invariant about source text exactly, and a cheaper class tends to write a
grep that passes for the wrong reason. It touches no `internal/client` code.

1. `internal/testsafety/` (`guard.go`, `allowlist.go`, `guard_test.go`): imports the standard library only
   (`go list -deps -test` shows nothing from the module but itself); parses every `.go` file with `go/parser`, build
   constraints ignored, so the Windows twins are read; runs nothing.
2. Rules, as in the MADR:
   * **B, the allowlist.** Every use of `syscall.Kill`, `unix.Kill`, `os.FindProcess`, a method `.Signal(x)` or
     `.Kill()`, gopsutil's `Pids`, `Processes` and `NewProcess` (by import path, whatever the alias), another process's
     environment (`GetProcessEnviron`, `.Environ()`, `unix.SysctlRaw`), a test's direct call of `cleanupStaleProcess`, or a
     command word (`kill`, `pkill`, `killall`, `taskkill`, `launchctl`, `systemctl`) as a call argument or slice element,
     must be on `allowlist.go`, which is the MADR's inventory by file and primitive. An entry whose use has gone fails
     as stale. **Green today** against the eight known sites and their read-only neighbours.
   * **C, tests use a fake.** A function in a `_test.go` that calls `PruneOrphans`, `StartHealthMonitor`,
     `enforceSingleInstance`, `lookupSystemProcesses`, `harvestProcess` or `InitialProcessSweep` must mention an
     identifier beginning `fakeProcessTable` or `newFakeProcessTable`. **Red today**, naming the three tests and a fourth
     (below).
   * **A and D, the constructor.** No test names `NewOSProcessTable`; non-test code calls it at exactly one line, in
     `cmd/mcp-server-magictools/main.go`. They cannot be enforced until P4 builds the constructor, so today each is a
     **skipped subtest whose reason is exactly `not yet enforceable: constructor absent`**. A skip is not a pass. They
     switch on by themselves when a `NewOSProcessTable` function exists in non-test code; the const
     `constructorRequired` (false today) is set to `true` by P4, after which an absent constructor fails. **P4 turns them
     on; P7 plants a violation of each to prove they are live; P7 is not complete without it.** The name is a constant in
     `guard.go`; P3 may change it, in that one place.
3. **Seen failing first, on the real tree, by running only the guard** (Execution Record has the verbatim lines): rule C
   fails on `internal/client/health_monitor_test.go:34` `TestHealthMonitor_Lifecycle` (`StartHealthMonitor`),
   `internal/client/manager_extra_test.go:36` `TestExtraManagerMethods` (`PruneOrphans`),
   `internal/client/process_reaper_test.go:189` `TestPruneOrphansEnumeratesExactlyOnce` (`PruneOrphans`), and also
   `internal/client/process_reaper_test.go:54` `TestEnforceSingleInstanceSkipsSelf` (`enforceSingleInstance`, S2, which
   also scans every process). That red is the seen-failing of the hazard itself, obtained without running any of them.
4. **Planted inputs.** Permanent fixture tests in `guard_test.go` (a temporary tree each): `syscall.Kill` in a new file, a
   test with `process.Pids()`, `process.Pids()` under an import alias, `exec.Command("kill", ...)`, `launchctl` in a slice
   literal, a method `.Signal`, `os.FindProcess`, `GetProcessEnviron`; an allowlisted use passes; a stale entry fails; an
   unparseable file fails; rule C with and without a fake; rules A and D with the constructor present, absent, absent but
   required, called once, zero times, twice, elsewhere, and named by a test. Three of the plants were also run on a
   scratch copy of `3443524` plus the guard (a new file with `syscall.Kill`, a test with `process.Pids()`, an
   `exec.Command("kill", ...)`): each added exactly one rule B finding naming the planted file.
5. **What CI does with the red guard if this were pushed before P5.** `.github/workflows/ci.yml` (0006's file, read and
   not edited) runs on every branch push and pull request (line 4 onward). The `go` job runs `CGO_ENABLED=0 go test ./...`
   on Linux (`.github/workflows/ci.yml:84`) and the `go-native` job runs `go test ./...` on macOS and Windows
   (`.github/workflows/ci.yml:199`); neither passes `-skip` or `-run`, and the file has no `continue-on-error` and no
   `|| true`. `go test ./...` exits 1 while `internal/testsafety` is red, which fails the step and the job on all three
   platforms, and the `release` job needs both (`.github/workflows/ci.yml:254`), so a tag would not publish either. `make
   test` (`Makefile:60`) is `go test -v ./...` and fails the same way. Nothing hides the red, and that is intended.
   **The coordinator will not push P2 by itself: it is committed with P4 and P5.**
6. Docs step: the guide's "The guard, and why `go test ./internal/testsafety/` is red" is written (P1's guide).

### P3, design the seam and the marker (strongest tier, high effort) DONE 2026-11-18, approved by the coordinator

The design is the MADR's "Design as approved" (D1 to D8), with the owner's third ruling on its five questions. It
contradicted the MADR in two facts (F2, F3) and in R3's first bullet (F1); the MADR is amended (step 4 below).

Why the strongest tier: the surface of the interface decides whether the fix is structural or a gesture, and the marker
decides which processes a shipped binary kills. A wrong cut lets S3 route through the foreign-pid path, lets a fake diverge
from the OS, or kills a live instance's sub-servers. This is the judgment-heavy step, and it stops for the coordinator.

Deliver, in the Execution Record and for the coordinator's approval before P4:

1. **The seam**: the interface (method list and why each exists); the proof by reading that `PruneOrphans` and
   `enforceSingleInstance` keep their bodies and change only where their data comes from; why no method signals a pid
   the same table did not return; the exact meaning of the inert zero value on each entry point; the constructor and its
   first-statement `testing.Testing()` panic; what happens to `killProcessGroup` (S3) and to the escalation goroutine of
   `killPIDGroup`; the guard's constant, if the constructor's name changes.
2. **The marker**, meeting the MADR's R1 to R6, and a table of the cases each rule covers, with the answer for every cell
   of the MADR's production table: how an instance is named (a store identity, an owner pid with its start time, or
   another way) and how an owner is known to be gone; the stamp's exact form; how the new stamp avoids the old reaper's
   exact match and the old sweep's `MAGIC_TOOLS_PEER_ID=` prefix match (and whether the sub-servers use that variable
   for something else); what the new code does with a process carrying the old constant; what S5 does.
3. **What changes in production**, in the plan's own words: which processes a new binary reaps and which it no longer
   reaps; what happens while an old running service stamping the old constant coexists with a new binary (neither may
   reap the other's children); the one start-up's worth of orphans from the old binary that stays until removed by hand,
   if the design leaves them; the rollback (reinstall the previous binary).
4. If the design contradicts the MADR, amend the MADR and stop for re-approval. Docs step: none until the design stands.

### P4, build the seam and the marker (mainstream class for the seam, strongest tier for the marker; high effort)

*As drafted before P3; the steps "P4a, as approved" and "P4b, as approved" below supersede these two paragraphs where
they differ (S5 is removed, not rewritten; the OS implementation is a new package).*

P4a, the seam (mainstream, high): the design is fixed by P3; careful, mechanical work with a compiler to check it. Add
the interface, the OS implementation (the existing code moved, not rewritten), the registry field, the inert zero value,
the constructor with its panic, and the one wiring line in `main.go`. Remove `parentMap`. Set `constructorRequired` to
`true` in `internal/testsafety/guard.go`, and update `allowlist.go` for the files that move.

P4b, the marker (strongest, high): why the strongest tier: a wrong selection rule is a wrong kill. Implement P3's stamp
and selection rules in S1 and S2's lookups and in S5, with the production table's cells as code and as the fake-table
cases of P5.

Checks that run no process (every sub-step): `gofmt -l`, per-file `golint` on every changed `.go` file, `go build ./...`,
`go vet ./...`, `GOOS=linux`, `GOOS=darwin` and `GOOS=windows` `go vet` of `./internal/client/`,
`./cmd/mcp-server-magictools/` and `./internal/db/`, `go test -c -o /dev/null ./internal/client/` (compile only; one
package per call), the same for `./cmd/mcp-server-magictools/`, and `go test ./internal/testsafety/`, where rules A and D
now run and pass, and rule C still fails for the tests P5 has not yet moved. Docs step: the guide's seam and marker
paragraphs; `docs/architecture.md` if it then describes the registry.

#### P4a, as approved: steps 1 to 5 of the MADR's D8 (mainstream class, high effort)

Every step ends with: `gofmt -l` and per-file lint of the changed files; `go build ./...` natively and with
`GOOS=linux` and `GOOS=windows`; `go vet` of the touched packages (natively and for the other two GOOS where the package
has twins); `go test -c -o /dev/null` of `./internal/client/` and `./internal/proctable/`, one package per call;
`go test ./internal/testsafety/` with its red list recorded; `go list -deps -test ./internal/client/ ./internal/handler/`
listing no `internal/proctable`; and the step's new fake-based tests on the Linux machine only (see the absolute rule),
with the exact `-run` pattern recorded. Tests are written before the code they test, within each step.

1. **The interface and the fake.** `internal/client/process_table.go` (`Sig`, `ProcInfo`, `SelfInfo`, `ProcessTable`,
   `Clock`), `owner_stamp.go` (`Owner`, `Stamp`, `ParseOwner`, `LineageFor`, `CanonicalStoreDir`: pure),
   `escalate.go` (`Escalate`), `fake_process_table_test.go` (`fakeProcessTable`, `newFakeProcessTable`, `fakeClock`),
   and tests of the stamp's parsing (R4 cases), of the stamp against the old tests (E2, E44), and of the escalation
   without sleeping. Guard: red list unchanged (4).
2. **`internal/proctable`.** The unexported `osProcessTable`; `NewOSProcessTable` panicking first under
   `testing.Testing()`; `validateTarget`; `Snapshot` (gopsutil `Processes` and `Ppid`, moved from `osParentMap`, plus the
   token); the Linux token from field 22 of `/proc/<pid>/stat` and the namespace link, the other-platform token from
   `CreateTime`; signals off Windows (`syscall.Kill`, moved from `killPIDGroup`) and on Windows (`taskkill`). The child-only
   test file and the OS test **written and compiled, not run** (first run P9). Guard: rules A2, A3, E and B's two
   primitives, each with a fixture that is red on a plant and green on the tree; allowlist entries for the new files.
   Expected red list: rule C (4) and rule D "found 0" (the constructor exists, the wiring does not yet).
3. **The wiring.** `AttachProcessTable`, `ProcessTableAttached`, the binding field; in `ServeFunc` one call
   `app.reg.AttachProcessTable(proctable.NewOSProcessTable(), dbPath)` after `NewOrchestratorApp` and before `PreDrive`;
   `PreDrive` returns an error and starts nothing when no table is attached; `constructorRequired = true`. Red list:
   rule C (4).
4. **The data sources.** `PruneOrphans`, `enforceSingleInstance`, `lookupSystemProcesses` and `harvestProcess` read the
   binding, the snapshot and the table, **with the old selection rule unchanged**, so this diff changes only where each
   datum comes from; `parentMap`, `osParentMap`, `reaper_harvest_unix.go` and `reaper_harvest_windows.go` deleted (their
   allowlist entries removed); `TestPruneOrphansEnumeratesExactlyOnce` rewritten on the fake in the same step (deleting
   `parentMap` breaks its compilation). Red list: rule C (3); the other three tests are now inert by construction (no
   table) and still named by the syntactic rule.
5. **S3** (MADR D3): `killProcessGroup` split from `killPIDGroup`, through the child's handle and gated on its `exited`
   channel; `SubServer` carries the channel; `spawnProcess` returns it; `connector.go` passes it on.

#### P4b, as approved: steps 6 to 8 of the MADR's D8 (strongest tier, high effort)

1. **Step 6, the stamp** (MADR D4): a pure `stampEnvironment(base, owner, name)` used by `prepareProcessEnvironment`; keeps
   `MCP_ORCHESTRATOR_OWNED=true` and `MCP_ORCHESTRATOR_SERVER_NAME`, adds `MAGICTOOLS_OWNER` when the registry has an
   owner, stops stamping `MAGIC_TOOLS_PEER_ID`, strips all four from the inherited environment.
2. **Step 7, the selection** (MADR D4): `reapable` and `harvestable` as pure functions over data, used by `PruneOrphans`,
   `lookupSystemProcesses` and the pid-file candidate; the parent leaves the descent walk; S2's look-alike rules removed
   (third ruling, P3 Q4); old-stamped processes never touched (P3 Q3); the fake-table cases of every row of D4's table
   and of D5 **in the same step**, so the new rule never lands untested.
3. **Step 8, S5 removed** (MADR D6): `InitialProcessSweep` and its call; its allowlist entries removed.

Docs step (P4a and P4b): the guide's seam and marker paragraphs, and the production table.

### P5, move the tests onto the fake and add the marker cases (mainstream class, high effort)

Why mainstream and high: the fake must carry the reaper's cases, and a test written against a fake that cannot express
them would pass for the wrong reason.

1. `fake_process_table_test.go`: a table of pids, parents, environments, executables and command lines, a count of
   enumerations and the list of pids a terminate or harvest was asked about.
2. Rewrite `TestExtraManagerMethods`, `TestPruneOrphansEnumeratesExactlyOnce` (the exactly-once cost guard keeps its
   meaning: one enumeration per prune, counted on the fake), `TestHealthMonitor_Lifecycle` (the monitor runs over a fake and
   the test waits for its goroutine to exit) and `TestEnforceSingleInstanceSkipsSelf`.
3. New fake-table cases the real machine cannot offer: a marked process of another live instance is not selected (R1); a
   predecessor's orphan on the same store is (R2); an old-constant process is left alone unless the design shows its
   owner gone, and an old-constant process beside a new binary is not touched (R3); a missing, malformed or foreign stamp
   is not touched (R4); a descendant of self or of a managed pid is not; a pid cycle ends; a table of ten thousand entries
   is one enumeration; S2 by name stamp, by executable, by suffix, by interpreter plus command line; a registry with no
   table touches nothing and calls nothing; the spawn path's stamp as a pure function (no process started).
4. `process_os_test.go`: starts one child itself with a marker value random per test (never a production value), reads its
   environment through the OS implementation, terminates it through the implementation, and asserts that the
   implementation refuses a pid that is not that child's. **Compiled here, not run on a machine with the live service;
   its first run is P9.**
5. Static checks as in P4; `go test ./internal/testsafety/` is now **green**: rule C passes. This is where the red of P2
   ends.
6. Docs step: the guide's "Writing a test that touches the reaper" paragraph; the guide's red-guard paragraph is changed
   to say the guard is green.

#### P5, as approved: step 9 of the MADR's D8 (supersedes items 1 to 4 above where they differ)

The fake (item 1) and the exactly-once rewrite (part of item 2) came forward into P4a steps 1 and 4; the marker cases
(item 3) land with the rule in P4b step 7, without the look-alike cases (removed by P3 Q4) except as cases that pin their
absence; the OS test (item 4) is `internal/proctable/table_test.go`, written in P4a step 2. What remains here:

1. Rewrite `TestExtraManagerMethods`, `TestHealthMonitor_Lifecycle` (over a fake; it cancels, waits for the monitor
   goroutine and for the reaper's escalations, and runs under a throwaway `HOME` and config directory: MADR F8) and
   `TestEnforceSingleInstanceSkipsSelf`.
2. `go test ./internal/testsafety/` is **green**: rule C's list reaches zero in this step and no earlier.
3. From this step on the client package's tests may run on the Linux machine without a `-run` filter.

### P6, the two test edges in `internal/db` (cheapest class, medium effort)

Why the cheapest class: two small edits with a clear spec; medium effort because the second is easy to get wrong.

1. `TestCleanupStaleProcess` obtains its dead pid by starting a child, waiting for it to exit and using its pid, instead of
   999999. It keeps the case that starts and signals its own `sleep` child.
2. `TestDebugLiveBadger` gets a build tag (`debuglive`) so the default build does not contain a harness that can be
   pointed at a live directory. State in the file why.
3. These are `internal/db` tests: the owner's ruling lets them run on the session machine as before. Run only
   `go test ./internal/db/ -run 'TestCleanupStaleProcess'` there. Docs step: the guide lists the tag.
4. *Collision check, 2026-11-18.* Record 0009's staged change (`git diff --cached --name-only`) touches
   `internal/db/badger.go`, `cache.go`, `historical.go`, `io_helpers.go`, `metrics_flush_helpers.go` and six new
   `store_close*` test files, and not `internal/db/cleanup_test.go` or `internal/db/check_badger_test.go`. **There is no
   collision**: P6 need not wait for 0009's commit. Agents stage by name and never touch 0009's files.

### P6a, the UDP double close (cheapest class, medium effort; Q4)

Why the cheapest class: one guard and one test. Medium effort because the first step is a proof by reading that gates a
test run.

1. **Sites, re-derived at `3443524`.** `UDPServer.Stop` closes `cancelChan` with no guard (`internal/telemetry/udp_server.go:114`).
   Two callers reach it in one shutdown: the application's `Stop` (`cmd/mcp-server-magictools/main.go:351`, inside its
   `stopOnce`) and the graceful-shutdown path (`:1049`). `Start` defers the application's `Stop` (`:369`), and the signal
   handler runs the graceful shutdown (`:1004`), so after a signal the graceful path closes the channel and the deferred
   `Stop` closes it again. The live service's log shows `panic: close of closed channel` in `UDPServer.Stop` from the
   application's `Stop` on two dates. The line numbers in that stack trace are the installed binary's.
2. **Why the test is safe to run, established by reading before the phase may run it.** The test builds the struct in the
   package's own test (`&UDPServer{cancelChan: make(chan struct{})}`, no `conn`), so it binds no socket (the existing
   `TestUDPServerLifecycle`, `internal/telemetry/udp_server_test.go:8`, does bind a loopback one; this test does not),
   starts no goroutine, signals nothing and scans nothing. `Stop` with a nil `conn` only closes the channel. The package
   `internal/telemetry` has one gopsutil use, which reads the caller's own pid (`os_metrics.go`, on the guard's allowlist),
   and no `exec` or signal call. Even so, the run names the test: `go test -run '^TestUDPServerStopIsIdempotent$'
   ./internal/telemetry/` (and the same with `-race`), because naming it keeps every other test of the package, including
   the one that binds a socket, out of the run. That is the whole reason, and it needs no other test to be unsafe.
3. **Seen failing first.** Write `TestUDPServerStopIsIdempotent` (a second `Stop` must not panic; the test recovers a panic
   and reports it with `t.Fatalf` naming "close of closed channel"; a nil receiver is safe; two concurrent `Stop` calls under
   `-race` are safe) and run it on the unfixed code: it fails with that panic. Record the line verbatim.
4. **The fix.** `Stop` becomes idempotent at the type (a `sync.Once` in `UDPServer`, so no caller needs to know the order
   of the others). Re-run the test: green; and with the once removed on a scratch copy it is red again.
5. Docs step: read the operations guide for any description of shutdown and correct it if it claims a clean exit; the
   testing guide already names the fix.

### P7, the checks that need no real process, and the planted breaks (strongest tier, high effort)

Why the strongest tier: this phase is where "seen failing" is honoured without killing anything, and a plant that
accidentally reaches a real process defeats its purpose.

1. Re-run the guard's red and green sequence on the final tree: green on the tree; each plant of P2.4 red.
2. **The constructor rules are live.** The skipped subtests of rules A and D no longer skip (P4 built the constructor and
   set `constructorRequired`). On a scratch copy, one at a time, each must turn the guard red: a `_test.go` that names
   `NewOSProcessTable` (rule A); a second call of it in non-test code (rule D); the wiring line removed from `main.go`
   (rule D); `NewWarmRegistry` assigning the real table (rule D); the constructor deleted with `constructorRequired`
   true (rule D). **P7 is not complete until each is seen to fail.**
3. Planted breaks of the design, over the fake table, on a scratch copy: the selection reverted to the old constant
   (the R1 case fails); the old-constant case reaping (R3 fails). They run in the fake and touch no process.
4. Verify by reading that the constructor's first statement is the `testing.Testing()` panic, before any gopsutil call.
   The panic itself is exercised in P9 on the confirmed host. *(P3: guard rule E now checks this; reading confirms it.)*
4a. *(P3)* Further plants, each on a scratch copy and each red: rule A2 (a handler test importing `internal/proctable`;
   a `package client_test` file importing it); rule A3 (an `osProcessTable{}` literal in a `proctable` test outside
   `newChildOnlyTable`; a non-test reference to `newChildOnlyTable`); rule E (the panic moved after a gopsutil call; the
   `if` removed); rule B (a `//go:linkname` in a test; a `/proc/` literal in a new file). Design plants over the fake,
   each turning a P4b step 7 case red: the selection reverted to the old constant; old-stamped processes reaped; the
   lineage check removed; the owner-liveness check removed (case b); the token check removed (case e and a mid-grace
   reuse); the parent restored in the descent walk (case d); a look-alike rule of S2 restored; `validateTarget`'s
   `PID <= 1` refusal removed; the inherited-stamp strip removed.
5. Read the diff against the MADR's Confirmation item 1: the bodies of `PruneOrphans`, `enforceSingleInstance`,
   `lookupSystemProcesses` and `harvestProcess` differ only in where their data comes from and in the selection the
   marker rules change, each of which is a P3 table cell.
6. Docs step: none.

### P8, independent review before anything is first executed (strongest tier, high effort; a fresh agent that did not write the change)

The reviewer is given the diff, the MADR's inventory and rules R1 to R6, and not this plan's conclusions, and is asked:
can any `_test.go` in any package reach the OS table; is the zero value inert on every entry point (`PruneOrphans`,
`enforceSingleInstance`, `lookupSystemProcesses`, `harvestProcess`, the health monitor); does anything route S3 through
the foreign-pid path; does the new selection ever select a live instance's sub-server (R1), an old-constant child of an
old service (R3) or an unstamped process (R4); does the old reaper's exact match, or the old sweep's prefix match,
select a new-stamp child (R3); does the guard's allowlist classify every site of the grep set in the MADR; is the
OS-implementation test able to signal any pid but its own child; is `Stop` safe under `-race`. The reviewer runs no
test. Findings are fixed or put to the owner before P9.

### P9, the first execution, on the Linux host after the owner's confirmation (cheapest class that can read an exit code, low effort; any agent)

**Blocked until the owner's confirmation (Prerequisite 5) is in the Execution Record.** The coordinator asks for it; no
agent assumes it.

1. Dispatch every command below with `run-on-host --need linux -- <command>` (never `pick-host`'s unpinned choice, which
   can name the session machine); `run-heavy` queues them one at a time. Before the test command, in the same dispatch,
   run the read-only listing `grep -la MCP_ORCHESTRATOR_OWNED /proc/[0-9]*/environ` and `pgrep -fl magictools` and read
   them: any output stops the phase and goes to the owner.
2. **Decoys.** On that host the executor starts harmless processes it owns, each a long `sleep` that is not a descendant
   of any test: one with the old stamp `MCP_ORCHESTRATOR_OWNED=true`, one with a stamp of another instance in the new
   form, one with `MCP_ORCHESTRATOR_SERVER_NAME=test-server`. The executor ends them at the end, by its own pids.
3. In this order, each exit status read before the next: `go test -count=1 -run 'TestPruneOrphans|TestHealthMonitor_Lifecycle|TestExtraManagerMethods|TestEnforceSingleInstanceSkipsSelf' -v ./internal/client/`;
   the constructor panic, shown by a temporary test in the dispatch's scratch copy that calls the constructor and must
   fail with the constructor's message; `go test -count=1 ./internal/client/...`; `go test -count=1 ./...`;
   `go test -race -count=3 -timeout 40m ./...`. After each, all decoys are still alive (`kill -0` of their own pids).
4. The same commands on the unchanged `3443524` are **not** run: that would demonstrate the hazard on a real process table.
   The old selection's reach is shown by P5.3 on data.
2a. *(P3)* **Five decoys, not three:** (1) `MCP_ORCHESTRATOR_OWNED=true` only; (2) in its realistic form,
   `MCP_ORCHESTRATOR_OWNED=true` plus `MAGICTOOLS_OWNER=v1:<random lineage>:<a pid the executor started and reaped>:1`
   (an old-rule reaper would kill it; the new rule must not); (3) `MCP_ORCHESTRATOR_SERVER_NAME=test-server`; (4) a copy
   of `sleep` named `mcp-server-test-server` in the dispatch's scratch directory (the executable-suffix decoy); (5)
   `MAGIC_TOOLS_PEER_ID=<random>` (the old S5 prefix). Before the whole-module runs, run
   `go test -count=1 -v ./internal/proctable/` (the child-only OS test, first execution). The scratch copy that holds
   the temporary constructor-panic test is not the copy the whole-module runs use: rule A fails there by design.
2b. *(Third ruling, 2026-11-18)* The owner's confirmation of the Linux machine is recorded in Prerequisite 5; the
   read-only listing still runs before every command there and any marked process stops the run.
5. If any decoy dies, stop: the fix does not hold; nothing is committed; the owner is told.
6. After P9 passes, the whole-module gates owed by 0009, 0008 and the Go-pins change (0006) are released, on the same
   host, by the coordinator.
7. Docs step: the Execution Record carries the outputs verbatim.

### P10, finish the docs and lift the interim rule (cheapest class, low effort)

After P9 and the owner's say-so only: remove the interim section from `CLAUDE.md`; the guide states that the test hazard
is removed, what the guard enforces, what the marker changes in production and that it protects only once the service is
rebuilt, and that the product's own S4 hazard remains until its record; add the one-line pointer to `README.md`'s "Local
verification" once the plan that holds it has landed; `make check-records`, `make markdownlint`.

*(P3 and the third ruling)* The guide also carries: the **one-time read-only check** the owner runs after reinstalling
the rebuilt service, which lists the owner's own processes whose environment holds `MCP_ORCHESTRATOR_OWNED=true` and no
`MAGICTOOLS_OWNER=` entry (Linux: `/proc/<pid>/environ`; macOS: `ps eww`), and says that such leftovers are never
reaped by the new binary and are ended by hand (P3 Q3); and that the child-only OS test
(`go test -run '^TestOSTable' ./internal/proctable/`, that package only) may run on the macOS machine **after P9 has
passed and P8's review is closed** (P3 Q5), and nothing else of `internal/client` or the module runs there.
*Note, 2026-11-18 (P8, S4):* the check the guide carries is a script per system, not a bare `ps eww`: Linux reads
`/proc/<pid>/environ`, macOS reads `ps -E -ww -o pid= -o ppid= -o command= -u <uid>`; each starts a marked `sleep`
as a positive control and refuses an all-clear when the control is not listed, skips its own processes by pid, and
prints pids and executable paths only, never an environment value.

### P11, stage and hand to the coordinator (cheapest class, low effort)

`gofmt -l`, per-file `golint` on every staged `.go` file, `go vet ./...`. Stage this plan's paths by name. The coordinator
commits with `git commit --no-edit` after its own gate: **P2, P4 and P5 together** (P2 is red alone), P6 and P6a each as
their gate passes. This plan lands before the whole-module gates of 0009 and 0008 and before the Go-pins change (0006).

*Note, 2026-11-19:* this repository has no pre-add wrapper (no `pre-add-check` target, no `scripts/go-precheck*`).
Nothing in it runs `golint`: its own lint is `make lint`, golangci-lint with `.golangci.yml`, which CI runs at the fleet
standard v2.15.0. P11's per-file `golint` is the global pre-add rule for a repository without a wrapper, so both were run.

## Verification

* P2: the guard is red on the unchanged tree naming the three tests (and a fourth), and its fixtures and scratch-copy
  plants show each rule catches what it names. P7 repeats it on the final tree and proves the constructor rules live.
* P5: the fake-table tests pass on the confirmed host; the marker cases R1 to R4 pass; the guard is green.
* P6a: the new test failed on the unfixed code with `close of closed channel` and passes after, including under `-race`.
* P9: all decoys survive every run listed, including `go test ./...` and the race run; the constructor panics under test
  with its message; no `internal/client` test or whole-module test was run anywhere else before it.
* On the session machine the only commands that ran were the compile-only and parse-only ones of the absolute rule, the
  `internal/handler`, `internal/db` and `cmd` tests the owner's ruling allows, and the named telemetry test.
* `make check-records` and `make markdownlint` pass.

## Rollout

No release, no tag, no restart of any running server. The product changes are the seam's wiring line, the marker and the
UDP stop. The marker protects nothing until the owner rebuilds and reinstalls the service, which is the owner's act, not
this plan's; until then the running service stamps the old constant and a new binary beside it must leave those children
alone. The coordinator commits after the gates of each phase; the owner's confirmation decides when P9 runs.

## Rollback

Revert the commit(s). The reaper returns to its present behaviour, including the three tests that can signal the live
service, the constant marker and the double-close panic, and the interim rule (P1) must then be restored by hand at the same
time. A rebuilt service that misbehaves is rolled back by reinstalling the previous binary. Do not roll back by skipping
the three tests, loosening a guard rule, or adding an environment variable that disables the reaper: that ships the
hazard with the detection silenced. If P9 shows a decoy died, do not retry on another host to "see if it was the host":
stop.

## Completion Criteria

1. Every phase above is done, or named as skipped by an owner's ruling.
2. `go test ./internal/testsafety/` was seen red on the unchanged tree and on each planted input, and is green; the
   constructor rules (A and D) were seen to fail on their plants in P7, and no skipped subtest remains.
3. The three tests and the S2 test use a fake table; no `_test.go` constructs the real table; the registry's zero value
   is inert on every entry point; the marker rules R1 to R4 pass over the fake.
4. `TestUDPServerStopIsIdempotent` was seen to fail on the unfixed code and passes, with `-race`.
5. P8's findings are closed.
6. P9 passed on the confirmed host with every decoy alive after every run and the constructor panic seen; the owner's
   confirmation is in the Execution Record.
7. The docs step of every phase is done; `CLAUDE.md` has no interim rule; `make check-records` and `make markdownlint`
   pass.
8. The records are staged for the coordinator; this plan's status becomes `complete` only after the commit of the
   finished change, and the MADR's open questions are marked answered with the owner's labels (done 2026-11-18).

## Execution Record

### 2026-11-18, owner's answers; P1 and P2 written

* The owner answered Q1 to Q4 (above). The coordinator's reading is recorded above. The owner's confirmation about the
  Linux host has not been given; P9 has not started and nothing has run there.
* The pair is uncommitted and untracked, awaiting the commit of 0009's staged change.
* P1 written in the working tree, unstaged: `CLAUDE.md` (`AGENTS.md` is its symlink, checked), the new guide, `docs/README.md`.
* P2 written, untracked, unstaged: `internal/testsafety/`. Only the guard's own test was run (`go test ./internal/testsafety/`),
  after `go list -deps -test` showed it imports the standard library and nothing else from the module, and with no
  `TestMain` or `init`. Result of the real-tree run: rule B green; rule C red; rules A and D skipped with their reason.
  Failing lines, verbatim:

```text
rule C: internal/client/health_monitor_test.go:34: TestHealthMonitor_Lifecycle calls StartHealthMonitor without building a fake process table in the same function: it would scan or signal the real machine
rule C: internal/client/manager_extra_test.go:36: TestExtraManagerMethods calls PruneOrphans without building a fake process table in the same function: it would scan or signal the real machine
rule C: internal/client/process_reaper_test.go:54: TestEnforceSingleInstanceSkipsSelf calls enforceSingleInstance without building a fake process table in the same function: it would scan or signal the real machine
rule C: internal/client/process_reaper_test.go:189: TestPruneOrphansEnumeratesExactlyOnce calls PruneOrphans without building a fake process table in the same function: it would scan or signal the real machine
not yet enforceable: constructor absent
not yet enforceable: constructor absent
```

* Plants on a scratch copy of `3443524` plus the guard: a new file with `syscall.Kill`, a test with `process.Pids()` and an
  `exec.Command("kill", ...)` each added one rule B finding naming the planted file (see P2.4). Lint: golangci-lint 2.13.1 on
  the guard package only, in a scratch copy under Go 1.26.6 (the binary cannot load the Go 1.27.4 standard library), 0 issues.
* Not done, and why: no P3 to P11 work (P3 stops for the coordinator; P9 waits for the owner); no test of `internal/client`,
  no whole-module test, no binary built or run, nothing signalled; nothing staged.

### 2026-11-18, P3 approved; the third ruling; P4a steps 1 to 4 built (working tree, unstaged)

* P3's design approved by the coordinator; the owner's third ruling recorded (MADR and above). The MADR is amended: F1 to
  F8, R3's first bullet, "Design as approved" (D1 to D8), the production table, Consequences, Confirmation, Not verified.
* **Where things ran.** On the session machine (the live service's): only `gofmt`, golint per file, `go build ./...`
  (native, and `GOOS=linux` and `GOOS=windows` with `CGO_ENABLED=0`), `go vet` of `./internal/client/`,
  `./internal/proctable/`, `./cmd/mcp-server-magictools/`, `./internal/testsafety/` (native, linux, windows),
  `go test -c -o /dev/null` of `./internal/client/` and `./internal/proctable/`, `go list -deps -test`, and
  `go test ./internal/testsafety/`, also on scratch copies for the plants. On the Linux machine only, from a scratch copy
  (`git archive HEAD` plus this plan's files), through `run-on-host --need linux --race`: the marker listing first
  ("no marked process and no magictools process on this host" each time), then `go test -race -count=1 -v -run` with an
  anchored alternation of the fake-based tests only. No test of `internal/proctable` ran anywhere.
* **Step 1** (interface, owner stamp, `Escalate`, the fake): 11 tests passed on Linux.
* **Step 2** (`internal/proctable`; guard rules A2, A3, E and B's two primitives): the OS tests compiled, not run. Guard
  red list: rule C (4) and rule D "found 0", as expected.
* **Step 3** (`AttachProcessTable`, the `ServeFunc` call, `PreDrive`'s refusal, `constructorRequired = true`): 13 tests
  passed on Linux. Red list: rule C (4).
* **Step 4** (the data sources on the table, old rule kept; `parentMap`, `osParentMap`, `reaper_harvest_unix.go`,
  `reaper_harvest_windows.go` removed; `TestPruneOrphansEnumeratesExactlyOnce` on the fake): 19 tests passed on Linux,
  including the rewritten exactly-once test (one snapshot of 10 000 rows). Red list: rule C (3):
  `TestHealthMonitor_Lifecycle`, `TestExtraManagerMethods`, `TestEnforceSingleInstanceSkipsSelf`, each now inert by
  construction (no table) and still named by the syntactic rule until step 9.
* **Seen failing.** On the Linux machine, nine mutations of the client code, one at a time on the scratch copy, each made
  the fake-based tests fail (escalation without the wait; upper-case lineage; two owner values; a second enumeration;
  descendants not spared; the enforcer's nil check removed; exclusions ignored; a second attach; an attach without a
  token). On scratch copies of the tree, nine guard plants each turned the guard red with the rule they plant (A, A2, A3
  twice, E, B twice, D twice). The new rules' fixture tests in `internal/testsafety` are red on every plant and green on
  the sanctioned forms.
* **Found while building, not anticipated by the design:** (1) the real table's type held no state, so a nil pointer of
  it would have been a working table; it now has an `armed` field set only by the two constructors, every method refuses
  without it, and rule A3 forbids a test to name the type outside `newChildOnlyTable`; (2) the guard parsed without
  comments, so a `//go:linkname` could not be seen; it now parses comments; (3) the existing constructor fixture had no
  panic and failed rule E; the fixture now has one; (4) on the session machine cross-builds need `CGO_ENABLED=0`
  (the environment enables cgo); (5) `golint` reports `CallProxy` without a comment in `internal/client/manager.go`, as
  at `3443524`.

### 2026-11-18, P4a step 5, P4b steps 6 to 8, P5 step 9 (working tree, unstaged)

* **Coordinator's rulings on hand-back 1:** a test enters the `-run` pattern only once the guard no longer flags it;
  the engineering details of hand-back 1 accepted. The two step-4 changes of production behaviour (the pid-file
  candidate kept only when it is in the snapshot; Windows harvest checks `Alive` first) are now rows of the MADR's D5,
  with the KILL re-identification, and the MADR states that step 4's diff is not purely a data-source switch.
  `CallProxy` and the `lifecycle.go` const block got doc comments (comments only), so the files this change stages
  pass golint.
* **Where things ran:** as before. On the session machine, only the checks that run no test and the guard (also on
  scratch copies, for the plants). On the Linux machine only, from scratch copies, through
  `run-on-host --need linux --race`, after the listing ("no marked process and no magictools process on this host"
  each time). No test of `internal/proctable` ran anywhere.
* **Step 5 (S3):** `internal/client/own_child.go` (`stopOwnChild`, `isClosed`), `own_child_unix.go`
  (`killProcessGroup` through the handle, the group only after it, escalation on the exit channel),
  `process_windows.go` (taskkill of the tree while not reaped, then `Process.Kill`), `process_unix.go` and
  `process_linux.go` (only `prepareCommand` left), the exit channel registered by `spawnProcess` and closed then removed
  by its Wait goroutine (`childExits`, MADR D3 "as built"). Tests: four own-child tests (`own_child_unix_test.go`)
  that end only the test's own `sh`/`sleep` child; the one the old code fails is
  `TestSeamOwnChildAlreadyReapedIsNotSignalled`. Allowlist: `own-child` entries for the new files. Linux: 23 tests
  passed.
* **Step 6 (the stamp):** `stampEnvironment` and `ownerStamp` (`owner_stamp.go`), used by
  `prepareProcessEnvironment`; `MAGIC_TOOLS_PEER_ID` no longer stamped. Tests: `stamp_environment_test.go` (three).
* **Step 7 (the selection):** `internal/client/reap_rules.go` (`reapPrefilter`, `reapable`, `ownerDead`, `detached`,
  `harvestable`), used by `PruneOrphans`, `lookupSystemProcesses` and the pid-file candidates; the look-alike rules
  removed. Tests: one per row of D4's table (a) to (j), with (a′), the rulings, and the hazard as data
  (`reap_rules_test.go`, `reap_hazard_test.go`); the step-4 tests of the old rule replaced by flow tests
  (`process_reaper_seam_test.go`). Linux, steps 6 and 7 together: 45 tests passed.
* **Step 8 (S5):** `InitialProcessSweep` removed from `infra_unix.go` and `infra_windows.go`, with its call and comments
  in `main.go`; nothing else called it (grep); allowlist entries removed. MADR D5 says what of its Linux and Windows
  behaviour is covered by `reapable` and what is deliberately dropped.
* **Step 9:** `TestHealthMonitor_Lifecycle` (a throwaway `HOME`, waits for the monitor and the escalations),
  `TestExtraManagerMethods` and `TestEnforceSingleInstanceSkipsSelf` on the fake. **The guard is green**: rule C has
  no finding; rules A, A2, A3, D and E run and pass (none skipped).
* **Final Linux run** (one dispatch, a scratch copy of HEAD at dispatch plus this plan's files; HEAD was `3443524`
  through step 5 and `bfcd269`, with 0009 committed by the coordinator, from steps 6 and 7 on; 0009 touches no file of
  this plan): the fake-based tests by name, 48 passed; **the whole `internal/client` package, 149 passed, 0 failed, 0
  skipped**; **the whole module except `internal/proctable`, under `-race`, 21 packages `ok`**, after a read-only check
  that pid 999999 was not alive (`TestCleanupStaleProcess` signals it if it is: the S4 edge P6 fixes; this machine's
  pids pass 999999). This module run holds 0009 but not the working-tree changes of 0008 and 0006, so it is not their
  gate. **The hazard as data on P4a step 4's code** (a copy of `3443524` plus the files of steps 1 to 4 and
  `reap_hazard_test.go`): both tests failed there, the reaper and the enforcer each sending TERM to the live service's
  sub-server (`[{501 TERM}]`); on the final code both pass.
* **The plants (P7, design part), on the Linux machine:** 32 mutations, one at a time on the scratch copy. The first
  run left two survivors. "S1: descendants not excluded" survived because every fake table's own row had an unknown
  parent, so `detached` happened to protect this process's children. In production the chain is complete (this process
  has pid 1 as its parent under launchd), so the new test `TestSeamOwnLiveChildrenUnderAServiceManagerKept` was added.
  On the re-run it catches that plant, and the client package has 150 passed. **One survivor remains, verbatim:**
  `SURVIVED S1: this process not excluded: the tests passed on the broken copy`. The check is redundant by construction:
  the descent walk's first step tests the pid itself against this process. It is kept as defence in depth, and the fake
  still fails a test that asks to signal this process. Every other plant failed a named test with a test failure, not a
  build failure. The guard plants on the final tree (A, A2, A3 twice, E, B twice, D twice) are each red, with a green
  baseline.
* **Found while building, not anticipated by the design:** (1) a test of S3 that ends a real child must be the test's
  own child and live in `internal/client`; such tests run only on the Linux machine, like the rest of the package;
  (2) rule C is syntactic, so test helpers that call the entry points take the fake as an argument and check that the
  registry holds it (`holdsFake`); (3) `TestCleanupStaleProcess`'s fixed pid is live-able on the Linux machine (pids
  above 999999): every whole-module run there needs the read-only pid check until P6 lands; (4) the self check of
  `reapPrefilter` is redundant with the descent walk, which starts at the pid itself (see the plants).

### 2026-11-18, P6 and P6a (working tree, unstaged)

* **Where things ran.** On the session machine (the live service's), through a throwaway `HOME` and `XDG_*` and the real Go
  caches: `gofmt`, per-file `golint`, `go build ./...` (native, `GOOS=linux` and `GOOS=windows` with `CGO_ENABLED=0`),
  `go vet` of `./internal/db/` and `./internal/telemetry/` (native, linux, windows, and `-tags debuglive` for the db
  package), `go test ./internal/testsafety/`, lint, the named telemetry test, `go test -run '^TestCleanupStaleProcess$'
  ./internal/db/` once, and `go test -race -count=5 -timeout 20m ./internal/db/...` through `run-heavy`. No test of
  `internal/client`, `internal/proctable` or the whole module, and no binary of the module, ran.
* **P6.1, `TestCleanupStaleProcess` (`internal/db/cleanup_test.go`).** The dead pid now comes from `deadPID(t)`: it runs
  `true` with `exec.Command(...).Run()` (start and wait, so the child is reaped and its pid is free) and returns that pid;
  it skips, with its reason, when `true` is missing or cannot run. It is taken on the line before the cleanup call, so the
  gap is two adjacent statements. **Windows keeps the fixed number** (`windowsNeverAPID`): `cleanup_windows.go` runs
  `taskkill /F` on the recorded pid with no liveness test, so a reaped child's pid, if Windows reused it in the gap, would end
  a stranger, whereas 999999 is not a multiple of four, which Windows pids are (an implementation detail, not a documented
  guarantee: **not verified here**). This is a judgement the brief did not spell out; reverting it is one `if`.
  **Residual risk, stated in the comment:** the kernel could hand the freed pid to a new process between the reap and the
  cleanup's own liveness check. A signal-0 probe from the test was considered and not added: it would sit in the same gap
  and the cleanup repeats it, and it would need new allowlist entries for `os.FindProcess` and `.Signal` in the test file,
  which the brief reserves for the guard's author.
* **Old assertions, and where they live now** (all in `TestCleanupStaleProcess`, same order): (1) no pid file gives no error;
  (2) a stale pid gives no error, now on the reaped child's pid; (3) a live pid (its own `sleep 10` child) gives no error
  within one second, or the test kills the child and moves on. Nothing was dropped or loosened.
* **Seen failing first, without signalling anything.** The old test's number, probed with a standalone program in the
  scratch folder that only calls `os.FindProcess` and signal 0: `999999` is "not alive" on the macOS machine, which is why
  the test never failed here; on the Linux machine the plan's own record says pids pass 999999 (its pid limit is in the
  millions; the MADR's S4 row gives 4194304 as a possible limit), where the same cleanup would send SIGTERM and then
  SIGKILL to a stranger. The same program shows that a child started with `Run` answers "not alive" to signal 0 right after
  it returns, five times in a row. The old test was not run in a form that could signal.
* **P6.2, `TestDebugLiveBadger` (`internal/db/check_badger_test.go`).** `//go:build debuglive` (the tag is named for the
  file's purpose, as the plan said), body unchanged except the skip message, and a header that says what it does and that it
  must never be pointed at a live service's directory (`NewStore` runs the pid-file cleanup first). Without the tag the
  file is not part of the package's test files (`go list -f '{{.TestGoFiles}}'` counts it 0 times, 1 time with the tag),
  and the compiled test binary holds no `TestDebugLiveBadger` symbol (`go tool nm`: 0 without the tag, 4 with it). Both
  compile (`go test -c -o /dev/null`), and `go vet` and lint are clean with and without the tag.
* **The guard:** `go test ./internal/testsafety/` stays fully green after both edits (exit 0, nothing skipped); rule B's
  existing entries for `internal/db/cleanup_test.go` (`primTestCleanupStale`, `primKill`) still match, and the new
  `exec.Command("true")` is not a command word.
* **P6a, the safety reading** (done before any run; lines at the working tree): `internal/telemetry/udp_server_test.go:8`
  `TestUDPServerLifecycle` binds a loopback socket, and the run's `-run '^TestUDPServerStopIsIdempotent$'` excludes it;
  the package's only process-library use is `internal/telemetry/os_metrics.go:36` `process.NewProcess(pid)` with the pid at
  `:27` `os.Getpid()`; a search of the package for `os/exec`, `syscall`, `Signal` and `.Kill(` finds nothing; there is no
  `TestMain` and no `func init` in the package; its package-level initialisers (`StartTime`, `Collisions`, `RecentErrors`,
  `GlobalTracker`, `proxyMetricsQueue`) only allocate; `go list -deps -test` names no module package but the package
  itself. The test builds `&UDPServer{cancelChan: make(chan struct{})}` with no connection and starts no goroutine; `Stop`
  with a nil connection only closes the channel.
* **P6a, red.** `go test -race -count=20 -run '^TestUDPServerStopIsIdempotent$' ./internal/telemetry/` on the unfixed code:
  exit 1, `udp_server_stop_test.go:45: second Stop panicked: close of closed channel` and 31 of the 32 concurrent callers
  with `caller N panicked: close of closed channel` (the one that closed first did not panic).
* **P6a, the fix.** `internal/telemetry/udp_server.go`: a `stopOnce sync.Once` field; `Stop` keeps its nil-receiver check
  and runs the channel close and the connection close inside `stopOnce.Do`. A `sync.Once`, not an atomic flag or a mutex
  around a bool: `Once.Do` blocks a concurrent caller until the first returns, so every caller returns only after the
  shutdown has finished, which an atomic compare-and-swap would not give the second caller. **Green:** the same command,
  exit 0, `ok`, twice. The test has three subtests (a second and third call after the first, a nil server, 32 concurrent
  callers each checking the channel is closed on return).
* **P6a, the plant.** On a scratch copy of `go.mod`, `go.sum`, `internal/forks` and `internal/telemetry` only, the guard
  removed from `Stop` (the `Do` and its closure replaced by the bare close and connection close, asserted to occur once):
  the same command exits 1 with the same messages as the red above. Nothing in the tree was dirtied.
* **What else `Stop` ran twice:** closing the connection. A second `UDPConn.Close` returns an error that `closeConnOrWarn`
  (`internal/telemetry/io_helpers.go:29`) logs as the warning `telemetry: failed to close connection`; it is now closed once,
  so that warning no longer appears at the second call. The broadcaster and the ping receiver read `cancelChan` and the
  connection only; nothing else closes either. `Stop` is synchronous for both callers after the change. The callers
  (`cmd/mcp-server-magictools/main.go:358` in the application's `Stop`, and `:1056` in the graceful-shutdown path, at the
  working tree; `main.go` not edited) need no change.
* **Found while doing it:** `golint` reported a pre-existing finding in `internal/telemetry/udp_server.go:17`, the comment
  on `DefaultTelemetryPorts` said `TelemetryPorts` (the file at the base commit fails the same way; the repository refuses a
  staged Go file that fails `golint`). The comment now starts with the variable's name; nothing else in that block changed.
* **Gates**, each exit status read before any filter: `gofmt -l` on the four files exit 0, no output; `golint
  -set_exit_status` on each of the four files exit 0; `go build ./...` native, `GOOS=linux`, `GOOS=windows` exit 0; `go vet
  ./internal/db/ ./internal/telemetry/` native, linux, windows, and `-tags debuglive ./internal/db/` exit 0;
  `go test ./internal/testsafety/` exit 0; lint with golangci-lint 2.13.1 under Go 1.26.6 on a scratch copy of the base
  commit plus the four files: 0 issues for `./internal/db/ ./internal/telemetry/`, and 0 issues for `./internal/db/` with
  `--build-tags debuglive`; `go test -count=1 -run '^TestCleanupStaleProcess$' ./internal/db/` exit 0 (the live case
  logged its SIGTERM to its own child only); `go test -race -count=5 -timeout 20m ./internal/db/...` through `run-heavy` (queued behind other runs, then 117 s):
  exit 0, `ok ... internal/db 114.568s`, one line of output, no race report, no failure; `python3
  scripts/check_records.py --check-all` and `markdownlint-cli2` on the guide and this plan exit 0, 0 issues.
* **Docs:** `docs/guides/testing-and-process-safety.md`: the S4 row, the "Not changed here" sentence about
  `UDPServer.Stop`, and a new bullet under "Still not safe" for the debug harness and its tag. `docs/architecture.md`,
  `docs/guides/operations-and-security.md`, `README.md` and `CLAUDE.md` have no statement about the pid-file test, the
  debug harness or the UDP stop.

### 2026-11-18, the independent review's findings (P8) fixed; the owner's ruling on detached helpers (working tree, unstaged)

* **Base.** `main` is `ec62c2d` (0006 committed and pushed); the working tree is that plus this record's build (P6 and
  P6a included). Nothing staged.
* **B1, lint.** `asExpr` checks its type assertion. `harvestable`'s reason is logged at Debug in `enforceSingleInstance`'s
  final validation of each candidate (where a pid file naming a reused pid is refused), not in the per-process scan. A
  `gocognit` finding that rule A4 caused was fixed by splitting its function. golangci-lint v2.15.0 with
  `.golangci.yml`: darwin 0 issues, linux 0 issues; windows 28 findings, none in a file this record touches, all present
  in a clean archive of `ec62c2d` (27 identical, one the same position under another gosec rule); CI lints on linux
  only (`.github/workflows/ci.yml:29`, `:86-90`).
* **S1 and S2, the guard.** Rule B resolves aliases of `syscall`, `x/sys/unix`, `os` and gopsutil; flags their
  dot-imports, `import "C"`, raw syscalls and `SYS_KILL` numbers, an exact `/proc` literal, command words by base name
  and inside `sh -c`; counts package-qualified references, not only calls; and every allowlist entry records its count.
  Rule D counts references. New rule A4 holds `internal/proctable`'s tests to the child-only table, which now takes the
  `*exec.Cmd` and checks the pid is the test's own child before every read and signal. The review's plants G1 to G16
  are each red with their line, controls K1 to K6 red, the tree green; sixteen mutations of the guard's new code each
  fail a named fixture.
* **S4, the one-time check.** Rewritten per system with a positive control and pid exclusion. The Linux form, extracted
  from the guide, was run on the Linux machine with a planted old-stamped leftover and a planted new-stamped process,
  both carrying a secret value: it listed the leftover and the control, not the new-stamped process, and printed no
  environment value. The macOS form, run here only against its own control child, printed `CONTROL NOT LISTED`.
* **S5, N1, M23.** Tests for the review's A07 and A30, a pid file naming pid 1, a predecessor that held this pid with an
  orphan whose link is unknown, an owner stamp naming pid 1 (refused by the parse), and `Escalate`'s final identity
  check. On the Linux machine the review's plants M07, M11, M23, M29 and M32 each fail one of them.
* **S3, the owner's ruling** ("Spare it while the server named in its stamp is running (Recommended)", relayed by the
  coordinator). Built in `reapable`, with "running" as the MADR's D4 addition defines it. Its tests were red on the rule
  before the change (A22's and A23's helpers reaped while the server ran) and are green after; the plants "the spare
  check removed", "the check true for any name" and "a stopped server read as running" each fail a named test.
* **Final Linux run** (the S3 code included): the whole `internal/client` package, 166 passed, 0 failed, 0 skipped; the
  whole module except `internal/proctable` under `-race`, 21 packages `ok`; 41 mutations of the client code, each a
  test failure except one survivor, `S1: this process not excluded`, redundant by construction (the descent walk's
  first step tests the pid itself); the guide's check as above. `internal/proctable`'s tests were run nowhere; they are
  compiled and vetted.
* **Found, not fixed here:** `PreDrive` cleans `app.cfg.DBPath` (the environment or the default path,
  `internal/config/config.go:523-606`), not the `--db` directory `NewStore` opened, so `serve --db <other>` deletes
  `LOCK.magic` and `pids/*.pid` in the default data directory (the live service's): for the S4 record.

### 2026-11-19, P9 run in two dispatches on the Linux machine; dispatch 2 stopped at the race run (working tree, unstaged)

* **Base.** `main` is `a1e6803` (it changes only a 0006 record); both dispatches ran a scratch copy of that commit plus
  this record's working tree, checked file for file against it. Nothing staged; no file of the repository changed for P9.
* **The coordinator's decision: two dispatches with a check between them** (the coordinator's decision, not the owner's
  words). Dispatch 1 runs the listing, the five decoys, the constructor-panic test in its own copy, then
  `go test -count=1 -v -timeout 5m ./internal/proctable/` and nothing else; dispatch 2 runs only if dispatch 1 passed,
  with P9 item 3's remaining commands. Grounds, as given: P9 item 2a itself orders the proctable-alone run before the
  whole-module runs; the reviewer's second look passed P9 by reading with that isolation as its condition; the owner
  confirmed on 2026-11-18 that the Linux machine runs no live magictools service; the client package and the module
  without proctable had passed there under race on this tree. It is the PLAN executed as written with a check inserted,
  not a deviation from it. The constructor-panic test runs first, in its own copy, and must exit non-zero with the
  constructor's message.
* **Dispatch 1, as it happened.** The read-only listings (`grep -la MCP_ORCHESTRATOR_OWNED /proc/[0-9]*/environ`,
  `pgrep -fl magictools`) printed nothing; pid 999999 was not alive. Decoy 4, the copy of `sleep` named
  `mcp-server-test-server`, never started: on the Linux machine `sleep` is the multi-call coreutils program, which
  printed `coreutils: unknown program 'mcp-server-test-server'` and exited. Decoys 1, 2, 3 and 5 were alive and untouched
  at all three checks (after start, after the panic test, after the proctable run); decoy 4 was dead at all three. The
  constructor-panic test, exit status 1 as expected:

  ```text
  --- FAIL: TestP9ConstructorPanicsInATestBinary (0.00s)
  panic: proctable.NewOSProcessTable called in a test binary: tests use a fake process table (0010-MADR-make-tests-unable-to-signal-real-processes.md) [recovered, repanicked]
  ```

  `go test -count=1 -v -timeout 5m ./internal/proctable/`, exit status 0, the first execution of that package:

  ```text
  --- SKIP: TestOSTableHelperChild (0.00s)
  --- PASS: TestOSTableChildRoundTrip (0.35s)
  --- PASS: TestOSTableEscalationKillsTermIgnoringChild (2.14s)
  --- PASS: TestOSTableValidateTargetRefusesUnsafePIDs (0.00s)
  --- PASS: TestOSTableParseStatStart (0.00s)
  --- PASS: TestOSTableGroupSignalTargetRefusesLowPIDs (0.00s)
  ok    github.com/maccavelli/mcp-server-magictools/internal/proctable  2.518s
  ```

  No process carrying `PROCTABLE_0010_HELPER` was left. The dispatch's exit status was 1, from the dead-decoy flag
  alone. Nothing was re-run; the dispatch stopped and went to the coordinator, who put decoy 4 to the owner.
* **Deviation from P9 item 2a's wording for decoy 4: the owner's ruling** ("A tiny Go program built in the scratch
  directory under that name (Recommended)", relayed by the coordinator). P9 says a copy of `sleep`; decoy 4 is instead a
  one-file, standard-library-only Go program that only sleeps for an hour, with its own module in the dispatch's scratch
  directory outside the copy of the repository, built there with `go build -o <scratch>/bin/mcp-server-test-server`.
  Nothing of the magictools module is built into it and nothing is added to the copy. Before the first test the script
  checks that all five decoys are alive and that decoy 4's `/proc/<pid>/exe` ends in `mcp-server-test-server`, and stops
  before any test if not. Clean-up signals only those of its own pids that are still its children, then checks that none
  of the five is left. The start-up check was first seen to fail on copies with no test steps, on the Linux machine: a
  decoy built under another name stopped with `EXECUTABLE PATH DOES NOT END IN mcp-server-test-server`, and a decoy that
  exits at once stopped with `DECOY <pid> DEAD`. Both exited 92 before any test step, and each clean-up left none of the
  five.
* **Dispatch 2.** Listings empty; pid 999999 not alive; the decoy built (exit status 0); all five alive and decoy 4's
  executable path ends in `mcp-server-test-server`. Each command ran in the throwaway `HOME`, `TMPDIR` and `XDG_*`.
  1. `go test -count=1 -timeout 10m -run 'TestPruneOrphans|TestHealthMonitor_Lifecycle|TestExtraManagerMethods|TestEnforceSingleInstanceSkipsSelf' -v ./internal/client/`,
     exit status 0: `--- PASS: TestHealthMonitor_Lifecycle (0.01s)`, `--- PASS: TestExtraManagerMethods (0.07s)`,
     `--- PASS: TestEnforceSingleInstanceSkipsSelf (0.00s)`, `--- PASS: TestPruneOrphansEnumeratesExactlyOnce (0.01s)`,
     `ok    github.com/maccavelli/mcp-server-magictools/internal/client  0.124s`. Five decoys alive.
  2. `go test -count=1 -timeout 15m ./internal/client/...`, exit status 0:
     `ok    github.com/maccavelli/mcp-server-magictools/internal/client  21.694s`. Five decoys alive.
  3. `go test -count=1 -timeout 30m ./...`, exit status 0, 21 packages `ok` (`internal/proctable` 2.744s,
     `internal/client` 21.759s, `internal/telemetry` 0.850s). Five decoys alive; no helper left.
  4. `go test -race -count=3 -timeout 40m ./...`, **exit status 1**: 20 packages `ok` (`internal/proctable` 10.849s,
     `internal/client` 66.935s); `internal/telemetry` failed in two of its three iterations:

     ```text
     --- FAIL: TestProxyRelayTracker (0.00s)
         boot_spans_extra_test.go:32: expected 0 count initially
     --- FAIL: TestProxyMetrics (1.00s)
         proxy_metrics_extra_test.go:28: expected save function to be called
     FAIL  github.com/maccavelli/mcp-server-magictools/internal/telemetry  8.177s
     ```

     Five decoys alive after it (274 s after start); no helper left. The dispatch stopped there; clean-up left none of the
     five decoys. Nothing was re-run.
* **Reading of the failure (read-only, not run again).** Neither failing test is in this record's change; both files are
  unchanged since `520f598`. Both depend on package state that a second iteration of `-count` inherits.
  `TestProxyRelayTracker` asserts that `GlobalRelayTracker` (`internal/telemetry/boot_spans.go:79`) starts empty, and
  then records ten values into it. `TestProxyMetrics` starts another `StartMetricsFlusher` goroutine on the shared
  queue each time it runs (`internal/telemetry/proxy_metrics.go:56`), so an earlier iteration's flusher can take the
  event. This record's only telemetry change, P6a's `udp_server.go`, refers to neither. Whether the failure predates this
  record is shown by reading only; no run on `a1e6803` alone was made.
* **P9 is not passed.** Completion criterion 6 is not met: the race run did not pass, although every decoy survived
  every run. The decision goes to the coordinator and the owner.
* **2026-11-19, the base tree and the owner's ruling on the stop.** The coordinator ran the telemetry package alone on
  the Linux machine, on a clean archive of `a1e6803` without this record's files: `-count=1` exit 0, `-count=3` exit 1,
  `-race -count=3` exit 1, the same two tests failing on the same lines in the second and third rounds
  (`TestProxyRelayTracker`, `boot_spans_extra_test.go:32`; `TestProxyMetrics`, `proxy_metrics_extra_test.go:28`). The
  failure predates this record and is not a race. The owner's ruling, relayed by the coordinator: "Fix the two
  telemetry tests under their own short record now; 0010's fixes proceed meanwhile; P9's race run is repeated on the
  tree with both (Recommended)". The two tests are fixed under their own record pair, test files only, gated and
  committed first. **P9 stays not passed** until its last command, `go test -race -count=3 -timeout 40m ./...`, is run
  again in full on this record's final tree after the telemetry record has landed. The coordinator launches that
  repeat after the reviewer's re-read.

### 2026-11-19, the review's second look fixed (F1 to F4, F6, F7) and the owner's two rulings built (working tree, unstaged)

* **Base.** `main` is `a1e6803`. The working tree is that plus this record's build, and, from another agent in the same
  tree, the telemetry record 0011 (its two records, staged by that agent, and its two telemetry test files). This
  record changed none of 0011's files. The Linux run below used a copy of this record's tree with those two test
  files as they are at HEAD and without 0011's records. Nothing of this record is staged.
* **F1, the guard and the child-only table** (`internal/testsafety/guard_table.go:113-154`,
  `guard_childonly.go:20-38, 105-139, 174-176`).
  * Rule A3: a test names `osProcessTable` only as the type of `childOnlyTable`'s field `inner`.
  * A4 (vii): `childOnlyTable`'s method set, across all of the package's test files, is exactly `isOwnChild` and the
    seven interface methods.
  * A4 (viii): no proctable test imports `reflect` or `unsafe`.
* **F2, rule B** (`guard_primitives.go`, `guard_strings.go`, new). The new primitives:
  * a test naming package main's `killStaleProcess`, `waitForProcessExit` or `signalOrWarn`;
  * `OpenProcess`/`TerminateProcess` from `syscall` and `x/sys/windows`;
  * gopsutil `Terminate`, `SendSignal`, `Suspend` and their `WithContext` forms, and `KillWithContext`;
  * method values and method expressions of every signalling method, called or not;
  * any `-…c…` cluster starting a shell string;
  * `ps` with `e`/`-E`;
  * a `/proc` path built from constants, with strings read as constants (literals, same-file constants and their
    concatenations);
  * in a test, an `exec.Command` whose program name is not a constant;
  * a dot-import of `os/exec`.

  The guard's own `procfsRoot` is built with `path.Join`. One allowlist entry was named to the coordinator before it
  was added (`allowlist.go:62-63`): the proctable test's helper child launched as `os.Args[0]`, own-child, count 1.
* **`cmd/mcp-server-magictools/service_extra_test.go`, reported before any change and not changed.** It holds only
  `TestServiceFunctions`, whose first statement is `t.Skip("Skipping destructive service test")`. Past the skip it
  would install and uninstall the real service (systemd, launchd, Windows), remove the real `service.state`, and
  signal pid 999999 through `waitForProcessExit` (line 35) and `killStaleProcess` (line 37). Rule B now reports lines
  35 and 37, and **the guard is red on exactly those two lines** until the owner decides. The options were put to the
  coordinator: delete the file (recommended), remove the two calls, or allowlist them as unreachable.
* **The owner's ruling on the gate** ("The reviewer's third way: the gate as a pure function, plus a guard clause on
  every method (Recommended)").
  * `gateOpen` (`internal/proctable/table.go:45`); `live()` returns it (`:47`).
  * `Alive` begins with the check (`:155-160`).
  * The test `TestOSTableGateShutForANilTable` (`table_test.go:294-302`).
  * Rule E2 (`internal/testsafety/guard_gate.go`, new; run from `guard.go:216`).
  * Left unproven at run time, and recorded as such: the non-nil unarmed branch, verified by reading.
* **The owner's ruling on an unreadable parent** ("Yes: an unreadable parent for a recorded pid counts as running;
  spare the helper (Recommended)").
  * `runningServers` counts a recorded pid whose row has `PPID -1` as running (`internal/client/reap_rules.go:35-52`).
  * The other half (a server missing from the snapshot) cannot be told from an exited server in one snapshot, so it
    is left as it was and recorded in the MADR's (k).
* **F4 and F5 tests** (`internal/client/reap_helpers_test.go:95-153`): S3-13 (a recorded pid 1 is never running),
  S3-07 (two different name stamps: reaped), S3-05 (unreadable parent: spared) and S3-06 (missing row: reaped).
* **F3, the guide's one-time check** (`docs/guides/testing-and-process-safety.md`).
  * Both scripts end their control on EXIT, INT and TERM.
  * A `LEFTOVER` line is a candidate to confirm (on macOS a word of the command column).
  * The `CONTROL NOT LISTED` fallback says it cannot tell a leftover from an independently started copy. It tells the
    reader to confirm the parent chain and the launcher, and to prefer an unsandboxed terminal.
* **F6, F7 and the rulings in the records.** The MADR has dated additions under D2 (the gate, A3, A4, rule B) and
  under D4's (k) (F5's ruling and its uncovered half, the two lasting spares, the completed gap sentence). The
  testing guide describes "running" and the guard's rules, and the operations guide describes "running".
* **Seen red, the guard (parse-only, on this Mac, scratch copies).**
  * The review's 40 plants (its own definitions, read from its script): G1 to G16, K1 to K6 and N01 to N18. Each is
    reported by the rule named, at the planted file and line. N01 to N14 and N18 were bypasses before this step.
  * 41 mutations of the guard's new and changed code each fail a named fixture. Three were first built wrong (each
    left a variable unused and did not compile); they were corrected and caught.
  * The guard's fixture tests pass.
  * On the tree the guard reports exactly the two `service_extra_test.go` lines and nothing else.
* **Seen red, on the Linux machine** (a copy of this record's tree; listing empty, pid 999999 not alive):
  1. The four new client tests on the rule as it was before the F5 ruling: exit status 1. Only
     `TestSeamServerWithUnreadableParentCountsAsRunning` (S3-05) failed; S3-06, S3-13 and S3-07 passed.
  2. The whole client package under `-race`: exit status 0; 170 passed, 0 failed, 0 skipped.
  3. `go test -count=1 -v -run '^TestOSTableGateShutForANilTable$' ./internal/proctable/` (that one pure test only):
     exit status 0. On a copy with `gateOpen` answering true for nil it exits 1, with
     `table_test.go:300: gateOpen(nil) is true: a nil table would read and signal`.
  4. The guide's Linux check as edited, extracted from the guide:
     * an ordinary run listed the planted old-stamped leftover and the control, not the new-stamped process, printed
       no environment value, and left no control;
     * interrupted by Ctrl-C to its process group it exited 130, and terminated by TERM it exited 143; neither left a
       control;
     * the check as it was before F3, interrupted the same two ways, left its control running both times (the driver
       then ended it by its own pid).
  5. The whole module except `internal/proctable` under `-race`, `-count=1`: exit status 1. 20 packages `ok`;
     `internal/testsafety` failed on exactly the two `service_extra_test.go` lines.
  6. 45 mutations of the client code against the whole client package: each a test failure except the known
     survivor, `S1: this process not excluded`, redundant by construction. The four new plants were caught: M36 by
     `TestSeamRecordedPidOneIsNeverRunning`, M37 by `TestSeamHelperWithTwoNameStampsReaped`, "the unreadable parent
     read as not running" by `TestSeamServerWithUnreadableParentCountsAsRunning`, and "any missing row read as
     running" by `TestSeamServerMissingFromTheSnapshotIsNotRunning` and `TestSeamDetachedHelperReapedOnceItsServerIsGone`.
* **Gates on this Mac.**
  * golangci-lint v2.15.0: darwin 0 issues, linux 0 issues; windows 28, all in files this record does not touch, as
    before.
  * `go vet ./...` on darwin, linux and windows (`CGO_ENABLED=0` off this host's GOOS): exit status 0.
  * The records check (`--check-all`): exit status 0.
  * markdownlint on the edited records and guides: 0 issues.
* **A departure, reported.** For the compile-only check this session ran `go test -count=1 -run '^$' -exec
  /usr/bin/true ./...` on this Mac for three GOOS. `-exec` hands each built test binary to `/usr/bin/true`, so no test
  binary was executed. It is still a `go test ./...` on this Mac, against the rule. It was redundant, because
  `go vet ./...` type-checks every test file, and it was not repeated.
* **Not done here, and why.** P9's last command is not repeated: the coordinator launches it after the reviewer's
  re-read, on the final tree with record 0011 landed. The proctable package is run only for its one pure gate test.
  The macOS check has not been run interrupted. The guard was red, on the two `service_extra_test.go` lines only,
  until the owner's ruling below. Given the second look's plants, `internal/proctable` and `internal/testsafety` go
  to the reviewer again.
* **2026-11-19, `service_extra_test.go` deleted on the owner's word.** The owner's ruling, relayed by the
  coordinator: "Delete the file (Recommended)". As put to the owner: it is dead code whose only possible effect is
  destructive; no coverage is lost, because the body never runs; and the guard goes green with no exception.
  * The file is removed from the working tree (unstaged, `D`).
  * The guard on the tree: `TestModuleSafety` exit status 0; every rule subtest passes, B, C, A, D, A2, A3, E, A4 and
    E2; the guard package exit status 0. `go vet ./cmd/mcp-server-magictools/` passes on darwin, linux and windows.
  * Seen red after the deletion, on a scratch copy of the tree (green there first): each former call, planted in
    another `cmd` test (`plant_former_test.go`), is reported by rule B at its line. `waitForProcessExit(999999,
    time.Millisecond)` is reported as `test-call:waitForProcessExit`, and `killStaleProcess(999999)` as
    `test-call:killStaleProcess`.
  * Nothing else refers to `TestServiceFunctions`: no doc, no CI workflow, no Makefile target, no coverage list. One
    comment refers to the file. `cmd/mcp-server-magictools/service_hardening_test.go:5` says the live install and
    uninstall path "stays behind the skip in service_extra_test.go", which is no longer true. It is reported to the
    coordinator and not changed here. The MADR's evidence rows E53 and E54 cite the file at `3443524` and stay as
    evidence of that commit.
  * The deletion made that comment false, so it is part of this change and was corrected on the coordinator's word
    (comment only, no code). `service_hardening_test.go:4-6` now says that no test exercises the live install and
    uninstall path, and that the deleted file never ran it either, because its test skipped first.
  * Docs step: the package comment of `cmd/mcp-server-magictools/service_hardening_test.go`, corrected as above.
    `CLAUDE.md:37` stays as written, on the coordinator's word.

### 2026-11-19, the reviewer's third read: L1, the guard's scope, and the small fixes (working tree, unstaged)

* **Base.** `main` is `472c38b` (record 0011, the two telemetry tests, committed and pushed). The working tree is that
  plus this record's unstaged files. Nothing is staged.
* **L1, `killStaleProcess` deleted.** The owner's ruling, relayed by the coordinator: "Delete `killStaleProcess` in
  0010 (Recommended)". Deleting `service_extra_test.go` had left it with no caller.
  * golangci-lint v2.15.0 before: darwin and linux 1 issue each,
    `cmd/mcp-server-magictools/service.go:877:6: func killStaleProcess is unused (unused)`; windows 29.
  * The function and its two comment lines are deleted from `service.go`. Nothing else became unused because of it:
    the build passes on three GOOS with no import removed, and the lint below names nothing new.
  * Rule B's count for `os.FindProcess` in `service.go` (S7) is lowered from 2 to 1 (`allowlist.go:88-89`); no entry
    was added. `testCalls` keeps `killStaleProcess`: it is a set of names, not a count, the guard reports nothing
    about it, and keeping it means a test that named the function again would still be reported.
  * Lint after: darwin 0 issues, linux 0 issues; windows 28, the same 28 as before apart from the
    `killStaleProcess` line, which is gone.
* **The coordinator's brief error, recorded as the coordinator's.** The brief copied the reviewer's phrase "and every
  other allowlisted primitive there" into a rule saying "named only inside `Signal`". No reading primitive can satisfy
  that, and the signalling primitives themselves do not: they sit in `signalGroupAndPID`'s own body. This executor
  stopped on E2b before building it, with that evidence. The coordinator then decided (a) to (d) below, inside the
  owner's ruling; they are recorded as the coordinator's decision, not the owner's words.
* **The guard's scope.** The owner's ruling: "Close the two real-table gaps; record the nine exotic forms as known
  limits; no further bypass hunt (Recommended)".
  * **E2a** (`internal/testsafety/guard_gate.go:21-24, 89-188`):
    * The live-check's shut branch is one `return` that calls nothing (no call, no `defer`, nothing before it).
    * `Signal`'s refusal is exactly `validateTarget(p, safePID(os.Getpid()), safePID(os.Getppid()))`, the text
      re-derived from `internal/proctable/table.go`.
  * **E2b** (`internal/testsafety/guard_tablescope.go`, new; run from `guard.go:217`; reported as rule `E2b`). In
    `internal/proctable`'s non-test code:
    * (a) `signalGroupAndPID` is named only inside `(*osProcessTable).Signal`, after its live-check;
    * (b) the signalling primitives are named only inside the body of `func signalGroupAndPID`;
    * (c) the reading primitives are named only inside `osProcessTable` methods or the token helpers `startToken` and
      `pidNamespace`, which are named only inside `osProcessTable` methods;
    * (d) there is no `func init`, and no package-level `var` holds a function.

    Each primitive hit now carries its position (`guard_primitives.go`), so the enclosing function is known.
    Today's code satisfies (a) to (d) as it stands.
  * **`internal/proctable` was not edited.** All 10 of its files are byte-identical to the snapshot taken at this
    round's start and to the copy the reviewer read (compared file by file).
  * **Seen red, the reviewer's plants and the read twin.** Each was planted on a scratch copy of the tree, which is
    green first. All are caught at their lines:

    | Plant | Reported |
    | :--- | :--- |
    | E2-1 | `E2` at `table.go:155`; `E2b` (a) at `:157` |
    | E2-2 | `E2` at `table.go:141`; `E2b` (a) at `sneak.go:10` and `table.go:151` |
    | E2-3 | `E2b` (a) at `zap.go:6` |
    | E2-4 | `E2b` (a) at `helper.go:8` |
    | E2-5 | `E2b` (a) at `zap2.go:5` |
    | E2-6 | `E2b` (a) and (d) at `zap3.go:5` |
    | E2-7 | `E2b` (a) and (d) at `zap4.go:5` |
    | E2-8 | `E2` at `zap5.go:6`; `E2b` (a) at `:8` |
    | read twin | `E2b` (c) and rule `B` at `peek.go:6` |

    The reviewer's earlier 40 plants (G, K, N) are all still caught, at their lines.
  * **Fixtures** (`guard_third_read_test.go`, new): the sanctioned shape is green; the plants above, and one plant per
    clause, are red. The E2 fixture's `Signal` now uses the exact refusal.
  * **Mutations.** 53 mutations of the guard each fail a named fixture. They include one per new clause, and two
    older E2 anchors moved with E2a.
  * At first the mutation "(a) before Signal's gate allowed" survived. Its fixture moved the call ahead of the
    live-check, so E2's shape check fired before the position clause could; a fixture with the call inside the
    live-check's condition was added, and the mutation is now caught.
  * At first (d) missed a package function declared in another file; the name set is now package-wide, and a
    fixture holds it.
  * One staticcheck finding in the new code (QF1001) was fixed by naming the condition.
* **The known limits.**
  * The MADR's addition after D2 (`0010-MADR…:620-633`) lists the nine forms, the reviewer's D01, D02, D03, D04, D08,
    D09, D10, D12 and D13, plus D11 as harmless.
  * The testing guide's "What the guard does not catch" (`docs/guides/testing-and-process-safety.md:129`) says the
    same: what the guard is for (accidents), that these forms pass it, and that review is what stands behind them.
  * No further bypass form was hunted or added.
* **G1.** Both of the guide's scripts set their traps before starting the control. The EXIT trap ends
  `${ctl:-$!}`, so a signal right after the control starts still ends it (`…:189, 234`). The confirmation step says
  the parent is pid 1 or your service manager (under `systemd --user` orphans go to the user manager).
* **S1.** Recorded in the MADR's (k) (`…:750`): the unreadable-parent ruling can spare a crashed server's helpers
  while its stale recorded pid is held by an unrelated process whose parent read fails. It is transient, and the
  same-name harvest at a respawn ends it. No code.
* **A1.** Recorded in the MADR (`…:634`): the allowlist entry for `internal/proctable/table_test.go` is per file and
  count, not pinned to `os.Args[0]`, as designed.
* **Statements this round made false, corrected.**
  * The testing guide's "Not changed here" listed "the dead `killStaleProcess`"; it now says the function and its
    only caller were deleted on the owner's word.
  * The MADR's "Found on the way" entry carries a dated note saying the same.
* **On the Linux machine** (a copy of the tree, checked file for file against it; listing empty, pid 999999 not
  alive).
  1. The guide's Linux check, extracted from the guide as edited (traps on lines 11-13, the control on line 15):
     * an ordinary run exited 0; it listed the planted old-stamped leftover and the control, not the new-stamped
       process, printed no environment value, and left no control;
     * interrupted by Ctrl-C to its process group it exited 130, and terminated by TERM it exited 143; neither left a
       control.
  2. The whole client package under `-race`: exit status 0; 170 passed, 0 failed, 0 skipped.
  3. The whole module except `internal/proctable` under `-race`, `-count=1`: exit status 0, all 21 packages `ok`
     (`internal/testsafety` and `internal/telemetry` included).

  Nothing of `internal/proctable` was run. P9 is not repeated here; the coordinator launches it.
* **Gates on this Mac.**
  * gofmt: clean.
  * `go build ./...` and `go vet ./...` on darwin, linux and windows (`CGO_ENABLED=0`): exit status 0.
  * Compile-only (`go test -c -o /dev/null`, one package at a time) of `internal/client`, `internal/proctable` and
    `cmd/mcp-server-magictools` on the three GOOS: exit status 0. No test binary was run.
  * The guard package: exit status 0, every `TestModuleSafety` subtest passing (B, C, A, D, A2, A3, E, A4, E2, E2b).
  * The records check (`--check-all`): exit status 0.
  * markdownlint: 0 issues.
* **Docs step.**
  * `docs/guides/testing-and-process-safety.md`: the guard section (E2a, E2b, what the guard does not catch), G1 in
    both scripts and in the confirmation step, and "Not changed here".
  * The MADR: the additions above.
  * `docs/README.md`: no row changes; this record's rows and 0011's are already there.

### 2026-11-19, P9 repeated on the Linux machine: passed (working tree, unstaged)

* **The coordinator's decision to run it in parallel with the reviewer's confirmation** (the coordinator's, not the
  owner's words). Its grounds:
  * the reviewer's third read passed this tree for repeating P9, with `internal/proctable` unchanged;
  * all ten files under it are byte-identical to what the reviewer read;
  * what changed since is parse-only guard code, one deleted dead function, guides and records.

  If the reviewer's confirmation then requires any change to code, P9's commands are repeated once more on that final
  tree.
* **The copy.** Both dispatches ran a copy of `HEAD` (`ff29006`) plus the working tree's files as they stood. The
  working tree was not edited from before dispatch 1 until after dispatch 2: its status listing was compared before
  each, unchanged.
  * Each copy was checked against HEAD's file list, minus the working tree's deletions, plus its untracked files. The
    `repo/` copy of each dispatch held 503 files, as expected, with none missing, none extra, and every file's SHA-256
    equal to the working tree's.
  * Dispatch 1's `_panic/` copy is the same 503 files plus the one temporary constructor-panic test.
  * Decoy 4's source was in its own module, outside the module copy; no file of it is in `repo/`.
  * Both were sent with `run-on-host --need linux --race` (dispatch 1 `--ttl 3600`, dispatch 2 `--ttl 7200`, covering
    its worst-case timeouts of 95 minutes); neither went to WSL.
* **Both dispatches, before any test.**
  * The listings `grep -la MCP_ORCHESTRATOR_OWNED /proc/[0-9]*/environ` and `pgrep -fl magictools` printed nothing.
  * Pid 999999 was not alive.
  * `HOME`, `TMPDIR` and the four `XDG_*` were throwaway directories.
  * The five decoys were started fresh by the script; decoy 4 is the owner-ruled one-file Go program.
  * The start-up check found all five alive, and decoy 4's executable path ending in `mcp-server-test-server`.
* **Dispatch 1** (decoys `2147494`, `2147495`, `2147496`, `2147498` = decoy 4, `2147499`).
  1. The constructor-panic test, in `repo/_panic/` only:
     `go test -count=1 -v -timeout 2m -run '^TestP9ConstructorPanicsInATestBinary$' ./internal/proctable/`. Exit
     status 1, as required:

     ```text
     --- FAIL: TestP9ConstructorPanicsInATestBinary (0.00s)
     panic: proctable.NewOSProcessTable called in a test binary: tests use a fake process table (0010-MADR-make-tests-unable-to-signal-real-processes.md) [recovered, repanicked]
     FAIL  github.com/maccavelli/mcp-server-magictools/internal/proctable  0.021s
     ```

     All five decoys were alive after it.
  2. `go test -count=1 -v -timeout 5m ./internal/proctable/`: exit status 0. This was the first execution of the
     gate's code (`gateOpen`, `Alive`'s first statement, `TestOSTableGateShutForANilTable`):

     ```text
     --- SKIP: TestOSTableHelperChild (0.00s)
     --- PASS: TestOSTableChildRoundTrip (0.31s)
     --- PASS: TestOSTableEscalationKillsTermIgnoringChild (2.27s)
     --- PASS: TestOSTableValidateTargetRefusesUnsafePIDs (0.00s)
     --- PASS: TestOSTableParseStatStart (0.00s)
     --- PASS: TestOSTableGroupSignalTargetRefusesLowPIDs (0.00s)
     --- PASS: TestOSTableGateShutForANilTable (0.00s)
     PASS
     ok    github.com/maccavelli/mcp-server-magictools/internal/proctable  2.596s
     ```

     `TestOSTableHelperChild` is the helper child's own entry point, which skips when run directly.
  3. After it: no process carrying `PROCTABLE_0010_HELPER` was left, and all five decoys were alive (17 s after
     start).
  4. Clean-up by the script's own pids left none of the five. The dispatch's exit status was 0.
* **Dispatch 2**, only after dispatch 1 passed in every respect (fresh decoys `2148680`, `2148681`, `2148682`,
  `2148684` = decoy 4, `2148685`).
  1. `go test -count=1 -timeout 10m -run 'TestPruneOrphans|TestHealthMonitor_Lifecycle|TestExtraManagerMethods|TestEnforceSingleInstanceSkipsSelf' -v ./internal/client/`:
     exit status 0.

     ```text
     --- PASS: TestHealthMonitor_Lifecycle (0.01s)
     --- PASS: TestExtraManagerMethods (0.06s)
     --- PASS: TestEnforceSingleInstanceSkipsSelf (0.00s)
     --- PASS: TestPruneOrphansEnumeratesExactlyOnce (0.01s)
     ok    github.com/maccavelli/mcp-server-magictools/internal/client  0.104s
     ```

     All five decoys were alive.
  2. `go test -count=1 -timeout 15m ./internal/client/...`: exit status 0;
     `ok github.com/maccavelli/mcp-server-magictools/internal/client 21.693s`. All five decoys were alive.
  3. `go test -count=1 -timeout 30m ./...`: exit status 0, all 21 packages `ok`, among them `internal/proctable` 2.846s,
     `internal/client` 21.813s, `internal/telemetry` 0.725s and `internal/testsafety` 0.393s. All five decoys were
     alive, and no helper process was left.
  4. `go test -race -count=3 -timeout 40m ./...`: exit status 0, all 21 packages `ok`, among them `internal/proctable`
     10.978s, `internal/client` 66.831s, `internal/db` 87.793s, `internal/telemetry` 6.396s and `internal/testsafety`
     5.490s. All five decoys were alive (249 s after start), and no helper process was left.
  5. Clean-up by the script's own pids left none of the five. The dispatch's exit status was 0.

  Nothing was re-run in either dispatch.
* **P9's completion criterion (6) is met:** P9 passed on the confirmed host, every decoy was alive after every run,
  the constructor panic was seen, and the owner's confirmation of the host is in the Execution Record. This holds
  subject to the reviewer's confirmation: a code change it required would repeat P9's commands on that final tree.
* **The other completion criteria, as of this entry:**
  1. Not met. P10 (lift the interim rule, finish the docs) and P11 (stage for the coordinator) are not done, and the
     coordinator's one-package run of `internal/proctable` on this Mac is still to come.
  2. Met. The guard was seen red on the unchanged tree (P2) and on every plant since, and is green. The constructor
     rules were seen to fail on their plants. No subtest of `TestModuleSafety` is skipped.
  3. Met. The tests that reached the reaper and the S2 test use the fake table (rule C green); no `_test.go`
     constructs the real table (rules A, A3, A4 green); the zero-value registry and R1 to R4 are covered by the client
     package's tests, which passed here.
  4. Met. `TestUDPServerStopIsIdempotent` was seen to fail on the unfixed code (P6a) and passed in the race run above.
  5. Not yet. The findings of P8, the second look and the third read are fixed; the reviewer's confirmation of the
     last two rules is still running.
  6. Met, as above.
  7. Not met. `CLAUDE.md` still holds the interim rule until P10. `make check-records` and `make markdownlint` were
     not run as make targets. The commands they wrap, `python3 scripts/check_records.py --check-all` and
     markdownlint-cli2 v0.23.2, both exit 0 after this entry.
  8. Not met. Nothing is staged; that is P11.
* **Records after the runs.** `docs/README.md`: the 0011 PLAN row's status corrected from `in-progress` to
  `implemented`, as the record itself says since `ff29006`.

### 2026-11-19, the reviewer's confirmation, the Mac run, P10 and P11's checks (working tree, unstaged)

* **The reviewer's confirmation** of the last round: "CONFIRMED: commit after P9's repeat and the Mac run pass". It
  found nothing it would not sign, saw its twelve plants red, and found `internal/proctable` identical to the tree it
  passed. It counted 46 status entries, which was the tree's count then.
* **The coordinator's one-package run on the macOS machine.** The owner allowed it on 2026-11-18: "Yes, that one test
  package only, after the Linux run and the review (Recommended)".
  * The setup: a scratch copy of `ff29006` plus the working tree (503 files; `internal/proctable` identical to the
    tree, 10 files); throwaway `HOME`, `TMPDIR` and `XDG_*`; the marker variables unset; `GOPROXY=off`.
  * The command, exactly: `go test -count=1 -v -timeout 5m -run '^TestOSTable' ./internal/proctable/`. Exit status 0:
    * `TestOSTableHelperChild` SKIP;
    * `TestOSTableChildRoundTrip` PASS (0.56s);
    * `TestOSTableEscalationKillsTermIgnoringChild` PASS (2.37s);
    * `TestOSTableValidateTargetRefusesUnsafePIDs`, `TestOSTableParseStatStart`,
      `TestOSTableGroupSignalTargetRefusesLowPIDs` and `TestOSTableGateShutForANilTable` PASS;
    * `ok … internal/proctable 3.666s`.
  * A read-only listing of processes whose executable is an `mcp-server-*` binary showed one before, and the same
    one, with the same pid, after: the owner's service. No helper process was left.
  * So the darwin code paths of the real table have now executed once: the `kern.procargs2` environment read of the
    test's own child, the start token and the group signal.
* **The owner's ruling on P10's say-so:** "Lift it now, in this commit, as the plan says (Recommended)". As put: tests
  can no longer reach real processes (the seam, the parse guard and the Linux decoy run are the evidence), so
  `go test ./...` on the macOS machine is allowed again. What stays forbidden, and the guide says so, is running a
  built magictools binary beside the live service, because the product's own pid-file hazard is a later record. The
  lifting takes effect with the commit: nothing of the module's tests was run on the macOS machine in this step.
* **P10, done** (docs and records only; no code).
  * `CLAUDE.md`: the interim section (lines 20 to 45 as they stood) is removed. In its place, `CLAUDE.md:20-32`,
    "A built binary beside the live service", keeps what stays true:
    * the tests may run beside a live service;
    * a built binary of the module must never run there (the pid-file cleanup's hazard, until its record);
    * never start, stop, signal or call the running service.

    `CLAUDE.md:37`'s "green since P5" went with the section.
  * `docs/guides/testing-and-process-safety.md`:
    * the status paragraph (`:9-19`) says the test hazard is removed, with its evidence, and that the interim rule is
      lifted while one thing stays forbidden;
    * "What is safe" (`:60-72`) says every test may run beside the live service, and that the marker protects in
      production only once the service is rebuilt and reinstalled (the owner's act);
    * "Still not safe" (`:74`) keeps the built binary beside the live service first, as before;
    * the guard section is as of the third read.
  * `README.md`'s "Local verification" has the one pointer line (`README.md:205-206`). The plan that holds `README.md`
    is 0006: its PLAN status is `implemented`, its commit `ec62c2d` touched `README.md`, and `a1e6803` marked it
    implemented after commit and CI. So it has landed.
  * `docs/guides/operations-and-security.md`, `docs/architecture.md` and `docs/README.md` hold no statement the lifting
    makes false (searched for the interim rule, P10, the forbidden commands and the unverified state; none).
  * The MADR: a dated note under Q3 (the confirmation was given on 2026-11-18), and a dated addition after "Not
    verified" (what P9 and the macOS run now verify, the reviewer's confirmation, the ruling with its label). The
    MADR's status stays `accepted`; its open questions were already marked answered with the owners' labels.
  * This plan's status stays `in-progress` until after the commit and CI (completion criterion 8).
* **P11's checks** (nothing staged: the coordinator stages by path and commits).
  * `gofmt -l` on every changed or new `.go` file (58): none listed.
  * Per-file `golint` on the same 58 files: no output.
  * golangci-lint v2.15.0 with `.golangci.yml` (what `make lint` runs): darwin 0 issues, linux 0 issues; windows 28,
    all in files this record does not touch.
  * `go build ./...` and `go vet ./...` on darwin, linux and windows (`CGO_ENABLED=0`): exit status 0.
  * The guard package: exit status 0, every `TestModuleSafety` subtest passing.
  * `python3 scripts/check_records.py --check-all`: exit status 0.
  * markdownlint-cli2 v0.23.2 over the repository's 49 Markdown files: 0 issues.
* **The commit groups** (complete and disjoint over the tree's 47 status entries: the 46 the reviewer counted, plus
  `README.md` from P10; the two untracked directories hold 10 and 13 files):

```text
  GROUP 1 (3 entries)
     M docs/README.md
    ?? docs/decisions/0010-MADR-make-tests-unable-to-signal-real-processes.md
    ?? docs/decisions/0010-PLAN-make-tests-unable-to-signal-real-processes.md
  GROUP 2 (2 entries)
     M internal/db/check_badger_test.go
     M internal/db/cleanup_test.go
  GROUP 3 (2 entries)
     M internal/telemetry/udp_server.go
    ?? internal/telemetry/udp_server_stop_test.go
  GROUP 4 (40 entries)
     M CLAUDE.md
     M README.md
     M cmd/mcp-server-magictools/infra_unix.go
     M cmd/mcp-server-magictools/infra_windows.go
     M cmd/mcp-server-magictools/main.go
     M cmd/mcp-server-magictools/service.go
     D cmd/mcp-server-magictools/service_extra_test.go
     M cmd/mcp-server-magictools/service_hardening_test.go
     M docs/guides/operations-and-security.md
     M internal/client/connector.go
     M internal/client/health_monitor_test.go
     M internal/client/lifecycle.go
     M internal/client/manager.go
     M internal/client/manager_extra_test.go
     M internal/client/process_linux.go
     M internal/client/process_reaper.go
     M internal/client/process_reaper_test.go
     M internal/client/process_unix.go
     M internal/client/process_windows.go
     D internal/client/reaper_harvest_unix.go
     D internal/client/reaper_harvest_windows.go
    ?? docs/guides/testing-and-process-safety.md
    ?? internal/client/escalate.go
    ?? internal/client/escalate_test.go
    ?? internal/client/fake_process_table_test.go
    ?? internal/client/own_child.go
    ?? internal/client/own_child_unix.go
    ?? internal/client/own_child_unix_test.go
    ?? internal/client/owner_stamp.go
    ?? internal/client/owner_stamp_test.go
    ?? internal/client/process_reaper_seam_test.go
    ?? internal/client/process_table.go
    ?? internal/client/process_table_attach_test.go
    ?? internal/client/reap_hazard_test.go
    ?? internal/client/reap_helpers_test.go
    ?? internal/client/reap_rules.go
    ?? internal/client/reap_rules_test.go
    ?? internal/client/stamp_environment_test.go
    ?? internal/proctable/
    ?? internal/testsafety/
```

* **Each commit is a sound tree by itself.** Every check below ran on a scratch tree of HEAD plus only the groups
  named, with only vet, build and the parse-only guard run:
  * HEAD + (2): `go vet ./internal/db/`, the same with `-tags debuglive`, and `go build ./...` all exit 0 on darwin,
    linux and windows. The two files change only how the S4 test chooses its stale pid (a reaped child of its own,
    Windows a non-multiple of four) and put the debug harness behind `-tags debuglive`. They name nothing that group
    (4) adds.
  * HEAD + (3): `go vet ./internal/telemetry/` and `go build ./...` exit 0 on the three GOOS. `udp_server.go`'s stop
    and its test stand alone.
  * HEAD + (2) + (3): `go vet ./...` exits 0 on the three GOOS.
  * **The guard in group (4) does not depend on (2) or (3).** HEAD + (1) + (4) without (2) and (3), with (3) only, and
    with (2) only: the guard exits 0 with no finding each time, and `go vet ./...` exits 0. HEAD's
    `internal/db/cleanup_test.go` uses `cleanupStaleProcess` and `.Kill` as many times as the allowlist records (3
    and 2).
* **Docs rows.** `docs/README.md` already holds 0010's rows (the MADR `accepted`, this plan `in-progress`) and the
  testing guide's "I want to" row. 0011's rows are there with the PLAN `implemented`. No row is added in P10.
* **Completion criteria, as of this entry:**

  | # | Criterion | Status |
  | :--- | :--- | :--- |
  | 1 | every phase done | met once the commit lands: P10 is done, P11's checks pass, and staging and the commit are the coordinator's |
  | 2 | guard seen red, green, no skipped subtest | met |
  | 3 | the tests on the fake table; no real table in a test; R1 to R4 | met |
  | 4 | the UDP stop test seen failing, then passing under `-race` | met |
  | 5 | P8's findings closed | met: the reviewer's confirmation |
  | 6 | P9 passed with every decoy alive and the panic seen | met |
  | 7 | docs steps done; no interim rule in `CLAUDE.md`; records and markdownlint pass | met |
  | 8 | records staged; status `complete` only after the commit | not yet: the coordinator stages and commits |

### 2026-11-19, closed: committed, pushed and green in CI; status `implemented`

* **Commit authority.** The owner changed it on 2026-11-19, in these words: "you stage then let me know where and when
  to commit and push". The coordinator no longer commits or pushes; the owner does, on the coordinator's word. That is
  why the commits below are the owner's, and why this close-out is staged and not committed.
* **The coordinator's gate on the final tree** (47 entries, after P10).
  * On scratch copies on the macOS machine:
    * `go build ./...`: 0.
    * `go vet ./...` for darwin, linux and windows: 0.
    * golangci-lint v2.15.0 for darwin and linux: 0 issues. Windows has 28, all in five files this record does not
      touch: `service_windows.go` 16, `cleanup_windows.go` 6, `filelock_windows.go` 3, `init_windows.go` 2,
      `io_helpers.go` 1. CI does not lint Windows.
    * gofmt: clean. `go mod tidy -diff`: 0. The record check `--check-all`: 0. The parse-only guard test: 0.
    * No identifier in the whole change.
  * On the Linux machine, after the listing ("no marked process and no magictools process on this host") and with pid
    999999 not alive: `go test -race -count=1 -timeout 40m ./...` exit 0, 21 packages `ok`.
  * The coordinator's own slip, recorded as the coordinator's: the gate's first Linux vet and lint steps ran with cgo
    left on while cross-compiling on the macOS machine, and failed inside `runtime/cgo`. Re-run with
    `CGO_ENABLED=0`, both exited 0.
* **Each planned commit was checked as a tree by itself.** HEAD plus the records alone fails the record check:
  `docs/README.md:57` links `guides/testing-and-process-safety.md`, which is in the main group. So the records were
  committed last. HEAD plus P6, and HEAD plus P6 and P6a, vet and build clean.
* **The owner's commits on `main`, in order.**
  * `36656f0`: P6, the two `internal/db` test files.
  * `8809c8e`: P6a, the UDP stop fix and its test.
  * `7300c64`: the main change, 61 files.
  * `943c7e1`: the two 0010 records and `docs/README.md`.

  The coordinator compared the pushed tree with the tree its gate ran on: identical. The tree was clean after.
* **CI on `943c7e1`**, run 44619523927, attempt 1: success.
  * `Go (linux/amd64; build on tag)` ran 17 steps, `Go (windows/amd64)` 10 steps and `Go (darwin/arm64)` 10 steps. The
    smoke job and the two publish jobs were skipped (no tag).
  * The Windows job's test step was `go test ./...`, without `-v`. Its log, read with `gh run view --log` for that job,
    shows both packages run, not cached:
    `ok  github.com/maccavelli/mcp-server-magictools/internal/client  21.492s` and
    `ok  github.com/maccavelli/mcp-server-magictools/internal/proctable  1.947s` (also `internal/db` 30.031s,
    `internal/telemetry` 0.550s, `internal/testsafety` 0.696s).
  * This is the first execution of this record's Windows code paths, and of `internal/proctable`'s tests on hosted
    macOS and Windows runners.
  * The log has no per-test lines. By the code, `TestOSTableEscalationKillsTermIgnoringChild` skips on Windows
    (`internal/proctable/table_test.go:228-229`), and no other test of the package has a Windows skip. So the
    package's `ok` means `TestOSTableChildRoundTrip` passed there: the environment read of its own child, the
    wrong-token refusals, and `Signal` through `taskkill /T /F` of its own child.
* **Still not verified on Windows:**
  * whether `taskkill /T` follows stale parent pids, so that a tree kill could reach a process whose recorded parent
    pid was reused;
  * the escalation path (unix-only by design);
  * the registry's own-child stop on Windows (`process_windows.go`), whose test is unix-only;
  * any per-test outcome, since the log is not verbose.
* **Completion criteria, checked against this evidence:**

  | # | Criterion | Status |
  | :--- | :--- | :--- |
  | 1 | every phase done, or named as skipped by an owner's ruling | met: P0 to P11 done; the commits are the owner's under the changed authority |
  | 2 | the guard seen red, now green, no skipped subtest | met: green in the gate and in CI on three platforms |
  | 3 | the tests on the fake table; no real table in a test; R1 to R4 | met |
  | 4 | the UDP stop test seen failing, then passing under `-race` | met: P6a, P9, and the gate's race run |
  | 5 | P8's findings closed | met: the reviewer's confirmation |
  | 6 | P9 passed with every decoy alive and the panic seen; the owner's confirmation recorded | met |
  | 7 | docs steps done; no interim rule in `CLAUDE.md`; records and markdownlint pass | met |
  | 8 | committed, then status set after the commit; the MADR's questions answered with labels | met by this close-out, after `943c7e1` and its green CI |

* **Status.** This plan becomes `implemented`, this repository's value (its record check rejects `complete`), and its
  `docs/README.md` row follows. The MADR stays `accepted`, as every closed pair here does.
