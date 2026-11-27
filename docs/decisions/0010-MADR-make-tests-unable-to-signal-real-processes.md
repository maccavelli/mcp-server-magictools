---
status: accepted
date: 2026-11-18
decision-makers: repository owner
consulted: "a read-only investigation of the signalling sites and of the live service's own log (tables and extracts held outside the repository); this repository at 3443524, read only; github.com/shirou/gopsutil/v4 v4.26.3 module source (read only); the owner's rulings of 2026-11-18, relayed by the coordinator (the decision to fix, the answers to the four open questions, and the third ruling on the design phase's five questions and the Linux host); the design phase P3 (2026-11-18, read only: gopsutil v4.26.3 source, the shared library mcplib and the sub-server repositories)"
informed: "the cross-repository coordinator"
---

# Make this module's tests unable to enumerate or signal a real process

## Context and Problem Statement

Three tests in `internal/client` run the orphan reaper against the real machine. The reaper scans every process the
user owns and sends SIGTERM, then SIGKILL, to any that carries the environment marker the orchestrator stamps on the
sub-servers it spawns. A developer's `go test ./internal/client/...`, or `go test ./...`, therefore signals the
sub-servers of the live service on the same machine. No test of this module is allowed to do that, and nothing stops
one from doing it.

This record decides to fix that in code, chooses the seam (the owner's answer to Q1, below) and sets the order in
which the fix is built and verified without ever running the hazard.

### Decided (the owner, 2026-11-18, relayed by the coordinator; the label is quoted only as a label)

The question put to the owner was how magictools testing should proceed given this hazard. The owner chose the option
labelled "Fix the hazard first, under its own short record; until then run only the safe packages here (Recommended)".
As put to the owner: a record puts the process scan and the signalling behind a seam so that no test ever enumerates or
signals real processes; until it lands, the handler, database and command packages run on this machine as before, and
no whole-module run and no `internal/client` run happens on any machine that has the live server; the whole-module gates
of the other plans wait for the fix. This record is that record. The decision to fix is `accepted`. The choice of seam
was `proposed` until the second ruling, below, answered the four open questions.

### Decided, second ruling: the answers to Q1 to Q4 (the owner, 2026-11-18, relayed by the coordinator; each label quoted only as a label)

An addition of 2026-11-18, made after this record was drafted as `accepted` for the decision to fix and open on the
seam. Nothing above this heading changes. The record's own recommendation is marked where the owner chose otherwise.

1. **Q1, which design:** "That, plus a per-instance marker so two orchestrators cannot reap each other's children".
   As put to the owner: the interface held by the registry, with a real constructor that refuses to run inside a test
   binary, fakes for tests and the parse-only guard (A+G below), plus the per-instance marker (C below); the marker
   also fixes the product hazard in which a second copy of the server reaps the live server's sub-servers; it changes
   which processes are reaped in production, and protects nothing until the owner's running server is rebuilt with the
   new marker; a larger change with its own risk. **This is not the option this record recommended** (A+G, tests
   only); the owner chose the wider one, A+G+C.
2. **Q2, the pid-file cleanup (S4):** "A record of its own, straight after this one (Recommended)". As put: this record
   stays short; the identity check on the recorded pid is the next record, not part of this one.
3. **Q3, where the first run and the owed whole-module gates happen:** "On the Linux machine, after you confirm no live
   magictools server runs there (Recommended)". As put: the first run of the `internal/client` tests and the
   whole-module gates owed by the other plans run on the Linux host, after the owner confirms that no live server
   runs there. **That confirmation has not been given.** Nothing runs on that host until it is.
   *Note, 2026-11-18:* the owner gave it the same day ("No live magictools server there: use it (Recommended)",
   relayed by the coordinator; the third ruling, item 6).
4. **Q4, the side findings:** "Fix the UDP double close here too". As put: a small, real product fix that stops the
   panic at every shutdown; it adds a production change and a test to this record. The dead kill helper and `make run`
   are recorded for later records.

**The coordinator's reading,** recorded as the coordinator's and told to the owner: these four answers approve the
plan to proceed phase by phase, each committed by the coordinator after its own gate; the design phase (P3) still stops
for the coordinator's approval before any code; the independent review (P8) precedes any first execution; P9 waits for
the owner's confirmation about the host.

### Decided, third ruling: the design phase's five questions and the Linux host (the owner, 2026-11-18, relayed by the coordinator; each label quoted only as a label)

The coordinator approved the design of phase P3 (below, "Design as approved") as the basis for the build. The owner
answered its five questions and one question about the Linux host. Each label is quoted only as a label; what was
asked is put beside it in my own words.

1. **P3 Q1, the old constant and R3's first bullet:** "Keep `=true` for the sub-servers, add a new owner stamp beside it,
   and amend the rule (Recommended)". As put: new binaries decide ownership only by the new stamp and never touch each
   other's sub-servers; the gap that remains is that an old binary can still reap a new binary's sub-servers, exactly
   as it can today, until the owner has rebuilt and reinstalled the server; no sub-server repository changes. R3's
   first bullet is amended accordingly (below).
2. **P3 Q2, what an instance is:** "The store directory is the instance (Recommended)". As put: the lineage is the
   canonical store directory (plus the Linux pid namespace); a second process on the same store is the same service.
3. **P3 Q3, processes stamped only with the old constant:** "Never touch them; the guide gives a one-time read-only
   check (Recommended)".
4. **P3 Q4, S2's look-alike rules (same executable, the `mcp-server-<name>` suffix, the interpreter command line):**
   "Remove them (Recommended)". As put: only stamped processes of the same instance are replaced; the cost is that a
   copy of a sub-server started outside magictools is no longer killed before magictools starts its own, so the two
   may run side by side.
5. **P3 Q5, the child-only test of the real process table on the macOS machine:** "Yes, that one test package only,
   after the Linux run and the review (Recommended)".
6. **The Linux machine, asked directly:** "No live magictools server there: use it (Recommended)". As put: before every
   run there, a read-only listing still checks that no process carries the orchestrator's marker, and the run stops if
   one does; the client-package tests run only on that machine until the fix is verified. This is the confirmation the
   plan's P9 waited for (Q3 of the second ruling), dated 2026-11-18.

**Placement note, 2026-11-18.** The index holds another record's staged change (0009's). This pair therefore stays
untracked and uncommitted until that change is committed, and the coordinator commits this pair right after. The guard
(P2) is not committed or pushed by itself: it is red by design until the phases that fix what it names (P4 and P5), and
is committed with them.

### What exists today

The reaper has three entry points (S1, S2, S4 below) and a fourth that only the server's own start-up reaches (S5). The
dangerous one is S1, `PruneOrphans`.

* **Selection.** It lists every pid on the machine (E1), keeps those that are not this process, its parent, or a
  descendant of either or of a registry-managed pid (E6), reads each one's environment (E2a), and treats a process whose
  environment holds exactly `MCP_ORCHESTRATOR_OWNED=true` (E2, E7, E8) as an orphan. The constant is the same in every
  orchestrator, in the live service and in a test's registry alike. There is no flag, environment variable, build tag or
  ownership check that turns it off. Operating-system permission, the same user, is the only limit.
* **Signal.** `killPIDGroup` sends SIGTERM to the process group and to the pid, then SIGKILL to both after 2 seconds if
  the pid still answers signal 0 (E10, E11, E12, E12a). On Windows `taskkill /T /F` (E13).
* **Who stamps the marker.** The live service stamps it on every sub-server it spawns (E9), so those sub-servers are
  exactly what a test's reaper selects: they are not self, not the test's parent, and not descendants of the test.
* **Who reaches it.** Three tests call it for real: `TestExtraManagerMethods` directly (E16), `TestPruneOrphansEnumeratesExactlyOnce`
  directly with the parent table stubbed to empty, so that nothing at all counts as a descendant (E17, E17a), and
  `TestHealthMonitor_Lifecycle` through `StartHealthMonitor`, whose first tick runs immediately and calls `PruneOrphans`
  (E18, E14, E15). A throwaway `HOME` does not help: the scan is by process, not by path.

**Added by the design phase (P3), 2026-11-18.** Facts the inventory did not carry, each checked by reading (lines at
`3443524` unless another module is named):

* **F1. `MCP_ORCHESTRATOR_OWNED=true` is the sub-servers' own signal that they run under the orchestrator.** The shared
  library mcplib reads it in `IsOrchestratorOwned` (`orchestrator.go:19-21`), called from seven sub-server repositories
  (brainstorm, go-modernizer, magicdev, duckduckgo, evolve-plan, magicskills, socratic-thinker) and from the library's
  streaming, fast-path and hardening code; this repository's copy reads it at `internal/util/hfsc/dispatch.go:57`. One
  example of what it gates: the duckduckgo server takes its own single-instance lock only when the flag is absent
  (its `cmd/mcp-server-duckduckgo/serve.go:31`), so under the orchestrator it relies on S2. The flag cannot carry an
  owner and cannot be removed or revalued here without changing every sub-server; so the old reaper's exact test (E2)
  and the old sweep's test (E44 to E46) match every child of a new binary too.
* **F2. In the installed service S1 spares every process.** `isLegitimateDescendant` counts a process whose ancestry
  reaches its own parent as legitimate (`internal/client/process_reaper.go:67`). The launchd job runs the binary
  directly (`cmd/mcp-server-magictools/service.go:458-461`) and the investigation's process listing shows the live
  service's parent is pid 1, to which orphans are re-parented; under systemd `--user` the parent is the user manager,
  which adopts orphans (systemd's documented behaviour, not run). So the orphans of a crashed run of the service are
  removed today by S2 (by name, when that server is spawned again) and, on Linux and Windows, by S5; S1 reaps only when
  the orchestrator was started by a terminal, an IDE or a test.
* **F3. S2 has no descendant or ownership check** (`internal/client/process_reaper.go:200-275`). A second orchestrator
  harvests the live service's sub-server of the same name, or with the same executable, each time it spawns that
  server (E22). The product form of the hazard is S2 as well as S1.
* **F4. S3 can signal a pid after the child was reaped.** `DisconnectServer` clears `s.Process` and
  `closeSubServerSnapshot` waits up to 5 seconds for the session to close before the kill
  (`internal/client/connector.go:35-39`, `:144-149`); closing the session usually makes the child exit and the Wait
  goroutine reap it (`internal/client/lifecycle.go:552-590`); the escalation then polls the raw pid for 2 seconds and
  sends SIGKILL (`internal/client/process_unix.go:33-45`). On Windows `taskkill` by pid has the same window.
* **F5. gopsutil's Linux start time is not a stable identity.** It is computed from the boot time (`/proc/stat`
  `btime`, or, under LXC, now minus uptime), cached per reading process (gopsutil `process/process_linux.go:1089-1094`,
  `internal/common/common_linux.go:48-100`), so two processes can compute different values for one process. The
  process identity on Linux is the raw start time in clock ticks, field 22 of `/proc/<pid>/stat`. darwin
  (`p_starttime`, gopsutil `process_darwin.go:95`) and Windows (creation time) read immutable sources.
* **F6. On Windows the descent table is not one enumeration.** `osParentMap` calls `Ppid` per process
  (`internal/client/process_reaper.go:42-47`) and each call takes a toolhelp snapshot there (gopsutil
  `process_windows.go:305-321`, `:879-901`). MADR 0003's claim holds on Linux and darwin. Recorded, not fixed.
* **F7. `MAGIC_TOOLS_PEER_ID` has no reader but S5** in any Go repository of the owner's checkouts (third-party
  sub-servers not checked).
* **F8. `TestHealthMonitor_Lifecycle` also reads the operator's real `servers.yaml`** through `PollConfigChanges`
  (`internal/client/health_monitor.go:371`). It signals nothing; its rewrite (P5) runs under a throwaway `HOME`.

### The two incidents, stated plainly, and what the evidence cannot show

