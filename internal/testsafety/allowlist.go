package testsafety

import "slices"

// Site classes besides the MADR's S1 to S8.
const (
	siteProbe    = "probe"     // a read-only or signal-0 use on the caller's own process or children
	siteOwnChild = "own-child" // signals a process the caller itself started
)

// Use is one primitive and the number of times a file uses it.
type Use struct {
	Primitive string
	Count     int
}

// u records that a file uses primitive p exactly n times.
func u(p string, n int) Use { return Use{Primitive: p, Count: n} }

// group builds the allowlist entries of one file.
func group(file, site, reason string, uses ...Use) []Allow {
	out := make([]Allow, 0, len(uses))
	for _, x := range uses {
		out = append(out, Allow{File: file, Primitive: x.Primitive, Site: site, Reason: reason, Count: x.Count})
	}
	return out
}

// defaultAllowlist is the MADR's inventory (S1 to S8) as the guard sees it: each file that uses a primitive that
// enumerates or signals processes, how many times, and why that is accepted. A use that is not here fails rule B, and
// so does a use beyond the recorded count, so a new site cannot appear unclassified, not even in a file already listed;
// an entry whose use has gone fails too, so the list stays true.
//
// Site names follow 0010-MADR-make-tests-unable-to-signal-real-processes.md, "The inventory, verified".
var defaultAllowlist = slices.Concat(
	// S1: the orphan reaper's helpers. Since P4a step 4 internal/client/process_reaper.go reads and signals only through
	// the process table, and its entries are gone; since step 5 killPIDGroup is gone too.
	// S3 since P4a step 5: the registry's own child, through its handle, gated on its Wait (the MADR's D3).
	group("internal/client/own_child_unix.go", siteOwnChild, "TERM, then KILL, to the registry's own child and its group",
		u(primSignal, 2), u(primSyscallKill, 2)),
	group("internal/client/process_windows.go", siteOwnChild, "the tree of the registry's own child, then its handle",
		u(primCmdTaskkill, 1), u(primKill, 1)),
	group("internal/client/own_child_unix_test.go", siteOwnChild, "the test's own sleep child: liveness and cleanup",
		u(primSignal, 1), u(primKill, 1)),
	group("internal/util/environ_darwin.go", "S1", "the environment of another pid on darwin (kern.procargs2)",
		u(primSysctlRaw, 1)),
	group("internal/util/environ_notdarwin.go", "S1", "the environment of another pid off darwin",
		u(primGopsutilNewProc, 1), u(primEnviron, 1)),

	// S1 and S2 behind the seam: the operating system's process table (P3, the MADR's D2). Only ServeFunc builds it.
	group("internal/proctable/table.go", "S1", "the one enumeration, and each pid's environment, executable and command line",
		u(primGopsutilProcs, 1), u(primGopsutilNewProc, 2), u(funcGetProcessEnviron, 1)),
	group("internal/proctable/start_linux.go", "S1", "the start token (field 22 of the stat file) and the pid namespace",
		u(primProcfs, 2)),
	group("internal/proctable/start_other.go", "S1", "the start token (gopsutil CreateTime)", u(primGopsutilNewProc, 1)),
	group("internal/proctable/signal_unix.go", "S1", "the group and pid signal of a validated, re-identified target",
		u(primSyscallKill, 2)),
	group("internal/proctable/signal_windows.go", "S1", "taskkill of a validated, re-identified target",
		u(primCmdTaskkill, 1)),
	group("internal/proctable/table_test.go", siteOwnChild, "ends the test's own helper child through its handle",
		u(primKill, 1)),
	// S7's killStaleProcess was deleted (0010, the owner's ruling of 2026-11-19): its os.FindProcess went with it.
	group("internal/proctable/table_test.go", siteOwnChild, "re-runs the test binary itself (os.Args[0]) as the test's own helper child",
		u(primTestExecNonLiteral, 1)),
	group("internal/serviceidentity/identity.go", "S1", "read-only start-token and executable identity; no signaling or enumeration",
		u(primGopsutilNewProc, 2)),

	// S2: the single-instance enforcer. Its platform files (reaper_harvest_unix.go, reaper_harvest_windows.go) were
	// deleted in P4a step 4; it signals through the process table above.

	// Probes of the registry's own children, and reads of the caller's own process.
	group("internal/client/lifecycle.go", siteProbe, "signal 0 on a child the registry started", u(primSignal, 1)),
	group("internal/client/manager.go", siteProbe, "signal 0 on a child the registry started", u(primSignal, 1)),
	group("internal/telemetry/os_metrics.go", siteProbe, "statistics of the caller's own process", u(primGopsutilNewProc, 1)),
	group("internal/util/proc.go", siteProbe, "RSS and CPU of a pid the caller passes (its own children)",
		u(primGopsutilNewProc, 2)),
	group("internal/util/proc_test.go", siteProbe, "the test reads its own process environment", u(funcGetProcessEnviron, 1)),

	// S4: the pid-file cleanup. 0016 refuses a lock holder and a foreign executable.
	group("internal/db/cleanup_unix.go", "S4", "stale-instance kill of the pid in server.pid",
		u(primFindProcess, 1), u(primSignal, 5)),
	group("internal/db/cleanup_windows.go", "S4", "stale-instance kill of a matching executable, after a liveness test",
		u(primCmdTaskkill, 1), u(primGopsutilNewProc, 1)),
	group("internal/db/cleanup_identity.go", "S4", "read one pid's executable and this process's start token; no enumeration",
		u(primGopsutilNewProc, 2)),
	group("internal/db/cleanup_test.go", "S4", "a dead pid and the test's own children only", u(primTestCleanupStale, 4)),
	group("internal/db/cleanup_test.go", siteOwnChild, "ends the test's own sleep children", u(primKill, 2)),
	group("internal/db/cleanup_lock_unix_test.go", siteOwnChild, "re-runs this test binary to hold LOCK.magic, then checks it is alive",
		u(primTestExecNonLiteral, 1), u(primSignal, 1), u(primKill, 1), u(primTestCleanupStale, 1)),

	// S5, the start-up sweep, was removed in P4b step 8 (the MADR's D6): PreDrive's PruneOrphans is the one sweep.

	// S6 to S8: service control, reached by no test.
	group("cmd/mcp-server-magictools/service.go", "S6", "service control by the platform manager",
		u(primCmdSystemctl, 9)),
	group("cmd/mcp-server-magictools/service_refresh.go", "S6", "daemon-reload", u(primCmdSystemctl, 1)),
	group("cmd/mcp-server-magictools/io_helpers.go", "S6", "bounded systemd daemon reload and disable",
		u(primCmdSystemctl, 2)),
)
