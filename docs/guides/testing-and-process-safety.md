# Testing and process safety

Read this before you run any test, or any built binary, on a machine that has a live magictools service. The decision
and its evidence are in
[0010-MADR-make-tests-unable-to-signal-real-processes.md](../decisions/0010-MADR-make-tests-unable-to-signal-real-processes.md);
the work is
[0010-PLAN-make-tests-unable-to-signal-real-processes.md](../decisions/0010-PLAN-make-tests-unable-to-signal-real-processes.md).

**Status (2026-11-19).** The test hazard is removed. No test of the module can enumerate or signal a real process
other than a child it started itself. The evidence:

- the seam: the reaper and the enforcer reach processes only through a table, and tests hold a fake one;
- the parse-only guard below, green on the tree and seen red on every planted violation;
- the plan's P9 on the Linux machine: every test of the module, under `-race` too, beside five decoy processes, none of
  them touched;
- one run of the real table's own test on a macOS machine with a live service, which touched nothing else.

The interim rule `CLAUDE.md` carried until then is lifted, on the owner's word. **One thing stays forbidden on a machine
with the live service: running a built binary of this module** (see "Still not safe" below).

## What the reaper, the enforcer and the pid-file cleanup do

- **The orphan reaper** (`PruneOrphans`, in `internal/client`) runs at start-up, after boot and on every health-monitor
  tick. It reaps a process only if the process carries this instance's owner stamp (`MAGICTOOLS_OWNER`, which names the
  store directory and the owning process), and that owner is dead, or is this very process and the process is provably
  detached from it. A detached process of this very process whose `MCP_ORCHESTRATOR_SERVER_NAME` names a server
  running in this instance (a helper that sub-server detached on purpose, such as a build server) is spared while that
  server runs, and reaped once it is gone (the owner's ruling of 2026-11-18). The server counts as running while a pid
  the registry recorded for it is, in the same snapshot, a direct child of this process or a process whose parent
  cannot be read (the owner's ruling of 2026-11-19); a recorded pid missing from the snapshot counts as stopped, so a
  helper can be reaped if one enumeration misses its live server. It never reaps a descendant of this
  process, a process carrying only the old constant
  `MCP_ORCHESTRATOR_OWNED=true`, a process of another instance, or a process whose environment it cannot read. It sends
  TERM, waits two seconds, then sends KILL.
- **The single-instance enforcer** (`enforceSingleInstance`) runs when a sub-server is spawned. It harvests only
  processes of this instance that carry the same server's name stamp. The look-alike rules (same executable, the
  `mcp-server-<name>` suffix, an interpreter's command line) are gone: a copy of a sub-server started outside magictools
  is no longer killed, and may run beside magictools' own.
- **The registry's own children** (S3) are ended through their own handles and never once their exit has been
  collected, so a reused pid is never signalled.
- **The start-up sweep** (S5, `InitialProcessSweep`) is removed; the reaper at start-up is the one sweep.
- **The pid-file cleanup** (`cleanupStaleProcess`, run by `db.NewStore`) is **unchanged**. It reads `server.pid` in the
  store directory and sends SIGTERM, then SIGKILL, to that pid if it is alive, without checking that the pid is a
  magictools process. Its identity check is the next record.

Every sub-server keeps `MCP_ORCHESTRATOR_OWNED=true`: the sub-servers read it themselves to know they are orchestrated.
An old binary therefore still matches a new binary's children, and can reap them, until no old binary runs.

## The sites

| Site | What | Reached by a test | After this record |
| :--- | :--- | :--- | :--- |
| S1 | the orphan reaper, `PruneOrphans` | tests in `internal/client`, over a fake process table only | no test reaches a real process |
| S2 | the single-instance enforcer | `internal/client` tests over the fake; `internal/client` and `internal/handler` tests that connect a server use a registry without a table, which scans nothing | no test reaches a real process |
| S3 | the registry ends a child it started | the same connect and disconnect tests, and `internal/client` tests of S3 itself, each ending only its own child | its own child, through its handle |
| S4 | the pid-file cleanup | every `db.NewStore` call, on a fresh temporary directory | unchanged in the product (no identity check; the next record). Its two test edges are closed: the stale-pid test records the pid of a child it started and reaped, and the debug harness is built only with `-tags debuglive` |
| S5 | the start-up sweep | none | removed |
| S6 to S8 | service control and the updater | none | unchanged |

## What is safe

On a machine with the live service, every test of the module may run: `go test ./...`, `make test`, `make race`,
one package or all of them. By the guard's showing:

- No test of the module can enumerate or signal a real process. A registry built without a table does nothing at the
  reaper's or the enforcer's entry points. The real table is built only by `ServeFunc`, and its constructor panics in
  a test binary. The fake table is data.
- The one test of the real table (`internal/proctable`) holds a table that reads and signals only a child it started
  itself. It ran first on the Linux machine (the plan's P9), then once on a macOS machine beside the live service.
- What the marker changes in production protects only once the service is **rebuilt and reinstalled** (the owner's
  act): until then the running service is the old binary, which still reaps by the old constant alone (see "An old
  binary beside a new one" below).

**Still not safe**, after this record too:

- **Running a built binary of this module beside the live service** (`make run`, `serve`, a bare run, `go run`) with
  the default database path. The pid-file cleanup (S4) terminates whatever holds the recorded pid, which is the live
  service. The next record adds its identity check.
- **The debug harness `TestDebugLiveBadger`** (`internal/db/check_badger_test.go`). It is built only with
  `-tags debuglive`, so a normal `go test` does not compile it. It opens the directory named by `MAGICTOOLS_DEBUG_DB`
  through `NewStore`, which runs the pid-file cleanup (S4) first: pointed at a live service's directory it stops the
  live service. Use a copy of a data directory, never the live one.
- **An old binary beside a new one.** An old binary still reaps any `=true` process that its own rules select,
  including a new binary's children, until it is gone.

CI is not affected: the hosted runners have no live service.

## Isolated launchd verification

Launchd lifecycle probes and their helper processes belong in a scratch clone,
not the primary checkout. Use a random `local.magictools.test.*` label, a
temporary HOME and plist, dynamic loopback listeners, and cleanup that names
only that exact label. The helper must not open MagicTools' database or import
the orchestrator startup path. Never use the production LaunchAgent label in a
test harness. An environment skip is unfinished proof, not a passing behavioral
test. Record macOS version, exit codes, and cleanup results with the run.

The accepted launchd lifecycle plan uses the same rule for clean exit, nonzero
exit, crash supervision, disable/start/restart, timeout, and update recovery.
Native full-suite and race runs still use the fleet queue and scratch-tree
workflow described above.

## The guard

`internal/testsafety` parses the module's Go source and imports nothing from the module, so running it cannot start a
process or scan one. It is green on the tree, and each rule has been seen red on a planted violation.

- **B, the allowlist.** Every use of a primitive that enumerates or signals processes must be on the allowlist in
  `allowlist.go`, by file, primitive and **count**, so an extra use in a listed file fails as surely as a new file:
  `syscall.Kill`, `unix.Kill`, the raw `Syscall`/`RawSyscall` entries and the `SYS_KILL` numbers, `os.FindProcess`,
  `OpenProcess`/`TerminateProcess` (`syscall` and `x/sys/windows`), gopsutil's process listing, reading another
  process's environment, the signalling methods (`Signal`, `Kill`, gopsutil's `Terminate`, `SendSignal` and `Suspend`)
  whether called, taken as a method value or written as a method expression, a command word such as `kill`,
  `taskkill` or `launchctl` (also as a path's base name, and inside a shell string after `-c`, `-lc` or any cluster
  with `c`), `ps` asked for environments (`e`, `-E`), a `//go:linkname` directive, `import "C"`, and a `/proc` path,
  literal or built by concatenating constants. Strings are read as constants: literals, same-file constants and their
  concatenations. In a test, a call of `cleanupStaleProcess`, `killStaleProcess`, `waitForProcessExit` or
  `signalOrWarn` is a primitive, and so is an `exec.Command` whose program name is not a constant. Import aliases of
  `syscall`, `x/sys/unix`, `x/sys/windows`, `os`, `os/exec` and gopsutil are resolved, a dot-import of one is itself
  a primitive, and a package-qualified primitive counts wherever it is referenced, not only where it is called.
- **C, tests use a fake table.** A test function that calls `PruneOrphans`, `StartHealthMonitor`,
  `enforceSingleInstance`, `lookupSystemProcesses`, `harvestProcess`, `InitialProcessSweep` or `AttachProcessTable`
  must name a fake process table (`fakeProcessTable…` / `newFakeProcessTable…`) in the same function.
- **A and D, the constructor.** No test names `NewOSProcessTable`. Non-test code refers to it exactly once, a call in
  `cmd/mcp-server-magictools/main.go`; zero fails as surely as two, and a function value of it counts.
- **A2, A3 and E, the real table's package.** No test outside `internal/proctable` imports it. Inside it, the real
  table's type is built only by `NewOSProcessTable` and by the test-only `newChildOnlyTable`, and a test names the type
  only as the type of the child-only table's field `inner`. The constructor's first statement is the
  `testing.Testing()` panic.
- **E2, the gate.** `live()` is exactly `return gateOpen(t)`, a pure function a test shows shut for a nil table, and
  every exported method of the real table begins by checking `live()` (`Signal` may first refuse an unsafe target,
  with exactly `validateTarget(p, safePID(os.Getpid()), safePID(os.Getppid()))`). On a shut gate the method does
  nothing but return: one `return` that calls nothing, with no `defer` and nothing before it.
- **E2b, the real table's package.** In its non-test code, `signalGroupAndPID` is named only inside the table's
  `Signal`, after the gate; the signalling primitives only inside `signalGroupAndPID`; the reading primitives only
  inside the table's methods and the two token helpers, which only the table's methods name. There is no `init` and no
  package-level variable that holds a function. So no new function, promoted method, unexported method or function
  value of the package can reach a process except through the gated methods.
- **A4, that package's own tests.** They name neither `signalGroupAndPID` nor `startToken`; `.inner` is used only in
  the child-only table's methods; `newChildOnlyTable` takes the `*exec.Cmd` the test started (never a pid, never a
  built value); no test builds an `exec.Cmd` or `os.Process` by literal; and each of the table's `Environ`, `Exe`,
  `Cmdline`, `Alive` and `Signal` begins with the check that the pid is the test's own child. The child-only table has
  no method beyond that check and the seven of `ProcessTable`, and no test of the package imports `reflect` or
  `unsafe`.

### What the guard does not catch

The guard is a tripwire against **accidents**: a test that starts to reach the real process table, or a new site that
appears without being classified. It is not a defence against someone trying to get past it, and it is not hunted
further for new forms (the owner's ruling of 2026-11-19). These forms pass it, and review is what stands behind them:

- a shell string that is computed rather than constant, after `-c` (`exec.Command("sh", "-c", "kill -9 "+pid)`);
- an `exec.Cmd` built as a struct literal, with its `Path` from a variable;
- `os.StartProcess` or `syscall.ForkExec` of a program whose name is computed;
- a `/proc` path built with `path.Join` (or `filepath.Join`) of separate words;
- a configuration struct whose `Command:` field names a program such as `kill`, which the registry would then spawn;
- a non-test helper, in any package, that runs whatever program it is given, called from a test with a variable;
- a method reached through `reflect` (`reflect.ValueOf(p).MethodByName("Kill")`);
- a non-test function in package main that wraps a signalling helper (`signalOrWarn`), called from a test.

A `//go:generate` line that names `kill` is harmless and also passes: `go generate` runs only when someone runs it by
hand, never during `go build`, `go vet` or `go test`.

## The seam

- `internal/client` defines `ProcessTable` (`Snapshot`, `Self`, `Environ`, `Exe`, `Cmdline`, `Signal`, `Alive`). The
  reaper and the enforcer read and signal other processes only through it, after `AttachProcessTable`.
- The operating system's table is `internal/proctable`. `internal/client` does not import it, and an `internal/client`
  test that did would be an import cycle, so no `internal/client` test binary contains it. `PreDrive` refuses to start
  a registry that has no table.
- Every read and signal checks the process's start token, so a reused pid is never treated as the process that was
  seen. The table refuses pid 1 or less, itself and its parent before any system call.
- The escalation (TERM, a two-second wait, KILL) is client code on an injected clock, so it is tested without sleeping.

## Writing a test that touches the reaper

Use the fake: `newFakeProcessTable(t, self, procs...)` in `internal/client`, attached with `AttachProcessTable`, and a
`fakeClock` for anything that escalates. Name a helper that builds one `newFakeProcessTable…` so the guard sees it.
Never import `internal/proctable` from a test, and never name `NewOSProcessTable`. A test may start and end its own
child (S3); it must never pass pid 0, 1 or -1, its own pid or its parent's to anything that signals.

## After installing a rebuilt service: the one-time check for old-stamped leftovers

A new binary never touches a process that carries only the old constant (the owner's ruling). After the rebuilt
service is installed and running, list, once, the processes the old binary left behind.

The check only **lists**. It prints pids and executable paths, never an environment value, and it signals nothing
except the control process it starts itself. It starts with a **positive control**: a `sleep` stamped as a leftover
would be. If the control is not listed, the check cannot see other processes' environments on this machine, and an
empty list would be a false all-clear; the check says so. It skips its own processes by pid. Save the script for your
system as a file and run it with `sh <file>`.

On Linux (reads `/proc/<pid>/environ` of your own processes):

```sh
#!/bin/sh
# One-time check for processes stamped only with the old constant. It only lists: it prints pids and executable
# paths, never an environment value, and signals nothing but the control process it starts itself.
# Run itself again without the marker variables, so that nothing this check starts can carry them.
if [ -z "$OLDSTAMP_CHECK_CLEAN" ]; then
  exec env -u MCP_ORCHESTRATOR_OWNED -u MAGICTOOLS_OWNER OLDSTAMP_CHECK_CLEAN=1 sh "$0"
fi
# End the control however the check ends: normally, on Ctrl-C or on TERM. A script's background job ignores
# Ctrl-C, so without this an interrupted run would leave the control running and a second run would list it.
# The traps are set before the control starts, so no signal can fall between the two; until ctl is set, $! is it.
trap 'kill "${ctl:-$!}" 2>/dev/null' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# The positive control: a process stamped as a leftover would be. It must be listed, or the check cannot answer here.
env MCP_ORCHESTRATOR_OWNED=true sleep 300 &
ctl=$!
sleep 1
me=$$
seen=no
for f in /proc/[0-9]*/environ; do
  d=${f%/environ}
  pid=${d#/proc/}
  [ "$pid" = "$me" ] && continue
  e=$(tr '\0' '\n' 2>/dev/null < "$f") || continue
  printf '%s\n' "$e" | grep -qx 'MCP_ORCHESTRATOR_OWNED=true' || continue
  printf '%s\n' "$e" | grep -q '^MAGICTOOLS_OWNER=' && continue
  if [ "$pid" = "$ctl" ]; then
    seen=yes
    continue
  fi
  # Skip this check's own processes (its children), by pid.
  ppid=$(sed 's/.*) [^ ]* \([0-9-]*\) .*/\1/' "$d/stat" 2>/dev/null)
  [ "$ppid" = "$me" ] && continue
  echo "LEFTOVER $pid $(readlink "$d/exe" 2>/dev/null)"
done
if [ "$seen" = yes ]; then
  echo "CONTROL LISTED: the check works on this machine; each LEFTOVER line above is a candidate to confirm"
else
  echo "CONTROL NOT LISTED: this check cannot answer on this machine; nothing above can be trusted"
fi
```

On macOS (reads `ps -E`, the environment of your own processes):

```sh
#!/bin/sh
# One-time check for processes stamped only with the old constant (macOS). It only lists: it prints pids and
# executable paths, never an environment value, and signals nothing but the control process it starts itself.
# Run itself again without the marker variables, so that nothing this check starts can carry them.
if [ -z "$OLDSTAMP_CHECK_CLEAN" ]; then
  exec env -u MCP_ORCHESTRATOR_OWNED -u MAGICTOOLS_OWNER OLDSTAMP_CHECK_CLEAN=1 sh "$0"
fi
# End the control however the check ends: normally, on Ctrl-C or on TERM. A script's background job ignores
# Ctrl-C, so without this an interrupted run would leave the control running and a second run would list it.
# The traps are set before the control starts, so no signal can fall between the two; until ctl is set, $! is it.
trap 'kill "${ctl:-$!}" 2>/dev/null' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
# The positive control: a process stamped as a leftover would be. It must be listed, or the check cannot answer here.
env MCP_ORCHESTRATOR_OWNED=true sleep 300 &
ctl=$!
sleep 1
me=$$
ps -E -ww -o pid= -o ppid= -o command= -u "$(id -u)" | while read -r pid ppid rest; do
  case " $rest " in *" MCP_ORCHESTRATOR_OWNED=true "*) ;; *) continue ;; esac
  case " $rest " in *" MAGICTOOLS_OWNER="*) continue ;; esac
  if [ "$pid" = "$ctl" ]; then
    echo CONTROL
    continue
  fi
  # Skip this check's own processes (itself and its children), by pid.
  if [ "$pid" = "$me" ] || [ "$ppid" = "$me" ]; then
    continue
  fi
  echo "LEFTOVER $pid $(ps -o comm= -p "$pid")"
done | awk '/^CONTROL$/ { c = 1; next } { print } END {
  if (c) print "CONTROL LISTED: the check works on this machine; each LEFTOVER line above is a candidate to confirm"
  else print "CONTROL NOT LISTED: this check cannot answer on this machine; nothing above can be trusted"
}'
```

It prints one `LEFTOVER <pid> <executable>` line per candidate, then one `CONTROL …` line. The control ends with the
check, also when the check is interrupted (Ctrl-C) or terminated.

What to do next:

- **`CONTROL LISTED` and no `LEFTOVER` line:** there are no leftovers; you are done.
- **`CONTROL LISTED` and `LEFTOVER` lines:** each is a **candidate**, not proof: a process that carries the old
  constant and no owner stamp. On macOS the match is a word of the command column (`ps -E` prints the environment
  after the arguments), so a process whose arguments contain the marker text is listed too. Confirm each before
  ending it: its executable is one of your sub-servers, its parent is pid 1 or your service manager (under
  `systemd --user` orphans go to the user manager; `ps -o ppid= -p <pid>`), and it started before the rebuilt
  service did (`ps -o lstart= -p <pid>`, and the same for the service pid). If so, end it by hand with `kill <pid>`;
  if it is still there after a few seconds, use `kill -9 <pid>`. Never end a pid the list did not print. A leftover
  that holds a resource (for example a sub-server's own database lock) is the likely reason a sub-server of the same
  name fails to start in the health report.
- **`CONTROL NOT LISTED`:** this shell cannot read other processes' environments (on macOS this happens in a
  sandboxed or restricted shell, such as a tool's embedded terminal). Nothing it printed can be trusted. First run the
  check again from an ordinary terminal outside any sandbox, and prefer that answer. Only if it still says
  `CONTROL NOT LISTED`, list your processes that pid 1 has adopted, with their start times:
  `ps -A -o pid=,ppid=,lstart=,comm= | awk '$2 == 1'`. This list has no stamp test: it **cannot tell a leftover from
  an independently started copy** of a sub-server (one you, another tool or another orchestrator started), and on
  macOS pid 1 adopts nearly every launchd job. Before ending anything from it, confirm the parent chain
  (`ps -o ppid= -p <pid>`, up to pid 1 or your service manager) and what launched it (`ps -o command= -p <pid>`,
  and whether a launchd job, a systemd unit or another program of yours owns it). End it only if it is one of your configured sub-servers, it
  started before the rebuilt service did (`ps -o lstart= -p <service pid>`), and nothing else you run started it.
  Read-only, like the check; end such a process by hand as above.

The Linux form was run on the Linux machine with a planted leftover carrying a secret value: it listed the leftover and
the control, did not list a new-stamped process, and printed no environment value (the plan's execution record). The
form above was run there again, ordinarily and while interrupted: after Ctrl-C to its process group it exited 130,
and after TERM 143, and neither run left its control behind. The earlier form, without the `trap` lines, left its
control running in both cases. The macOS form has been run, before and after its `trap` lines were added, from this
record's session only against its own control child. There `ps -E` showed no environment, so it printed
`CONTROL NOT LISTED`, as it must, and no control was left after it; it has not been run interrupted on macOS. Whether
it lists on your own shell is shown by its control line.

## Not changed here

The pid-file cleanup's identity check (its own record, straight after this one) and `make run`. (`UDPServer.Stop` was
changed in this record's phase P6a: it is safe to call twice, so a clean shutdown no longer panics with
`close of closed channel`. The dead `killStaleProcess` and the skipped test that alone called it were deleted in this
record, on the owner's word.)