The live service's own log records, on 2026-11-17 at 13:37:55 and on 2026-11-18 at 08:33:16, the two sub-servers it keeps
pinned (`ddg-search` and `recall`) exiting within a fraction of a second of each other. In both cases the log has no
service-side line before the exits (no eviction, restart, disconnect or shutdown): the service reports "process exited"
and nothing that caused it. On the second date one exit status is `signal: terminated`; on the first both are
status-zero exits, which is also how a server that handles SIGTERM exits. An earlier group death on 2026-09-04 at
16:30:34 ends with one server logging `signal: killed` two seconds after the others exited, which is what S1's TERM,
wait 2 seconds, KILL escalation looks like.

The coordinator established afterwards, from the same log and the session's own run history, that both deaths fall
inside a whole-module test run of this module made by this session: the second was the coordinator's own gate, whose
whole-module race run ended at 08:33:29, thirteen seconds after the deaths. On each date the service logs "process
exited" first and no eviction, offline or respawn line follows. After the 13:37:55 deaths on the 17th no sub-server was
alive until the service was restarted the next morning, so the later whole-module runs of that day had nothing left to
kill; that is consistent with the first event being the only one that day, and says nothing against the hazard.

What this does not establish, stated so nobody builds on more than is there:

* The log records no sender. A SIGTERM from S1 is consistent with every one of these lines. So is a SIGTERM from S2
  (also two seconds), from the operator, or from the service manager. The evidence is circumstantial: two events, each
  inside a whole-module run, each with the signature of an outside kill, and no other cause found.
* The timing of the test runs comes from the session's run history, not from a file in the repository; this record
  does not carry it, and I did not re-derive it.
* The code does establish the capability: the three tests above signal any marked process that is not theirs.

An earlier incident of the same family is already written down: the repository assessment records that one ad hoc
`serve` probe invoked the stale-PID cleanup (S4) against the recorded process and the service manager restarted it
(E61). That one is S4, not S1; both are in the inventory.

### Evidence index

Every row is a claim, the `path:line` that carries it at commit `3443524`, and a quotation from that line. Lines are
as they are at that commit, not in the working tree (the working tree differs from it in `internal/db/badger.go`, which
another plan has staged). Each row was checked by reading `git show 3443524:<path>` and looking for the quotation on the
cited line; a row whose quotation is not on its line fails the check.

| ID | Claim | Where | Quotation |
| :--- | :--- | :--- | :--- |
| E1 | S1 lists every pid on the machine | `internal/client/process_reaper.go:90` | `process.Pids()` |
| E2 | S1 treats a process whose environment holds the constant marker as an orphan | `internal/client/process_reaper.go:132` | `slices.Contains(env, EnvManaged` |
| E2a | S1 reads each candidate's environment | `internal/client/process_reaper.go:127` | `util.GetProcessEnviron(int(pid))` |
| E3 | S1 signals through `killPIDGroup` | `internal/client/process_reaper.go:140` | `killPIDGroup(int(pid))` |
| E4 | the descent table comes from a package variable | `internal/client/process_reaper.go:98` | `parents := parentMap()` |
| E5 | that variable is assigned once, as a package-level function value | `internal/client/process_reaper.go:27` | `var parentMap = osParentMap` |
| E6 | descent is judged against self, parent and managed pids only | `internal/client/process_reaper.go:118` | `isLegitimateDescendant(pid, parents` |
| E7 | the marker name | `internal/client/lifecycle.go:53` | `"MCP_ORCHESTRATOR_OWNED"` |
| E8 | the marker value is the constant `true` | `internal/client/lifecycle.go:54` | `"true"` |
| E9 | the spawn path stamps every sub-server with it | `internal/client/lifecycle.go:721` | `EnvManaged, EnvManagedValue` |
| E10 | S1 on darwin and the BSDs: SIGTERM to the group | `internal/client/process_unix.go:27` | `syscall.Kill(-pid, syscall.SIGTERM)` |
| E11 | S1 on darwin and the BSDs: SIGKILL to the group after the grace period | `internal/client/process_unix.go:43` | `syscall.Kill(-pid, syscall.SIGKILL)` |
| E12 | S1 on Linux: SIGTERM to the group | `internal/client/process_linux.go:29` | `syscall.Kill(-pid, syscall.SIGTERM)` |
| E12a | S1 on Linux: SIGKILL to the group after the grace period | `internal/client/process_linux.go:45` | `syscall.Kill(-pid, syscall.SIGKILL)` |
| E13 | S1 on Windows | `internal/client/process_windows.go:26` | `taskkill` |
| E14 | the health monitor's tick calls S1 | `internal/client/health_monitor.go:52` | `m.PruneOrphans()` |
| E15 | the first tick runs at once, not after an interval | `internal/client/health_monitor.go:78` | `runTick(time.Now())` |
| E16 | test 1 reaches S1 | `internal/client/manager_extra_test.go:36` | `wr.PruneOrphans()` |
| E17 | test 2 reaches S1 | `internal/client/process_reaper_test.go:189` | `m.PruneOrphans()` |
| E17a | test 2 stubs the descent table to empty | `internal/client/process_reaper_test.go:182` | `parentMap = func()` |
| E18 | test 3 starts the monitor at a 10 ms interval | `internal/client/health_monitor_test.go:34` | `go m.StartHealthMonitor` |
| E19 | the product starts the monitor | `cmd/mcp-server-magictools/main.go:1100` | `app.reg.StartHealthMonitor` |
| E20 | the product prunes before start | `cmd/mcp-server-magictools/main.go:284` | `app.reg.PruneOrphans()` |
| E21 | the product prunes again after boot | `cmd/mcp-server-magictools/main.go:1495` | `app.reg.PruneOrphans()` |
| E22 | S2 runs on every spawn | `internal/client/lifecycle.go:482` | `m.enforceSingleInstance(ctx, name, command, args)` |
| E23 | S2 selects by the server-name stamp | `internal/client/process_reaper.go:226` | `slices.Contains(env, targetStamp)` |
| E24 | S2 selects by executable equal to the command | `internal/client/process_reaper.go:254` | `exe == command` |
| E25 | S2 selects by executable suffix | `internal/client/process_reaper.go:261` | `strings.HasSuffix(exe` |
| E26 | S2 also enumerates every pid | `internal/client/process_reaper.go:201` | `process.Pids()` |
| E27 | S2 reads a recorded pid from a file | `internal/client/process_reaper.go:188` | `filepath.Join(m.PIDDir, name+".pid")` |
| E28 | S2 on unix: SIGTERM to the group | `internal/client/reaper_harvest_unix.go:27` | `syscall.Kill(-pid, syscall.SIGTERM)` |
| E29 | S2 on unix: SIGKILL to the group | `internal/client/reaper_harvest_unix.go:49` | `syscall.Kill(-pid, syscall.SIGKILL)` |
| E30 | S2 on Windows | `internal/client/reaper_harvest_windows.go:18` | `taskkill` |
| E31 | handler tests give the registry a fake server name | `internal/handler/common_test.go:31` | `{Name: "test-server"` |
| E32 | S3 kills the registry's own child after a failed session | `internal/client/lifecycle.go:241` | `killProcessGroup(cmd)` |
| E33 | S3 kills the registry's own child on disconnect | `internal/client/connector.go:149` | `killProcessGroup(process)` |
| E34 | `NewStore` runs the stale-pid cleanup | `internal/db/badger.go:474` | `cleanupStaleProcess(path)` |
| E35 | the cleanup runs before the lock is taken | `internal/db/badger.go:478` | `AcquireLock(path)` |
| E36 | the store writes its own pid file | `internal/db/badger.go:516` | `server.pid` |
| E37 | S4 sends SIGTERM to the recorded pid | `internal/db/cleanup_unix.go:65` | `process.Signal(syscall.SIGTERM)` |
| E38 | S4 escalates to SIGKILL | `internal/db/cleanup_unix.go:87` | `process.Signal(syscall.SIGKILL)` |
| E39 | S4 on Windows kills the recorded pid with no liveness test | `internal/db/cleanup_windows.go:60` | `taskkill` |
| E40 | the S4 test writes pid 999999 | `internal/db/cleanup_test.go:27` | `999999` |
| E41 | the S4 test starts its own child | `internal/db/cleanup_test.go:33` | `exec.Command("sleep", "10")` |
| E42 | the debug harness opens whatever directory an environment variable names | `internal/db/check_badger_test.go:12` | `MAGICTOOLS_DEBUG_DB` |
| E43 | S5 lists every pid | `cmd/mcp-server-magictools/infra_unix.go:79` | `process.Pids()` |
| E44 | S5 reads environments through gopsutil | `cmd/mcp-server-magictools/infra_unix.go:98` | `p.Environ()` |
| E45 | S5 spares a marked process only when its direct parent's name contains the product name | `cmd/mcp-server-magictools/infra_unix.go:116` | `strings.Contains(pName, "magictools")` |
| E46 | S5 sends SIGKILL to the group | `cmd/mcp-server-magictools/infra_unix.go:122` | `syscall.Kill(-int(pid), syscall.SIGKILL)` |
| E47 | S5 is called only from the server's start | `cmd/mcp-server-magictools/main.go:139` | `InitialProcessSweep()` |
| E48 | S5 on Windows | `cmd/mcp-server-magictools/infra_windows.go:86` | `taskkill` |
| E49 | S6: `service stop` on macOS | `cmd/mcp-server-magictools/service.go:1001` | `"kill", "SIGTERM"` |
| E50 | S6: `service start` on macOS kills a running instance | `cmd/mcp-server-magictools/service.go:952` | `kickstart` |
| E51 | S7: SIGKILL after a wait | `cmd/mcp-server-magictools/service.go:860` | `signalOrWarn(p, os.Kill)` |
| E52 | S7: the stale-service killer | `cmd/mcp-server-magictools/service.go:877` | `func killStaleProcess` |
| E53 | its only caller is a test that skips itself first | `cmd/mcp-server-magictools/service_extra_test.go:37` | `killStaleProcess(999999)` |
| E54 | that test skips before doing anything | `cmd/mcp-server-magictools/service_extra_test.go:10` | `t.Skip("Skipping destructive service test")` |
| E55 | S8: the updater drives `launchctl` | `cmd/mcp-server-magictools/update_service.go:201` | `launchctl` |
| E56 | the default database path is the live service's own | `cmd/mcp-server-magictools/root.go:40` | `DBPath = config.DefaultDataPath()` |
| E57 | a bare run starts `serve` | `cmd/mcp-server-magictools/serve.go:35` | `Make serve the default` |
| E58 | `make run` builds and executes the binary | `Makefile:76` | `run: build` |
| E59 | `make test` is the whole module | `Makefile:60` | `go test -v ./...` |
| E60 | a test of the application constructor passes an empty database path, so it skips before any process work | `cmd/mcp-server-magictools/cmd_extra_test.go:12` | `NewOrchestratorApp(ctx, cancel, "", ""` |
| E61 | the earlier incident, in the repository assessment | `docs/guides/repository-assessment.md:22` | `stale-PID cleanup` |
| E62 | the operations guide says startup terminates a recorded stale process | `docs/guides/operations-and-security.md:62` | `Startup also reads PID state` |
| E63 | the house precedent: a guard that fails a test binary that touched the operator's real state | `internal/config/main_test.go:26` | `func TestMain` |
| E64 | on darwin the environment is read from the kernel's argument block | `internal/util/environ_darwin.go:23` | `kern.procargs2` |
| E65 | CI runs the whole module on hosted runners | `.github/workflows/ci.yml:84` | `go test ./...` |
| E66 | the UDP server's `Stop` closes its channel with no guard | `internal/telemetry/udp_server.go:114` | `close(s.cancelChan)` |
| E67 | the first caller of that `Stop`: the application's `Stop` | `cmd/mcp-server-magictools/main.go:351` | `app.udpSrv.Stop()` |
| E68 | the second caller: the graceful-shutdown path | `cmd/mcp-server-magictools/main.go:1049` | `app.udpSrv.Stop()` |
| E69 | `Start` defers the application's `Stop` | `cmd/mcp-server-magictools/main.go:369` | `defer app.Stop()` |
| E70 | the signal handler runs the graceful shutdown | `cmd/mcp-server-magictools/main.go:1004` | `app.performGracefulShutdown()` |
| E71 | the process-environment source on Linux is gopsutil's | `internal/util/environ_notdarwin.go:16` | `p.Environ()` |
| E72 | the handler reaches `Registry.Connect` (and so S2) from the sync handlers | `internal/handler/sync_handlers.go:192` | `h.Registry.Connect(` |
| E73 | and from the list-tools handler | `internal/handler/list_tools_handler.go:151` | `h.Registry.Connect(` |
| E74 | and from the proxy service | `internal/handler/proxy_service.go:343` | `Registry.Connect(` |
| E75 | the one existing UDP server test binds a loopback socket and stops once | `internal/telemetry/udp_server_test.go:8` | `func TestUDPServerLifecycle` |
| E76 | the UDP server's cancel channel is created at construction and never guarded | `internal/telemetry/udp_server.go:78` | `cancelChan: make(chan struct{})` |
| E77 | macOS and Windows CI jobs run the whole module with no skip | `.github/workflows/ci.yml:199` | `go test ./...` |

One fact rests on module source and not on this repository: gopsutil v4.26.3 (file `process/process_bsd.go`, line 63),
`EnvironWithContext`, returns `ErrNotImplementedError` on darwin, so S5's per-process `p.Environ()` fails for every
pid there and the sweep reaps nothing on darwin; on Linux it reads the process's `environ` file and is live.

### The inventory, verified

Every site where the module enumerates processes it did not start, or signals a pid it did not start in that call. `Reached
by` is by reading each `_test.go` in the module. "Safe today" is about running the module's tests on a machine that has
the live service.

| Site | Selects by | Signal | Reached by tests | Safe today |
| :--- | :--- | :--- | :--- | :--- |
| S1 `PruneOrphans` then `killPIDGroup` (E1 to E13) | every pid; environment holds the constant marker; not self, parent or descendant | TERM to group and pid, KILL after 2 s; `taskkill /T /F` on Windows | `internal/client`: TestExtraManagerMethods, TestPruneOrphansEnumeratesExactlyOnce, TestHealthMonitor_Lifecycle. No other package: `internal/handler`, `internal/db` and `cmd` never call it (the product calls it from `main.go`, which no test enters) | No |
| S2 `enforceSingleInstance`, `lookupSystemProcesses`, `harvestProcess` (E22 to E30) | every pid; the server-name stamp, or executable equal to the command, or executable suffix `mcp-server-<name>`, or interpreter plus command line; plus the pid in `<PIDDir>/<name>.pid` | TERM to group and pid, KILL after 2 s; `taskkill` on Windows | `internal/client`: every test that calls `Connect` or `Boot` (`manager_test.go`, `client_exhaustive_test.go`, `manager_extra_test.go`), and `TestEnforceSingleInstanceSkipsSelf`. `internal/handler`: the Wake, Reload and Sync tests (`sync_handlers_test.go`) and any handler test that reaches `Registry.Connect` (E72 to E74), through `newTestHandler` (E31) | Not by design. Every name a test uses is fake (`self`, `test-server`, `failing-server`, `deadlock-test`, `restart-race-test`, `fake`, `test`), so it cannot name a live server. It scans every process, so two test runs at once that share the name `test-server` can signal each other's children; and a process whose executable equals a test's command (E24) is signalled |
| S3 `killProcessGroup` (E32, E33) | the `exec.Cmd` the registry itself started, by its own pid | TERM, then KILL after 2 s | the same Connect and Disconnect tests | Yes: own child. A pid reused after the child was reaped is possible in theory, not observed |
| S4 `cleanupStaleProcess` (E34 to E39) | the pid in `<store dir>/server.pid`, if it answers signal 0 | TERM, 3 s, KILL; on Windows `taskkill /F` with no liveness test | every `NewStore` call, in about forty test files, each with a fresh temporary directory; `TestCleanupStaleProcess` writes pid 999999 and the pid of its own `sleep` child; `TestDebugLiveBadger` if the variable is set | In practice yes. Two open edges: pid 999999 is dead where the pid limit is 99998 but is not guaranteed dead where it can be 4194304; and the debug harness, if pointed at a live directory, would signal the live service (E42). No check that the pid is a magictools process or is not the caller |
| S5 `InitialProcessSweep` (E43 to E48) | every pid; marker value or a `MAGIC_TOOLS_PEER_ID=` prefix; spared when its direct parent's name contains the product name | SIGKILL to group and pid; `taskkill /T /F` on Windows | none (only `ServeFunc`) | No test reaches it. A no-op on darwin (gopsutil), live on Linux. Its parent-name guard spares a live service's direct children and not their children |
| S6 `service stop`, `start`, `restart`, `install`, `uninstall` (E49, E50) | the launchd job by its fixed label and the current user | `launchctl kill SIGTERM`, `kickstart -k`, `bootout` | none: `TestServiceFunctions` skips first (E54) | Yes |
| S7 `waitForProcessExit`, `killStaleProcess` (E51 to E54) | the pid in `service.state` | Kill, Interrupt | none: `killStaleProcess` is called only from the skipped test (E53); `waitForProcessExit` only from the three uninstall paths | Yes |
| S8 the updater's `serviceControl` (E55) | the installed service definition, which must name this executable | `launchctl bootout` and `bootstrap` | none: the tests give the updater stop and start functions | Yes |
| read-only probes | own registry children | signal 0, RSS and CPU reads | many | Yes |

Anything the investigation missed? The grep set was `syscall.Kill`, `.Kill()`, `.Signal(`, `process.Pids`, `gopsutil`,
`FindProcess`, and `exec.Command` of `kill`, `pkill`, `taskkill`, `launchctl`, over the module excluding the vendored
`internal/forks` tree, which holds none of them. It found no site beyond S1 to S8 and their Windows twins. Adjacent and
not a process signal: `internal/handler/execute_pipeline.go:868` runs `git checkout -- .` on a pipeline target and was
not investigated here.

The parse-only guard of P2 (`internal/testsafety`) reads the syntax tree instead of grepping, and found further uses
that the grep set did not list. All are read-only or service control and are classified in its allowlist: gopsutil
opened on the caller's own process (`internal/telemetry/os_metrics.go`) or on a pid the caller passes
(`internal/util/proc.go`); the environment readers (`internal/util/environ_darwin.go`, `environ_notdarwin.go`) that S1
and S5 use; a test that reads its own process environment (`internal/util/proc_test.go`); and `systemctl` calls in
`service.go`, `service_refresh.go` and `update_service.go` (S6 and S8 on Linux). None is reached by a test in a way that
signals another process.

## Decision Drivers

* **No test may enumerate or signal a real process.** Enumerate means list the machine's pids or read another process's
  environment or command line. Signal means send any signal to a pid the call did not start. A test may signal a pid it
  started (S3) and read its own children.
* **No flag to remember.** A bare `go test ./...`, `make test`, and a bare `go test ./internal/client/...` must be safe.
  A fix that is safe only with a build tag, an environment variable or a `-skip` list is a workaround.
* **Production behaviour is unchanged unless the owner chooses hardening.** The reaper still reaps orphans, by the same
  rule, in the shipped binary.
* **The tests of the reaper's logic stay.** The selection rule, the descent walk and the single-instance logic keep
  tests, over a fake process table.
* **Verifiable without running the hazard.** The fix is built, reviewed and first run in an order that never executes the
  three tests against a real process table on a machine that has the live service (see Confirmation).
* **No collision with the plans in flight.** 0009 changes `internal/db` (store files) and 0008 changes
  `internal/handler`; 0006 changes `go.mod`, `go.sum`, `Makefile`, the CI workflow and `README.md`. This record touches
  `internal/client`, one line of `cmd/mcp-server-magictools/main.go`, two test files of `internal/db`, a new
  `internal/testsafety` package, `CLAUDE.md` and new documentation, and no file of those three plans.
* **A decision the owner can answer.** The options are priced, and what is the minimum to lift the hazard is separated
  from what is hardening.

## Considered Options

* **A. A process-table seam.** The registry holds an interface for the process enumerator, the environment reader and
  the signaller of foreign pids. Production wiring gives it the OS implementation; a registry built without one is
  inert. Tests get a fake table. `parentMap` is the existing, partial version of this idea.
* **B. A "no real processes under test" default.** Under `testing.Testing()` the reaper's scan and foreign-pid signalling
  refuse unless a test opts in.
* **C. Scope the reaper's selection so it cannot match another instance's children.** The marker value names the
  instance (or the store directory, or the owner pid and start time) instead of the constant `true`, and a reaper
  selects only children of a dead or its own predecessor instance.
* **D. A build tag or an environment variable that disables the reaper.**
* **E. Do nothing in code; skip the three tests by name.**

A combination can be right. The ones considered: **A+G** (A with the structural default and a parse-only guard, G, that
fails if a test could reach the real implementation), **A+G+C** and **B alone**.

## Decision Outcome

Chosen option: **A+G+C**, the owner's answer to Q1 (second ruling, above): the process-table seam with its inert
zero value, its tripwire and the parse-only guard, plus the per-instance marker, because the seam is the only option that is
safe with no flag for every present and future test, and the marker removes the product hazard in which one orchestrator
reaps another's children. Added by the owner's answer to Q4: `UDPServer.Stop` becomes safe to call twice.

*Annotation, 2026-11-18.* This record, as drafted, recommended **A+G, tests only**, with C as a later record, because C
changes production selection and cannot be verified without running a reaper. The owner chose the wider option. The
reasons for the recommendation stand and are carried below as the risks of the choice: the verification of C is
by fake table and by reading until the first real run on the confirmed host, and the real selection runs for the first
time in the owner's service after it is rebuilt. The identity check on the S4 pid file (Q2) and moving S5's scan onto
the seam are not part of this record, except that S5 takes the new marker rule (below). The S4 check is the next record.
*Annotation, P3:* the approved design meets R6 by removing S5 rather than rewriting it (D6).

### What A+G is, precisely

1. **An interface in `internal/client`.** One small interface, owned by the registry, that offers: list pids; the
   parent table; one process's environment, executable and command line; terminate a foreign pid (S1's TERM, wait, KILL
   tree) and harvest a pid (S2). The exact surface is a design step of the plan, with two constraints: a fake needs no
   operating system, and no method signals a pid that was not returned by the same table.
2. **The zero value is inert.** A `WarmRegistry` built without a table, whether as a literal or through
   `NewWarmRegistry`, enumerates nothing and signals nothing: `PruneOrphans` and `enforceSingleInstance` return at once.
   The OS implementation is built by one exported constructor, which the product's `main.go` calls once and passes to
   the registry. `NewWarmRegistry`'s signature does not change.
3. **A tripwire as well.** That constructor panics when `testing.Testing()` is true, so a test that builds the real
   table fails at once, with a message, before it touches a process.
4. **S3 stays direct.** `killProcessGroup(cmd)` kills a child the registry started, from its own pid. It is not behind
   the interface; it must not route through the foreign-pid path.
5. **`parentMap` goes.** The package variable is replaced by the interface method; a test never assigns a package
   variable that a leaked goroutine can read.
6. **G, the guard.** A new package, `internal/testsafety`, whose test parses the module's source with `go/parser` and
   never imports the packages it checks, so that running it cannot start a process. It fails when (a) any `_test.go`
   references the real-table constructor, (b) any file calls a signalling or enumerating primitive (`syscall.Kill`,
   `Process.Signal`, `process.Pids`, `taskkill`, `launchctl`, and a test's direct call of `cleanupStaleProcess`, which has
   no fake) outside an allowlist of the inventory above, so a new site cannot appear unclassified, (c) any `_test.go`
   function calls `PruneOrphans`, `StartHealthMonitor`, `enforceSingleInstance`, `lookupSystemProcesses`,
   `harvestProcess` or `InitialProcessSweep` without constructing a fake table in the same function, and (d) the real-table constructor is called from non-test code anywhere but exactly one line of
   `cmd/mcp-server-magictools/main.go`: zero calls there (the reaper silently off) fails as surely as a second call.
   Rule (c) is a syntactic proxy; the structural backstop is items 2 and 3.
7. **Two small test changes.** `TestCleanupStaleProcess` derives its dead pid from a child it started and reaped,
   instead of 999999; `TestDebugLiveBadger` moves behind a build tag that must be given to run it, so that the
   default build never contains a harness that can be pointed at a live directory.

Production behaviour: the selection rules and the signals are the code they are today, moved behind the interface. One
line is added to start-up, which gives the registry its table. If that line were missing, the reaper would silently do
nothing in production; guard rule (d) gates it.

### What C adds, and the rules its design must meet

The plan's design phase (P3) designs the marker. This record states the rules the design must meet, and does not
design it. The marker is a stamp on the sub-servers an orchestrator spawns (E7 to E9); the reaper of S1 and the sweep
of S5 select by it.

* **R1, instances do not reap each other.** An orchestrator never signals a process stamped by another instance that is
  alive, wherever that instance's store directory is. Two orchestrators on one machine, with different databases or
  with one of them a copy built by a developer, leave each other's sub-servers alone.
* **R2, a restart still reaps its predecessor's orphans.** An instance that starts, or runs, on the same store as an
  instance that died still reaps the orphans that instance left behind. This is the reaper's purpose and it is kept.
* **R3, an old running service and a new binary coexist.** While the owner's running service still stamps the old
  constant (`MCP_ORCHESTRATOR_OWNED=true`) and a new binary runs beside it, neither may reap the other's children:
  * *(amended by the third ruling, P3 Q1; the replaced text is quoted at the end of this bullet)* the new owner stamp
    is not matched by the old code's tests: the old reaper's exact `MCP_ORCHESTRATOR_OWNED=true` (E2) and the old
    sweep's `MAGIC_TOOLS_PEER_ID=` prefix (E44 to E46). **The child as a whole still is matched**, because it must keep
    `MCP_ORCHESTRATOR_OWNED=true`, which every sub-server reads (F1); and an old binary's S2 still matches it by its
    server-name stamp and by its executable. **The remaining gap, plainly: an old binary can still reap a new binary's
    children, exactly as it can reap anything today, until no old binary runs** (that is, until the owner has rebuilt
    and reinstalled the server). In the owner's launchd service the old S1 reaps nothing (F2); its exposure is the old
    S2 and, on Linux and Windows, the old S5. *Replaced text, for the record:* "the new stamp must not be matched by
    the old code's tests. The old reaper matches the exact entry `MCP_ORCHESTRATOR_OWNED=true` (E2), and the old sweep
    matches that or any entry that begins `MAGIC_TOOLS_PEER_ID=` (E44 to E46, live on Linux only), spared only when the
    direct parent's name contains the product name";
  * the new code must not reap a process that carries the old constant, unless the design shows that its owner is gone
    (for example, that it has been reparented away from any orchestrator). Where it cannot show that, it leaves the
    process alone; the cost is that one start-up's worth of orphans from the old binary stays until removed by hand.
* **R4, a stamp the new code cannot attribute is not reaped.** A missing, malformed or foreign value is left alone.
* **R5, the rules are tested over data.** Each of R1 to R4, the old constant, an unstamped process, a descendant of this
  instance and a pid cycle, as cases of the fake process table; the spawn path's stamp is tested as a pure function of
  its inputs, with no process started.
* **R6, the same rules in S5.** The start-up sweep applies them too, or the design says why it cannot be reached by
  them. S5 is reached by no test and has no seam; it is held to R1 and R3 by reading and by the guard's allowlist.

What the owner is told, as put: this changes which processes are reaped in production, and protects nothing until the
running server is rebuilt with the new marker, because the running service is an older binary and its children carry the
old constant.

### What changes in production under C

*Superseded by the table in "Design as approved", section D5, which corrects the "Today" column with F2 and F3 and
gives the cells of the approved design. This table is kept as drafted.*

| Situation | Today | After C (the rules above) |
| :--- | :--- | :--- |
| One orchestrator, its own sub-servers | not reaped (descendants) | not reaped |
| A previous run of this instance died and left orphans on the same store | reaped | reaped (R2) |
| A second orchestrator, another database, starts or runs while the first lives | each reaps the other's sub-servers (they carry the same constant) | each leaves the other's alone (R1) |
| A copy built by a developer runs beside the live service | it reaps the live service's sub-servers, and the live service reaps its own | neither reaps the other's (R1) |
| A new binary meets sub-servers stamped with the old constant | reaped, as above | left alone unless the design shows their owner is gone (R3) |
| The old service meets sub-servers stamped by a new binary | not applicable | not matched by the old reaper (R3, first bullet) |
| A process with no stamp, or a stamp the code cannot read | not reaped | not reaped (R4) |
| A test binary (a registry given no table, or a fake) | S1 reaped every marked process on the machine | cannot reach a real process at all (A+G) |

### What the UDP stop adds (Q4)

`UDPServer.Stop` closes its `cancelChan` with no guard (E66). A service shutdown calls it twice, from the graceful
shutdown (E68) and from the deferred `Stop` of `Start` (E67, E69), so every clean shutdown ends as a crash
(`panic: close of closed channel`; see "Found on the way"). The fix makes `Stop` idempotent at the type, so
that no caller has to know the order of the others. A test that calls it twice is safe to run: it builds the struct in the
package test without binding a socket, starts no goroutine, signals nothing and scans nothing, and is run with `-run`
naming only that test (the plan's phase P6a).

### Design as approved (P3, 2026-11-18)

The design of phase P3, approved by the coordinator with the owner's third ruling. It is the build's specification:
nothing the build relies on lives anywhere else. Lines are at `3443524`.

#### D1. The seam

**The interface**, in `internal/client` (`process_table.go`):

```go
type Sig uint8 // SigTerm (SIGTERM; taskkill /T /F on Windows), SigKill (SIGKILL; the same on Windows)

type ProcInfo struct {
    PID   int32
    PPID  int32  // -1 when the parent could not be read
    Start uint64 // the OS start token, compared for equality only; 0 when unreadable
}

type SelfInfo struct {
    ProcInfo
    Namespace string // Linux: the link of /proc/self/ns/pid; "" elsewhere
}

type ProcessTable interface {
    Snapshot() ([]ProcInfo, error)        // one enumeration: pid, parent, start token
    Self() (SelfInfo, error)
    Environ(p ProcInfo) ([]string, error) // fails if p.PID is no longer the process p describes
    Exe(p ProcInfo) (string, error)
    Cmdline(p ProcInfo) (string, error)
    Signal(p ProcInfo, s Sig) error // to the group led by p.PID, then to p.PID
    Alive(p ProcInfo) bool          // p.PID exists and its start token equals p.Start
}
```

* `Snapshot` replaces S1's `process.Pids()` and `parentMap()` (`process_reaper.go:90`, `:98`) and S2's
  `process.Pids()` (`:201`), as one enumeration per call, and adds the start token. `Self` replaces `os.Getpid` and
  `os.Getppid` in the selection and gives the owner stamp its token, read by the same reader that later judges
  liveness. `Environ`, `Exe`, `Cmdline` replace `util.GetProcessEnviron`, `p.Exe()`, `p.Cmdline()`. `Signal` replaces
  `killPIDGroup` for foreign pids and the signals of `harvestProcess`, with today's target (the group led by the pid,
  then the pid). `Alive` replaces the `kill(pid, 0)` polls, with identity.
* **Refusals in the OS implementation, before any system call** (a pure `validateTarget`, tested over data only):
  `p.PID <= 1` first (on unix `kill(-1)` signals every process the user may signal and `kill(0)` the caller's own
  group), then its own pid, its parent's pid, and `p.Start == 0`. The token is re-read immediately before each signal
  and a mismatch is refused: a `ProcInfo` is honoured only with the token this table's own reads produce, so no method
  signals a pid the table did not return, or one reused since. The residual race is the gap between that re-read and
  the kill system call.
* **The escalation is client code**, `Escalate(t ProcessTable, c Clock, p ProcInfo, grace, poll time.Duration) (killed
  bool, err error)` with an injected `Clock` (`Now`, `Sleep`): TERM; poll `Alive` every `poll` until `grace` has passed;
  KILL if still alive. Grace 2 s and poll 100 ms, as today. S1 runs it in a goroutine per orphan (today's asynchronous
  `killPIDGroup`), tracked by a `sync.WaitGroup` tests wait on; S2 runs it synchronously (today's `harvestProcess`).
* **Absorbed:** `parentMap` and `osParentMap` (removed); `killPIDGroup`'s foreign-pid use; `reaper_harvest_unix.go` and
  `reaper_harvest_windows.go` (deleted; `harvestProcess` becomes generic code over the table); every `process.*` and
  `util.GetProcessEnviron` call in `internal/client`; the sleeps of harvest and settle (now the clock).
* **The registry** holds `procs atomic.Pointer[tableBinding]` (table and this instance's owner), set once by
  `AttachProcessTable(t ProcessTable, storeDir string) error` (an error on a second call or if `Self` fails), read by
  `ProcessTableAttached() bool`. `NewWarmRegistry`'s signature does not change and it leaves the binding nil. The
  health monitor reaches the binding through `PruneOrphans`; `StartHealthMonitor` does not change.
* **The zero value is inert.** The first statement of `PruneOrphans` and of `enforceSingleInstance` (before the pid
  file is read) is a nil check on the binding that returns; `lookupSystemProcesses` and `harvestProcess` take the
  snapshot and the table as arguments, so they cannot run without one. S3 is unaffected.
* **Forgotten wiring is loud**, on paths no test enters: guard rule D (one call in `main.go`); `PreDrive` refuses to
  start a registry without a table (it is reached only from `ServeFunc`); the wiring line is in `ServeFunc`, after
  `NewOrchestratorApp` (the store is locked) and before `PreDrive`, not inside `NewOrchestratorApp`, which a cmd test
  calls (E60).

#### D2. The real table, and why a test cannot reach it

* A new package, `internal/proctable`, which imports `internal/client` for the types; `internal/client` does not import
  it. The type `osProcessTable` is unexported. `NewOSProcessTable() client.ProcessTable` panics as its **first
  statement** when `testing.Testing()` is true. Panic, not an error: an error can be discarded.
* An internal test of `internal/client` that imports `internal/proctable` is an import cycle, a compile error; `go list
  -deps -test ./internal/client/` and `./internal/handler/` do not list `internal/proctable`.
* Guard additions: **A2**, no `_test.go` outside `internal/proctable/` imports it (an external `package client_test`
  would not hit the cycle); **A3**, a composite literal of `osProcessTable` appears only inside `NewOSProcessTable` or
  `newChildOnlyTable`, `newChildOnlyTable` is declared only in a `_test.go` of that package, and no non-test file names
  it; **E**, the first statement of `NewOSProcessTable` is an `if` whose condition is `testing.Testing()` and whose body
  calls `panic`; rule **B** gains two primitives, a `//go:linkname` directive and a string literal beginning `/proc/`.
  Not caught by the guard: a test that builds and runs the product binary (none exists at `3443524`).
* Twins: Linux token = field 22 of `/proc/<pid>/stat` (parsed after the last `)`), namespace = the link of
  `/proc/self/ns/pid`; elsewhere token = gopsutil `CreateTime`, namespace empty; signals by `syscall.Kill(-pid)` then
  `syscall.Kill(pid)` off Windows, `taskkill /T /F /PID` on Windows; the environment through `util.GetProcessEnviron`.
* **The one test of the real table** (`internal/proctable/table_test.go`, `childonly_test.go`): the child is the test
  binary re-executed as a helper, stamped with a random owner (a lineage that is never a production one) and a random
  mark; `newChildOnlyTable` wraps the real table and refuses reads and signals of every pid but that child, and a
  signal unless a fresh snapshot shows the child's parent is the test process. It asserts that the child's own `Self`
  token equals the parent's snapshot token for it (the property liveness rests on), that its environment yields the
  mark and the owner, that a wrong-token signal is refused and the child lives, and that TERM and the real escalation
  end it. It never passes pid 0, 1, -1, its own pid or its parent to a real table. It refuses other processes'
  environments too: sub-servers carry `MCP_LLM_TOKEN` (`internal/client/lifecycle.go:757-760`). First run: P9.

*Addition, 2026-11-19 (the independent review's second look, F1 and F2; the owner's ruling on the gate, relayed by
the coordinator):*

* **The gate.** The owner's ruling: "The reviewer's third way: the gate as a pure function, plus a guard clause on
  every method (Recommended)". `gateOpen(t *osProcessTable) bool` is `t != nil && t.armed`, and `live()` is exactly
  `return gateOpen(t)`. A test, over data only, shows `gateOpen(nil)` is false; its argument is an untyped nil, so rule
  A3 is untouched. New guard rule **E2**: in `internal/proctable`'s non-test code, `live()`'s only statement is that
  return, and every exported method of `osProcessTable` begins with `if !<recv>.live() … { …; return … }`, the `live()`
  test the leftmost operand (`Signal` may first refuse an unsafe target with `validateTarget`, which touches nothing).
  `Alive` gained the check as its first statement. No guard exception is needed, and a broken gate can signal nothing in
  a test. **Left unproven at run time:** the other shut branch, a non-nil table that is not armed. No test can build one
  (rule A3), so it is verified by reading only.
* **Rule A3, tightened (F1).** In a test file the type may be named only as the type of `childOnlyTable`'s field
  `inner *osProcessTable` (and inside `newChildOnlyTable`). Any other type declaration that names it fails: an alias, a
  look-alike struct to cast to, a second field, a slice.
* **Rule A4, (vii) and (viii) (F1).** (vii): `childOnlyTable`'s method set, across all of the package's test files, is
  exactly `isOwnChild` and the seven `ProcessTable` methods, so no further method can reach `.inner` under the type's
  name. (viii): no test file of `internal/proctable` imports `reflect` or `unsafe`.
* **Rule B, further primitives (F2).**
  * A test that names `killStaleProcess`, `waitForProcessExit` or `signalOrWarn` (package main's wrappers, which signal
    the pid or process they are given) is a primitive, as `cleanupStaleProcess` already was.
  * `golang.org/x/sys/windows` and `syscall` `OpenProcess`/`TerminateProcess`.
  * gopsutil's `Terminate`, `SendSignal`, `Suspend` and their `WithContext` forms, and `KillWithContext`.
  * Method values and method expressions of every signalling method, called or not (`f := p.Signal`,
    `(*os.Process).Kill(p)`).
  * Any short-option cluster containing `c` starts a shell string (`bash -lc`, `sh -ec`).
  * `ps` with a BSD `e` or a dash `e`/`E` argument, as arguments or inside a shell string.
  * A `/proc` path built by concatenating constants.
  * Strings read as constants: literals, same-file constants and their concatenations, so a command word held in a
    constant counts.
  * In a test, an `exec.Command`/`CommandContext` whose program name is not a constant.
  * A dot-import of `os/exec`.
  One new allowlist entry, named to the coordinator before it was added: `internal/proctable/table_test.go`, the helper
  child launched as `os.Args[0]` (own-child, count 1). The guard's own `procfsRoot` is now built at run time. Not
  covered by parsing: a shell string whose text is not constant. The one such site is
  `exec.Command("sh", "-c", script)` in `internal/client/own_child_unix_test.go:24`, where `script` is a parameter of
  `startOwnChild`; its five callers pass literals (`exec sleep 30`, and `trap "" TERM; exec sleep 30`), read by hand.

*Addition, 2026-11-19 (a deliberate removal on the owner's word, relayed by the coordinator):*
`cmd/mcp-server-magictools/service_extra_test.go` is deleted. The owner's ruling: "Delete the file (Recommended)".

* **What it held:** one test, `TestServiceFunctions`, whose first statement was
  `t.Skip("Skipping destructive service test")` (E54). Past the skip it would have done the following, on whatever
  machine ran it:
  * installed and uninstalled the real service (systemd, launchd, Windows);
  * removed the real `service.state`;
  * called `waitForProcessExit(999999, …)` and `killStaleProcess(999999)` (E53), which signal that pid if it is alive.
* **Why delete it:** it was dead code whose only possible effect was destructive, and no coverage is lost, because
  the body never ran.
* **The guard:** rule B's new test-call primitives reported its two calls, and with the file gone the guard is green
  with no exception. The same two calls planted in another `cmd` test are still reported.

*Addition, 2026-11-19 (the reviewer's third read; the owner's rulings, relayed by the coordinator):*

* **`killStaleProcess` deleted.** The owner's ruling: "Delete `killStaleProcess` in 0010 (Recommended)". Deleting
  `service_extra_test.go` left `killStaleProcess` (`cmd/mcp-server-magictools/service.go`) with no caller. It
  signalled a pid, and golangci-lint reported it unused on linux and darwin. This takes that one function, and only it,
  out of the earlier deferral of dead helpers to a later record. Nothing else became unused because of it. Rule B's
  count of `os.FindProcess` in `service.go` (S7) drops from 2 to 1. `signalOrWarn` keeps its other caller,
  `waitForProcessExit`, and `testCalls` still lists `killStaleProcess`, so a test that named it would be reported.
* **The guard's scope.** The owner's ruling: "Close the two real-table gaps; record the nine exotic forms as known
  limits; no further bypass hunt (Recommended)". As put: the table's own package gets the tight rule (signalling code
  named only inside `Signal` after the gate; no `init`; no function-valued variables), and the nine forms are recorded
  as things the guard does not catch.
  * **E2a**, the gate's shut branch and Signal's refusal: the live-check's shut branch is one `return` that calls
    nothing, with no `defer` and nothing before it; `Signal`'s refusal is exactly
    `validateTarget(p, safePID(os.Getpid()), safePID(os.Getppid()))`.
  * **E2b**, the real table's package. The coordinator's decision, inside that ruling, on what today's code satisfies;
    it corrects a brief that asked for every allowlisted primitive inside `Signal`, which no read can satisfy. In
    `internal/proctable`'s non-test code:
    * (a) `signalGroupAndPID` is named only inside `(*osProcessTable).Signal`, after its live-check;
    * (b) the signalling primitives are named only inside the body of `func signalGroupAndPID`;
    * (c) the reading primitives are named only inside `osProcessTable` methods or inside the token helpers
      `startToken` and `pidNamespace`, and those two are named only inside `osProcessTable` methods;
    * (d) there is no `func init`, and no package-level `var` holds a function.

    Today's code satisfied all four as it stood: `internal/proctable` was not edited.
  * **Known limits: what the guard does not catch.** The guard is a tripwire against accidents, not a defence against
    someone trying to get past it, and no further bypass forms are hunted. These forms pass it, and review is what
    stands behind them (the reviewer's D01, D02, D03, D04, D08, D09, D10, D12, D13):
    * a computed shell string after `-c`;
    * an `exec.Cmd` struct literal with its `Path` from a variable;
    * `os.StartProcess` or `syscall.ForkExec` of a computed name;
    * a `/proc` path built with `path.Join` of separate words;
    * a configuration struct's `Command:` field naming a program the registry would spawn;
    * a non-test run-anything wrapper called from a test with a variable;
    * `reflect` `MethodByName`;
    * a non-test wrapper, in package main, of a signalling helper, called from a test.

    A `//go:generate` line (D11) is harmless: `go generate` runs only by hand, never in `go build`, `go vet` or
    `go test`. The testing guide states the same where it describes the guard.
* **The allowlist entry for `internal/proctable/table_test.go`** (the helper child launched as `os.Args[0]`, own-child)
  is per file and count, as every entry is: one non-literal program launch in that file, whichever expression it uses.
  It is not pinned to `os.Args[0]`. That is as designed: the count makes a second launch fail, and rule A4 holds what
  that file's tests may do with the child.

#### D3. S3, the registry's own child

S3 stays direct, in `internal/client`, and never touches the table (with no table the registry must still end its own
children). It closes F4: the Wait goroutine closes a per-child `exited` channel after `cmd.Wait()` returns; the
`SubServer` carries it beside `Process`; the kill signals the leader through `cmd.Process.Signal` (which returns
`os.ErrProcessDone` once `Wait` has returned), the group only if that succeeded, and escalates by waiting on `exited`
for 2 s instead of polling the pid; Windows uses `cmd.Process.Kill()` and `taskkill /T` only while `exited` is open. Its
files keep their primitives on the allowlist as `own-child`.

*As built (P4a step 5):* the channel is held by the registry, in a map from the child's `*exec.Cmd` to its channel
(`childExits`), not on the `SubServer`; the Wait goroutine closes the channel and then removes the entry, and
`stopOwnChild` signals a command only if it finds it there with the channel open. So a command the registry never
started, or one already reaped, is never signalled, and neither `spawnProcess`'s signature nor the `SubServer` changes.
`killPIDGroup` is gone; the unix kill is `internal/client/own_child_unix.go`, the Windows one `process_windows.go`.

#### D4. The marker

* **Lineage:** the first 32 hexadecimal characters of SHA-256(canonical store directory + NUL + pid-namespace link).
  The canonical store directory is `filepath.Abs`, `filepath.EvalSymlinks`, `filepath.Clean` of the directory whose
  `LOCK.magic` this process holds (flock, exclusive, non-blocking: `internal/db/cleanup_unix.go:21-36`). Same store,
  same lineage (the third ruling, P3 Q2): the flock admits one holder and shutdown drains every sub-server before it
  closes the store (`cmd/mcp-server-magictools/main.go:324-366`). Two spellings of one directory give two lineages,
  which fails safe.
  *Note, 2026-11-18 (the independent review, P8):* the mechanism cited above is not what holds. `PreDrive` unlinks
  `LOCK.magic` right after the store is opened (`cmd/mcp-server-magictools/main.go:311`), so a later process creates
  a new file of that name and its flock succeeds. One holder per store is held by Badger's own directory lock:
  `NewStore` opens Badger on the store directory, and Badger v4.9.1 takes `flock(LOCK_EX|LOCK_NB)` on the directory
  itself (`db.go:191`, `acquireDirectoryLock`, `dir_unix.go:43-52`; this repository never sets `BypassLockGuard`), which
  no unlink inside the directory can release; on Windows it opens its own `LOCK` file with share mode 0
  (`dir_windows.go:78-81`). The conclusion, one live holder per store, stands.
* **Owner:** this process's pid and start token, from `Self()`.
* **The stamp on every child:** `MCP_ORCHESTRATOR_OWNED=true` (kept: the sub-server protocol, F1),
  `MCP_ORCHESTRATOR_SERVER_NAME=<name>` (kept), and new `MAGICTOOLS_OWNER=v1:<lineage>:<pid>:<token>`, absent when the
  registry has no owner (a test registry). `MAGIC_TOOLS_PEER_ID` is no longer stamped (F7). All four are stripped from
  the inherited environment first. The new variable matches neither old test (E2 exact; E44 to E46 exact or prefix);
  the child as a whole still does (R3, amended).
* **Parsing (R4):** exactly one distinct `MAGICTOOLS_OWNER` value; four `:`-separated fields; `v1`; lineage 32
  lowercase hexadecimal characters; pid decimal 1 to 2147483647; token decimal greater than 0. Anything else is
  unattributable and is not reaped. A change to how the token or lineage is computed changes the version.
* **S1, `reapable`** (snapshot `R`, self `me`, lineage `L`, managed child pids `M`):

```text
r.PID > 1  ∧  r.PID ∉ {me.PID, me.PPID}
∧ ¬descends(r, {me.PID} ∪ M)            # the walk over R, cycle- and depth-safe, WITHOUT me.PPID (F2)
∧ env := Environ(r) readable ∧ o := ParseOwner(env) ok ∧ o.Lineage = L
∧ (   (o.PID, o.Start) = (me.PID, me.Start) ∧ detached(r)     # an orphan of this process
    ∨ (o.PID, o.Start) ≠ (me.PID, me.Start) ∧ ownerDead(o) )  # an orphan of a dead owner of this lineage (R2)

ownerDead(o) ⇔ no row of R has PID o.PID
             ∨ that row's Start ≠ 0 ∧ that row's Start ≠ o.Start   # the pid was reused
             # Start = 0 (unreadable): alive, not reaped
detached(r)  ⇔ the walk from r reaches pid ≤ 1 (or ppid 0) through known links only, no cycle, within the cap
```

* **S2, `harvestable`** (before spawning `name`): `r.PID > 1`, not self, parent or excluded; environment readable;
  owner stamp parses with `o.Lineage = L`; and the entry `MCP_ORCHESTRATOR_SERVER_NAME=<name>` is present. Owner
  liveness is not required (one process per name within a lineage is S2's purpose, as today). The executable, suffix
  and interpreter look-alike rules are removed (third ruling, P3 Q4). A pid from `<PIDDir>/<name>.pid` is a candidate
  only if it is in the snapshot and passes this rule.
* **Old-stamped processes** (`MCP_ORCHESTRATOR_OWNED=true` and no owner stamp) are never touched by S1 or S2 (third
  ruling, P3 Q3); the guide gives a one-time read-only check for leftovers after the reinstall.

| Case | S1 | S2 |
| :--- | :--- | :--- |
| (a) two live instances, different stores, both new binaries | neither reaps the other's | neither harvests the other's |
| (a′) the same, one an old binary | the new leaves the old's alone; the old can reap the new's (R3, amended) | the same |
| (b) restart overlap on one store (the old process is past its drain and exiting) | waits while the old owner lives; reaps leftovers on the first tick after it exits | harvests same-name leftovers at the spawn, as today |
| (c) a crashed instance's children, after a restart on the same store | reaped at `PreDrive` and on later ticks | same-name ones harvested at the spawn |
| (d) a child that outlived its instance, adopted by pid 1 | reaped if the lineage is mine, else left | the same |
| (e) the dead owner's pid reused by an unrelated process | owner dead by token mismatch; the unrelated process carries no stamp and is not touched | the same |
| (f) a test binary, or `go run … serve` on its own temporary store | a test registry enumerates nothing and its children carry no owner; a temporary store is another lineage | the same |
| (g) a malformed, unreadable or foreign stamp | not reaped | not harvested |
| (h) Windows | an unreadable environment is not reaped; readable ones follow the same rules; compiled, not run | the same |
| (i) the owner pid present, its token unreadable | not reaped | harvested if name and lineage match |
| (j) my own child with an unknown link in its ancestry | not reaped | not applicable |
| (k) *(P8, the owner's ruling of 2026-11-18)* a process stamped by this very process and provably detached from it (adopted by pid 1 or a subreaper), whose `MCP_ORCHESTRATOR_SERVER_NAME` names a server running here | not reaped while that server runs; reaped on the first tick after it has stopped, crashed, or left the registry, and when the name is absent, empty, or names no server of this registry | harvested at that server's next spawn, as any same-name process of the lineage |

*Addition, 2026-11-18 (the independent review, P8 S3; the owner's ruling, relayed by the coordinator):* "Spare it
while the server named in its stamp is running (Recommended)". As put: a helper a live sub-server deliberately detaches
(a build server, the `go` command's telemetry child) is left alone as long as the sub-server it came from is running in
this instance, and is reaped once that server is gone. **Running** means: the registry holds the server's name and a pid
it recorded for it (`SubServer.LastKnownPID`, set under the registry lock as soon as the process is started, before the
handshake; and `SubServer.Process` once the handshake completed) is, in the same snapshot, a direct child of this
process. That counts a server that is starting, healthy, syncing, or draining after a disconnect (still a child until it
exits); it excludes one that has exited or crashed (its pid is gone, or belongs to a process that is not this one's
child) and one the registry no longer holds (an eviction deletes the entry). A helper can be reaped in the gap between
its server's exit and a restart (`orchestrateRestart` waits 3 s before it spawns again); that gap does not change the
outcome, because S2 harvests every same-name process of the lineage before the new spawn in any case. Dead owners'
orphans and other instances' processes are unchanged.

*Addition, 2026-11-19 (the independent review's second look, F5 to F7; the owner's ruling on F5, relayed by the
coordinator):*

* **An unreadable parent counts as running (F5).** The owner's ruling: "Yes: an unreadable parent for a recorded pid
  counts as running; spare the helper (Recommended)". A recorded pid whose snapshot row has an unreadable parent
  (`PPID -1`) now counts as running, as a direct child of this process does. It errs toward leaving a process alone.
  At worst a real leftover survives until a tick that can read that parent, or until the same-name harvest at a respawn.
* **A server the enumeration misses is not running (F5's other half, not covered by the ruling).** In one snapshot a
  recorded pid with no row at all cannot be told from a server that has exited: the rule's only input is the snapshot,
  and a missing row carries no field, no parent, no start token, to tell the two apart. The registry's own-child Wait
  state (`childExits`, D3) is another source, not part of this rule, and no rule was invented for it. So a live
  server's detached helper can be reaped if one enumeration misses the server. That is the remaining gap.
* **Two spares that can last (F6).** Both leak in the safe direction, and the same-name harvest at a respawn ends them:
  * A crashed server's stale `LastKnownPID`, reused by another live child of this process, keeps that server's helpers
    spared while the other child lives (the review's S3-04).
  * A forged name stamp is spared while the named server runs (S3-10): a process stamped by this process, detached,
    whose name stamp says a running server's name although that server did not start it.
* **The gap sentence, completed (F7).** A helper can be reaped in the gap between its server's exit and a restart (as
  above). As the second look found, a live server's helper could also be reaped when that server's snapshot row had an
  unreadable parent, or when the enumeration missed the server (F5). The ruling closes the first; the second remains,
  as stated above.
* **A third spare that can last (the reviewer's third read, S1).** The unreadable-parent ruling can spare a crashed
  server's helpers while that server's stale recorded pid is now held by an unrelated process whose parent cannot be
  read. It is transient: it lasts only while that read fails and the pid is held, and the same-name harvest at a
  respawn ends it. No code changes for it.
* **Tested** (`internal/client/reap_helpers_test.go`):
  * the review's S3-05 (unreadable parent: spared), red on the rule before the change;
  * S3-06 (missing row: reaped), which pins the unchanged half;
  * S3-13 (a recorded pid 1 is never running);
  * S3-07 (two different name stamps: reaped).

#### D5. What changes in production (replaces the table "What changes in production under C")

| Situation | Today | After this record |
| :--- | :--- | :--- |
| One orchestrator, its own live sub-servers | not reaped (descendants) | not reaped |
| *(P8, the owner's ruling)* A helper a live sub-server detached (adopted by pid 1 or a subreaper), stamped by this instance | the launchd service: spared (F2); an instance whose parent does not adopt orphans: reaped (it carries `=true`) | spared while the server named in its stamp runs in this instance; reaped on the first tick after that server is gone, or when the name is absent or unknown; harvested at the server's next spawn (S2) |
| The service crashed and restarted on the same store; its orphans carry the new stamp | launchd service: S1 spares them (F2); S2 harvests same-name ones when it respawns them; Linux and Windows S5 SIGKILLs them | **S1 reaps them at start-up and on every tick (TERM, 2 s, KILL)**, including those S2 would not; S2 still harvests same-name ones. New behaviour in the launchd service |
| A second orchestrator with another store (IDE-run, developer's build) beside the live service | the one whose parent does not adopt orphans reaps the other's sub-servers (S1); each harvests the other's same-name or same-command sub-servers at spawn (S2); S5 SIGKILLs the other's grandchildren at start-up | neither touches the other's (both new binaries) |
| A developer's build on the default store beside the live service | S4 kills the live service (the next record) | the same S4; the build is then the successor and reaps the dead service's orphans |
| A new binary meets old-stamped processes | not applicable | never touched (S1, S2); S5 is gone |
| An old binary meets a new binary's children | not applicable | still matched by the old S1, S2 and S5, as anything is today (R3, amended) |
| An unstamped process with a configured server's executable | S2 harvests it at spawn | left alone; it may run beside magictools' own |
| A malformed, unreadable or foreign stamp | `=true` exact only | not reaped (R4) |
| A test binary | S1 reaped every `=true` process; S2 scanned the machine | no real process reachable (no table, or a fake); the one OS test signals only its own child |
| A restart overlap on one store | S1 reaps the old instance's children if the newcomer's parent does not adopt orphans; S2 harvests same-name | S1 waits for the old owner's death, then reaps; S2 harvests same-name |
| A pid reused between the read and the signal (S1, S2) | the signal goes to whoever holds the pid | the token is re-checked before each signal |
| S3 after the child was reaped | raw-pid signals for up to 5 s plus 2 s | gated on the child's own exit and handle |
| The Linux and Windows start-up sweep (S5) | SIGKILL to `=true` or `MAGIC_TOOLS_PEER_ID` processes whose direct parent's name lacks the product name | removed (D8 step 8). What it did is split: a new-stamped orphan of this store with a dead owner is **covered** by `PreDrive`'s S1, with TERM first instead of SIGKILL; an old-stamped process, a `MAGIC_TOOLS_PEER_ID`-only process and another lineage's process are **deliberately no longer killed** (P3 Q3, F7, R1); its direct-parent-name exemption is replaced by the owner's liveness |
| A build with no table wired | not applicable | refuses to start (`PreDrive`) |
| *(P8, N2)* Attaching the table fails at start-up: this process's start token cannot be read, the store directory cannot be resolved (`EvalSymlinks`), or the process is pid 1 (a container's entrypoint) | not applicable | `serve` exits with `process table: …` before `PreDrive` (`cmd/mcp-server-magictools/main.go:136`); a service manager restarts it and it fails the same way. Deliberate: without a table there is neither orphan reaping nor single-instance enforcement. Running as pid 1 needs an init in front of it |
| *(D8 step 4)* A pid recorded in `<PIDDir>/<name>.pid` (S2) | harvested if `kill(pid, 0)` succeeds, whatever process holds the pid; if the scan fails, still harvested | harvested only if the pid is in the snapshot, as the process the snapshot saw (its start token), and, from D8 step 7, only if it passes `harvestable`; if the snapshot fails, nothing is harvested. Safer: a pid file left by a crash no longer names a process that merely reused the pid |
| *(D8 step 4)* S2's harvest on Windows | `taskkill /T /F /PID` with no check | `Alive` (the start token) first, then `taskkill`; a pid that has gone or been reused is not killed. Safer; neutral for a process that is still the one found |
| *(D8 step 4)* The KILL after the grace period (S1, S2) | sent if `kill(pid, 0)` succeeds, to whoever holds the pid; a failed TERM was ignored | sent only if the start token still matches; a refused or failed TERM ends the escalation. Safer: the pid-reuse window of the 2 s grace is closed |

**D8 step 4 is not purely a data-source switch.** Its diff keeps the old selection rule, but the rows marked *(D8 step 4)*
above change behaviour, each by refusing a signal the old code would have sent to a pid that no longer names the process
that was found. The lines are in `internal/client/process_reaper.go` at that step: the pid-file candidates kept only
when they are in the snapshot (`byPID[...]` in `enforceSingleInstance`); `harvestProcess`'s `Alive` check before the
escalation (new on Windows; on unix the old `Signal(0)` check, now with identity); and `Escalate` (stop on a refused
TERM, KILL only after an identity-checked `Alive`). One more difference is timing only: S1's TERM is now sent from the
escalation goroutine, not before it.

**Migration.** The first rebuilt service starts while children of the old binary may be alive (they carry `=true` and no
owner). The install stops the old service, whose graceful path drains and TERMs its children before its double-close
panic; leftovers exist only after a hard kill or for a child that ignores TERM, and the new binary leaves them alone
until reboot or removal by hand. If one holds a resource (a sub-server's own database lock), the new sub-server of that
name fails to start until it is removed; the health report shows it. Rollback: reinstall the previous binary, which
treats the new children as its own, as today.

#### D6. S5 and `MAGIC_TOOLS_PEER_ID`

S5 is removed, not rewritten: `InitialProcessSweep` (`cmd/mcp-server-magictools/infra_unix.go:77-134`,
`infra_windows.go:41-96`) and its call (`main.go:139`). `PreDrive`'s first act is `PruneOrphans` (`main.go:282-284`),
which runs straight after it with the approved rule, TERM first, tested over the fake; a rewritten S5 would be a second,
untested reader of the same rule. R6 is met by deletion. `MAGIC_TOOLS_PEER_ID` (stamped at `lifecycle.go:720`, read only
by S5) is no longer stamped.

#### D7. Tested where

Over the fake (P5): every case of D4's table, every row of D5 except the old binary's and the mis-wired start (which no
test enters: read, and rule D), the escalation without sleeping, one snapshot per call on a table of 10 000 rows,
inertness, the stamp as a pure function, and the monitor. Only on a real machine (P9, the OS test): the stamp crosses
`exec` and comes back through the OS reader; the token a process reads of itself equals the token another reads of it;
the namespace link is readable; a signal reaches the child's group; the real escalation ends a TERM-ignoring child.
darwin: the same test on the macOS machine, after P9 and P8 (third ruling, P3 Q5). Windows: compiled only.

#### D8. Build order

Nine steps; after each, only checks that run no process on a machine with the live service, and the new fake-based
tests on the Linux machine (third ruling, item 6). (1) The interface, the owner stamp's pure functions, `Escalate`,
and the fake. (2) `internal/proctable` with the OS code moved, the constructor and guard rules A2, A3, E and B's
primitives; the OS test written, not run. (3) The wiring: `AttachProcessTable`, the `ServeFunc` call, `PreDrive`'s
refusal, `constructorRequired`. (4) The entry points read the binding with the old selection rule kept; `parentMap`
and `reaper_harvest_*` deleted; `TestPruneOrphansEnumeratesExactlyOnce` rewritten on the fake. (5) S3 (D3). (6) The
stamp (D4). (7) `reapable` and `harvestable`, with their fake cases in the same step. (8) S5 removed (D6). (9)
`TestExtraManagerMethods`, `TestHealthMonitor_Lifecycle` and `TestEnforceSingleInstanceSkipsSelf` on the fake. The
guard's red list is 4 until step 4, 3 from step 4, 0 at step 9 and no earlier.

### What each option does

| | A+G | A+G+C | B alone | C alone | D | E |
| :--- | :--- | :--- | :--- | :--- | :--- | :--- |
| `go test ./...` safe on a machine with the live service, with no flag | yes | yes | yes, while every site carries the check | not while the service runs an older binary that stamps `true`, unless the new reaper ignores the old value | no | no |
| S1 | cannot be reached by a test | cannot be reached, and cannot select another instance's children in production | refused under test | cannot select another instance's children; a test still enumerates every process | disabled by flag only | not fixed |
| S2 (the name-based scan) | cannot be reached by a test | same | refused if the check is placed at harvest and lookup | unchanged: still name-based and machine-wide | no | no |
| S4 (pid-file kill, no identity check) | not fixed; two test edges closed | not fixed | could be refused under test, but `TestCleanupStaleProcess` legitimately kills its own child, so it needs an opt-in | not fixed | no | no |
| S5 (Linux sweep) | not fixed, no test reaches it | the marker scope applies to it too | not applicable | the marker scope applies to it too | no | no |
| `TestDebugLiveBadger` | build tag | build tag | not fixed | not fixed | no | skipped by name |
| Production change | one wiring line | wiring line, plus which processes are reaped (below) | none | which processes are reaped (below) | a tag or variable production must set, or the reaper is silently off | none |
| Reaper logic remains testable in-package | yes, over a fake | yes | no: the two scan tests become no-ops or must opt in | partly | no | no |
| Test rewrite | the three reaper tests and the S2 scan tests move to a fake; every Connect test unchanged | same | the exactly-once test cannot be kept as it is | selection tests only | none | none |
| Main risk | the interface's surface; a fake that diverges from the OS | as A, and a product behaviour change that needs its own verification | tested code differs from shipped code; a future site added without the check | a wrong selection rule is a wrong kill | forgetting; or the reverse, a tag nobody sets | a new test reaches S1 again |

What C changes in production, stated exactly. Today every sub-server carries `MCP_ORCHESTRATOR_OWNED=true`, and every
orchestrator reaps every such process that is not its own descendant. Under C a sub-server carries a value that names its
owner instance, and an orchestrator reaps a marked process only when that value names its own store directory (the
variant this record recommends if C is chosen: one instance per store directory is already enforced by the database
lock, which `NewStore` takes before the reaper runs) or names an owner that is no longer alive. Two orchestrators on one
machine with different databases then stop reaping each other's children, which today they do. Sub-servers started by
an older binary carry the old value and are not reaped by the new one; one start-up's worth of leftover orphans stays
until cleared by hand. S5 uses the same marker and, in addition, any process carrying `MAGIC_TOOLS_PEER_ID=`, which is
random per spawn and therefore identifies no instance; C has to change S5 as well.

### Why not the others as the minimum

* **B alone** makes the three tests safe and leaves nothing that tests the reaper's logic; the exactly-once cost guard
  (MADR 0003) cannot be kept as written; and its safety lives in a branch on `testing.Testing()` that production never
  takes, at every site, for ever.
* **C alone** is a product change, it protects tests only once the owner's service itself stamps the new value (the
  running service is an older binary), and a test would still enumerate every process.
* **D and E** fail the "no flag to remember" driver.

## Consequences

* Good, because after A+G no test of the module can enumerate or signal a real process, with no flag: the registry's
  zero value is inert, the real table's constructor panics under test, and a parse-only gate fails the build if a test
  or a new signalling site appears.
* Good, because the reaper's logic stays tested, now over a fake table that can express cases the real machine cannot:
  a marked process whose parent is another live orchestrator, a pid cycle, a table of ten thousand entries.
* Good, because the package variable that a leaked goroutine could read while a test writes it is gone.
* Bad, because the interface is a new surface to keep, and a fake can drift from the OS implementation. The OS
  implementation therefore keeps one test that scans and signals only a child it started itself, selected by a marker
  value that is random per test and is never the production constant.
* Bad, because the zero value being inert means a missing wiring line turns the reaper off without any error. The plan
  adds a gate for the wiring.
* Bad, because this record does not make S4 safe in production. The pid-file cleanup still kills whatever holds the
  recorded pid; the repository assessment's incident (E61) and the live service's log record it killing a process three
  times (see "Found, not fixed here"). The owner's answer to Q2 makes its identity check the next record.
* Good, because under C two orchestrators on one machine, or a developer's copy beside the live service, stop reaping
  each other's sub-servers (R1), which is the product's own form of the hazard.
* Bad, because C changes which processes the shipped binary reaps, and a wrong selection rule kills the wrong process.
  It cannot be verified by a real reaper before the owner's service is rebuilt with it: it is verified by reading, by
  the fake table (R5) and, for the table's OS implementation only, by a test that signals only its own child. The
  first real exercise of the selection is the rebuilt service, and rollback is to reinstall the previous binary.
* Bad, because C protects nothing until the owner's running server is rebuilt with the new marker: the running service
  stamps the old constant, and a new binary beside it must leave those children alone (R3). The test hazard is removed
  by the seam at once; the product hazard only on the rebuild.
* Good, because the UDP stop no longer panics, so a clean shutdown ends cleanly.
* Bad, because in the launchd service S1 now reaps a crashed run's orphans, which it never did (F2). Only processes
  with this store's lineage and a dead owner are reaped (D4).
* Bad, because old-stamped leftovers of the last old-binary run are never reaped (third ruling, P3 Q3); they stay until
  reboot or removal by hand, and one that holds a resource stops the new sub-server of that name from starting.
* Bad, because a copy of a sub-server started outside magictools is no longer harvested before magictools starts its own
  (third ruling, P3 Q4), so the two may run side by side.
* Bad, because until the owner reinstalls, an old binary can still reap a new binary's children (R3, amended).
* Bad, because three windows of pid reuse remain, each microseconds wide and each between two system calls on the
  registry's own child (the independent review, P8): on darwin, `wait4` can return before `os.Process` marks the
  handle done, so a `Signal` in that gap reaches the raw pid; on Windows, `taskkill /T` runs between `Wait` releasing
  the handle and `close(exited)`; and on unix the group signal `kill(-pid)` follows the handle's successful signal of
  the leader. Each needs the pid to be reused inside that gap.
* Neutral, because the three plans in flight are untouched; but the whole-module gates of 0009 and 0008 wait for this
  record's verification (Q3).

### Confirmation

Compliance is confirmed by these, in the order they can be run safely:

1. **By reading.** The diff keeps the bodies of `PruneOrphans`, `enforceSingleInstance`, `lookupSystemProcesses` and
   `harvestProcess` textually the same except for where each datum comes from; no `_test.go` names the real table's
   constructor; the three tests and the S2 scan tests construct a fake. *Added by P3:* except in the steps that apply
   the marker rules (D8 steps 6 and 7), each a cell of D4 or D5; the step that switches the data sources (D8 step 4) and
   the step that changes the selection (step 7) are separate diffs, so each can be read alone.
2. **By a static check that runs no process.** `go build ./...`, `go vet ./...` and `go test -c -o /dev/null
   ./internal/client/` (and again for `./cmd/mcp-server-magictools/`, one package per call) compile everything and run
   nothing; `go list -deps
   ./internal/testsafety/` shows the guard package imports neither `internal/client` nor gopsutil; `go test
   ./internal/testsafety/` runs the guard, which only parses.
3. **Seen failing first, without a real process.** The guard run against the unchanged tree fails and names the three
   test files; run against a temporary copy with one planted violation per rule it fails on each; a table-driven test
   over a fake table shows that the unchanged selection rule selects a marked process that has nothing to do with the
   caller (the hazard, expressed as data); and the "a registry with no table touches nothing" tests fail when a scratch
   copy makes `NewWarmRegistry` build the real table, by the constructor's panic, which stops before any process is
   touched.
4. **Run only where nothing can be harmed.** The first execution of the `internal/client` tests, and of `go test ./...`,
   happens on the Linux host (Q3), after the owner (not an agent) has confirmed that no live magictools server runs
   there, and with a read-only check of that host's process environments for the marker run first. On that host
   harmless decoy processes started by the executor for the purpose, one carrying the old constant, one carrying a
   marker of another instance and one carrying a server-name stamp, survive the whole run.
   *Added by the second ruling:* the marker rules R1 to R6 are verified over the fake table, and the guard (rules A and
   D, switched on by P4) is shown live by a plant in P7.
5. **Independent review** of the diff by a fresh agent that did not write it, before anything is staged for commit.

## Pros and Cons of the Options

### A. A process-table seam

* Good, because it is structural: a registry that was never given a table cannot reach a process, whatever a test does.
* Good, because production selection and signals are the same code behind the interface.
* Good, because it covers S1 and S2 together and is the base on which S5 can later be moved.
* Neutral, because `parentMap` shows the idea is already accepted here; but it is a package variable, a write to which
  is not synchronized with a read by a goroutine that outlives its test. The health-monitor test starts one and does
  not wait for it (E18). Under `-race` that is a data race waiting for an overlap; no package-`client` test is
  parallel, so by reading it needs a leaked goroutine to overlap a later test's write. It was not observed.
* Bad, because the interface and the fake are new code to write and to keep faithful.
* Bad, because the zero value being inert can silently disable the reaper in production if the wiring line is lost.

### B. Refuse under `testing.Testing()`

* Good, because it is small and needs no interface.
* Good, because production behaviour is untouched.
* Bad, because it leaves the logic untestable in-package: a test that wants the scan has to opt in, which is the hazard.
* Bad, because every site needs its own check, and a site added later without one is not caught.
* Bad, because a program started by a test is not a test binary, so the check is false in it.

### C. A per-instance marker

* Good, because it fixes a product hazard as well: two orchestrators, or a developer's built binary, on one machine
  reap each other's children today.
* Good, because the selection becomes a property of the data, not of where the code runs.
* Bad, because it changes which processes are reaped, which needs its own verification; and verifying a reaper means
  running one.
* Bad, because it protects nothing while the running service stamps the old value, unless the new code drops the old
  value, which leaves one start-up's orphans behind.
* Bad, because it does not stop a test from enumerating every process, or S2, or S4.

### D. A build tag or an environment variable

* Good, because it is a few lines.
* Bad, because it fails the no-flag driver: a bare `go test ./...` is still the hazard. The inverse, a tag that
  compiles the reaper in, makes a release built without it silently orphan-prone.

### E. Do nothing in code

* Good, because it costs nothing now.
* Bad, because the next test that calls `PruneOrphans`, or a `-skip` list that rots, reintroduces the hazard; and a
  bare `go test ./...` stays unsafe.

## Open questions for the owner

**All four were answered on 2026-11-18** (see "Decided, second ruling" above, which carries the owner's labels). The
design phase's five questions were answered by the third ruling (above) and are not repeated here. The
questions and their options are kept as asked; each is marked with the option chosen. Q1 is the one answer that is not
the option this record recommended.

### Q1. Which seam? ANSWERED: option 2, "A+G+C"

1. **A+G, tests only (Recommended).** The process-table seam with the inert zero value, the tripwire and the parse-only
   guard; two small test changes; one wiring line in production. Cost: the interface and a fake. It lifts the hazard,
   changes no production selection, and is the smallest change that makes `go test ./...` safe with no flag.
2. **A+G+C.** The same, plus the marker value that names its instance or store directory, so that two orchestrators on
   one machine stop reaping each other's children. Cost: a product behaviour change in S1 and S5, a one-start-up orphan
   leftover, and a verification that needs a reaper to run; the record grows.
3. **B alone.** A refusal under `testing.Testing()` at each site. Cost: the reaper's logic loses its in-package
   tests, and a future site is unprotected. Smallest diff.

### Q2. Does the pid-file cleanup (S4) get an identity check in this record? ANSWERED: option 1, a later record straight after this one

1. **A later record, straight after this one (Recommended).** This record stays short. The S4 hazard is the product's,
   not the tests': any `serve`, `make run` or ad hoc run against the live store directory kills the live service
   (E56, E57, E58, E61). It is the most likely way to hurt the live service after this record lands, so it should
   follow at once, not be forgotten. *Note, 2026-11-27:* that record is
   [0016-MADR-stop-the-pid-file-cleanup-signalling-a-live-store-holder.md](0016-MADR-stop-the-pid-file-cleanup-signalling-a-live-store-holder.md),
   `proposed`. 0011 to 0015 took the numbers in between. The cleanup at `f539892` is still the one this answer describes.
2. **In this record, as a final phase.** Check that the recorded pid is not the caller's and that its executable is the
   product's before any signal; Windows gets a liveness test. Cost: a product behaviour change and a new unit of
   verification here.
3. **Never; document it.** The operations guide already says startup terminates a recorded stale process (E62); add that
   a second run against the live store directory kills the live service. Cost: the hazard stays.

### Q3. Where do the `internal/client` and whole-module gates run until the fix is verified? ANSWERED: option 1, the Linux host, after the owner's confirmation (not yet given)

1. **On a Linux fleet host the owner confirms (Recommended).** Dispatched with `run-on-host --need linux`, so the
   session machine is never chosen, after the owner has said that no magictools service runs on any Linux host the
   dispatcher may pick, and after a read-only listing of process environments on the chosen host shows no process with
   the marker. Cost: a copy to the host; the owner's confirmation.
2. **On this machine, with the live service stopped by the owner for the duration.** The owner stops and restarts it;
   no agent does. Cost: the owner's time and a service gap.
3. **On the CI runners only,** by pushing a branch. Cost: a push (which needs the owner's explicit ask, in the same
   turn), a slower loop, and no way to run the decoy check.

*Note, 2026-11-19:* the owner's confirmation was given on 2026-11-18 (the PLAN's Prerequisite 5, the third ruling's
item 6). The gates ran on the Linux machine, and P9 passed there on 2026-11-19.

### Q4. The findings under "Found on the way" (the double close, the dead code, `make run`) ANSWERED: option 2, fix the UDP double close in this record; the other two are for later records

1. **Record them; fix each in a later record (Recommended).** None is a test hazard.
2. **Fix the UDP server's double close in this record,** as one extra, separate phase: it is one guard and one test.
3. **Fix all three here.**

## More Information

### Found on the way (the UDP stop is fixed here; the rest is not)

* **`UDPServer.Stop` closes its channel twice and panics (fixed in this record by the owner's answer to Q4, phase
  P6a).** `Stop` closes `cancelChan` with no guard (E66). The
  application's `Stop` calls it (E67) and so does the graceful-shutdown path (E68); `Start` defers the application's
  `Stop` (E69) and the signal handler runs the graceful shutdown (E70), so a service shutdown calls it twice. The live
  service's log shows the result: `panic: close of closed channel` with a stack ending in `UDPServer.Stop` called from
  the application's `Stop`, on 2026-09-03 and on 2026-11-16, the first straight after "graceful shutdown complete". The
  lines in that stack trace (lines 341 and 315 of `main.go`) are the installed binary's, not this commit's; at this
  commit the sites are the ones in E67 to E70. The log shows a panic after "graceful shutdown complete" on four
  dates; the stack was read on the two named. Effect: a clean shutdown ends as a crash, so the service manager sees a
  failure each time.
* **`killStaleProcess` is dead code.** Its only caller is a test that skips before it reaches it (E52, E53, E54).
  *2026-11-19:* the test file and then the function were deleted in this record, on the owner's word (D2's additions).
* **`make run` executes the built binary, whose default command is `serve`** (E57, E58), against the default database
  path, which is the live service's (E56). `serve` runs the pid-file cleanup (S4), the initial sweep (S5, live on Linux)
  and the reaper (S1). It is the same hazard as a test run, started by hand; `make run` should not be run on a machine
  that has the live service.
* **The pid-file cleanup has killed on three recorded dates.** The log shows "Stale instance detected; sending
  SIGTERM" at the start of a new boot on 2026-08-29 (the date of the repository assessment's probe), 2026-09-03 and
  2026-11-16, the last followed by a SIGKILL after the 3 second wait. The last two follow a shutdown that ended in a
  panic, so the recorded pid had been left behind; the first follows an exit after a usage error, the assessment's own
  probe. The log does not say what held the pid; that it was a recycled pid is an inference.
* **S5's guard spares only direct children.** It skips a marked process whose direct parent's name contains the product
  name (E45); a child of a child, for example a script runner's worker, has another parent and is not spared.
* **`PruneOrphans`' comment names `MAGIC_TOOLS_PEER_ID`; the code does not use it.** Only S5 does.

### Not verified

* The times of the whole-module runs at the two incident times: relayed by the coordinator from the session's run
  history, not re-derived here (see Context).
* That any of the logged exits was caused by S1 and not by S2, S4 or something else.
* Behaviour under `-race` of the leaked health-monitor goroutine against a later test's write to `parentMap`: reasoned
  from the code, not run, because running the package is what this record forbids.
* Windows behaviour of the twins: read, not run.
* The marker's design, and so every cell of the production table under C: this record states the rules (R1 to R6) and
  designs nothing; they are verified in P3 to P5 over the fake table and, for the table's OS implementation only, on
  the confirmed host. The real selection is first exercised by the rebuilt service.
* That `TestUDPServerLifecycle` (E75) and the new double-stop test never signal or scan a process: established by
  reading `internal/telemetry` (its one gopsutil use reads the caller's own pid, `os_metrics.go`); neither has been run
  under this record yet.
* S8 beyond the stop and start seams the updater tests use.
* *Added by P3:* that field 22 of `/proc/<pid>/stat`, and gopsutil's `CreateTime` on darwin, are equal when read by two
  different processes (kernel and gopsutil semantics, read, not run; the OS test asserts it, P9 and, for darwin, the
  macOS run the third ruling allows); that the live service's parent is pid 1 (the investigation's listing); systemd's
  adoption of orphans and `KillMode=mixed` (documentation); that no reader of `MCP_ORCHESTRATOR_OWNED`,
  `MCP_ORCHESTRATOR_SERVER_NAME` or `MAGIC_TOOLS_PEER_ID` exists outside the owner's Go checkouts (not searched); that a
  normal reinstall leaves no old-stamped leftovers (inferred from the drain order).
* *Added by P4 and P5:* the real process table (`internal/proctable`) has not run anywhere (P9); the Windows code of
  S1, S2 and S3 is compiled, not run; the macOS form of the guide's one-time check (`ps -E`) is from `ps(1)`, not
  run; the `PreDrive` refusal and the `ServeFunc` wiring are read and guarded (rule D), not executed; S3's remaining
  window, between `Wait` reaping the child and the handle recording it (darwin, and Linux without pidfd), is
  microseconds and not removed.

*Addition, 2026-11-19 (P9, the reviewer's confirmation, and the owner's ruling on P10, relayed by the coordinator):*

* **Now verified, from the lists above.**
  * The real process table ran: on the Linux machine in P9, and once on a macOS machine beside the live service (the
    owner's ruling of 2026-11-18, "Yes, that one test package only, after the Linux run and the review
    (Recommended)"). That includes the darwin environment read of the test's own child, the start token read by two
    processes, and the group signal.
  * The module under `-race`, three rounds, ran on the Linux machine with every decoy untouched.
  * `TestUDPServerLifecycle` and the double-stop test ran in the module runs.
  * The macOS form of the guide's one-time check ran in a restricted shell and printed `CONTROL NOT LISTED`, as it
    must there.
* **Still not verified:**
  * the times of the incident runs;
  * which site caused the logged exits;
  * the Windows code (compiled, not run);
  * the production selection, which the rebuilt service exercises first;
  * the items P3 listed as documentation, read rather than run.
* **The reviewer's confirmation** of the last round: "CONFIRMED: commit after P9's repeat and the Mac run pass".
* **The interim rule is lifted.** The owner's ruling: "Lift it now, in this commit, as the plan says (Recommended)". As
  put: tests can no longer reach real processes (the seam, the parse guard and the Linux decoy run are the evidence),
  so `go test ./...` on a machine with the live service is allowed again. What stays forbidden, and `CLAUDE.md` and the
  guide say so, is running a built binary of this module beside the live service, because the pid-file cleanup (S4)
  keeps its hazard until its own record. The marker protects in production only once the service is rebuilt and
  reinstalled, which is the owner's act.

### Relation to other records

* [0003-MADR-bound-the-orphan-reaper-process-walk.md](0003-MADR-bound-the-orphan-reaper-process-walk.md) made the
  reaper's walk bounded and enumerated once; its exactly-once test is one of the three this record rewrites. Nothing in
  it is superseded.
* [0004-MADR-a-parse-error-must-not-replace-the-operators-server-registry.md](0004-MADR-a-parse-error-must-not-replace-the-operators-server-registry.md)
  is the precedent for a guard that fails a test binary that touched the operator's real state (E63); this record's
  guard refuses before the touch, not after.
* [0009-MADR-stop-store-close-racing-background-writes.md](0009-MADR-stop-store-close-racing-background-writes.md) and
  [0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md](0008-MADR-redesign-the-list-tools-middleware-under-the-complexity-limit.md):
  their plans' whole-module gates wait for this record's verification.
* [0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md](0006-MADR-move-go-pins-to-1-26-11-and-1-27-4-and-bump-klauspost-compress.md):
  the pins change runs whole-module gates, so it follows this one.
