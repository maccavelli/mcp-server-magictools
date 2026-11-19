package client

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
)

// The walk-through table of 0010-MADR-make-tests-unable-to-signal-real-processes.md ("Design as approved", D4), one
// named test per row, and the owner's third ruling as tests of their own. Every case is data in the fake table: no
// process is read or signalled.

// otherLineage is a lineage that is not any test registry's: another instance's store.
var otherLineage = LineageFor("/elsewhere/store", "")

// TestSeamRowA_TwoLiveInstancesOnDifferentStoresNeverTouch: another instance (pid 700, alive) on another store; its
// child, and even its orphan whose owner is dead, are never reaped or harvested.
func TestSeamRowA_TwoLiveInstancesOnDifferentStoresNeverTouch(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(Owner) []fakeProc {
		other := Owner{Lineage: otherLineage, PID: 700, Start: 7000}
		gone := Owner{Lineage: otherLineage, PID: 799, Start: 7990}
		return []fakeProc{
			{info: ProcInfo{PID: 700, PPID: 1, Start: 7000}, env: []string{"PATH=/bin"}},
			stamped(701, 700, other, nameStamp("recall")),
			stamped(702, 1, gone, nameStamp("recall")),
		}
	})
	prune(t, m, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake)
}

// TestSeamRowAPrime_NewBinaryLeavesOldBinaryChildren: an old binary's live service (pid 800) and its children, one
// adopted by pid 1, carry only the old constant and the name; a new binary touches none of them.
func TestSeamRowAPrime_NewBinaryLeavesOldBinaryChildren(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(Owner) []fakeProc {
		return []fakeProc{
			{info: ProcInfo{PID: 800, PPID: 1, Start: 8000}, env: []string{"PATH=/bin"}},
			oldStamped(801, 800, nameStamp("recall")),
			oldStamped(802, 1, nameStamp("recall")),
		}
	})
	prune(t, m, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake)
}

// TestSeamRowB_RestartOverlapOnOneStore: the predecessor on the same store (pid 900) is still alive, past its drain;
// S1 leaves its child alone while it lives; S2 still harvests the same-name child before spawning its own.
func TestSeamRowB_RestartOverlapOnOneStore(t *testing.T) {
	build := func(mine Owner) []fakeProc {
		pred := Owner{Lineage: mine.Lineage, PID: 900, Start: 9000}
		return []fakeProc{
			{info: ProcInfo{PID: 900, PPID: 1, Start: 9000}, env: []string{"PATH=/bin"}},
			stamped(901, 900, pred, nameStamp("recall")),
		}
	}
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), build)
	prune(t, m, fake)
	wantSignalled(t, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake, 901)
}

// TestSeamRowC_RestartReapsPredecessorOrphans: the predecessor (pid 950) is gone; its orphan and the orphan's child
// are reaped at start-up; S2 harvests them by name too.
func TestSeamRowC_RestartReapsPredecessorOrphans(t *testing.T) {
	build := func(mine Owner) []fakeProc {
		pred := Owner{Lineage: mine.Lineage, PID: 950, Start: 9500}
		return []fakeProc{stamped(951, 1, pred, nameStamp("recall")), stamped(952, 951, pred, nameStamp("recall"))}
	}
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), build)
	prune(t, m, fake)
	wantSignalled(t, fake, 951, 952)

	m2, fake2, _ := newFakeProcessTableRegistry(t, seamSelf(), build)
	enforce(t, m2, fake2, "recall")
	wantSignalled(t, fake2, 951, 952)
}

// TestSeamRowD_AdoptedByPidOneUnderAServiceManager: under launchd this process's parent is pid 1, to which every
// orphan is re-parented. The old walk spared every such process (F2); the new rule reaps this lineage's orphan and
// still leaves another lineage's alone.
func TestSeamRowD_AdoptedByPidOneUnderAServiceManager(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 1, Start: 1}}
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		return []fakeProc{
			stamped(960, 1, Owner{Lineage: mine.Lineage, PID: 969, Start: 9690}),
			stamped(961, 1, Owner{Lineage: otherLineage, PID: 968, Start: 9680}),
		}
	})
	prune(t, m, fake)
	wantSignalled(t, fake, 960)
}

// TestSeamRowE_DeadOwnersPidReused: the dead owner's pid 970 now belongs to an unrelated process (another start
// token): the orphan is reaped, the unrelated process is not touched. And this process itself holds the pid of a
// dead earlier owner (same pid, another token): that owner's orphan is reaped too.
func TestSeamRowE_DeadOwnersPidReused(t *testing.T) {
	self := seamSelf()
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		return []fakeProc{
			{info: self.ProcInfo, env: []string{"PATH=/bin"}},
			{info: ProcInfo{PID: 970, PPID: 1, Start: 1111}, env: []string{"PATH=/bin"}, dies: SigTerm},
			stamped(971, 1, Owner{Lineage: mine.Lineage, PID: 970, Start: 9700}),
			stamped(972, 1, Owner{Lineage: mine.Lineage, PID: self.PID, Start: self.Start + 4}),
		}
	})
	prune(t, m, fake)
	wantSignalled(t, fake, 971, 972)
}

// TestSeamRowF_TestRegistryAndTemporaryStores: a test registry's child carries no owner stamp; a developer's run on a
// temporary store has its own lineage. Neither is touched, whatever its parent.
func TestSeamRowF_TestRegistryAndTemporaryStores(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(Owner) []fakeProc {
		temp := Owner{Lineage: LineageFor("/tmp/dev-run-store", ""), PID: 989, Start: 9890}
		return []fakeProc{oldStamped(980, 1, nameStamp("test-server")), stamped(981, 1, temp, nameStamp("test-server"))}
	})
	prune(t, m, fake)
	enforce(t, m, fake, "test-server")
	wantSignalled(t, fake)
}

// TestSeamRowG_MalformedOrForeignStampNeverReaped (R4).
func TestSeamRowG_MalformedOrForeignStampNeverReaped(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(mine Owner) []fakeProc {
		gone := Owner{Lineage: mine.Lineage, PID: 999, Start: 9990}
		withEnv := func(pid int32, env ...string) fakeProc {
			return fakeProc{info: ProcInfo{PID: pid, PPID: 1, Start: uint64(pid) * 10},
				env: append([]string{EnvManaged + "=" + EnvManagedValue, nameStamp("recall")}, env...), dies: SigTerm}
		}
		return []fakeProc{
			withEnv(990, EnvOwner+"=v1:"+mine.Lineage+":999"),
			withEnv(991, EnvOwner+"=v2:"+mine.Lineage+":999:9990"),
			withEnv(992, gone.EnvEntry(), Owner{Lineage: otherLineage, PID: 999, Start: 9990}.EnvEntry()),
			withEnv(993, EnvOwner+"="),
		}
	})
	prune(t, m, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake)
}

// TestSeamRowH_UnreadableEnvironmentAsOnWindows: a process whose environment cannot be read (another session, an
// elevated process) is never reaped, even if it would carry this lineage's stamp.
func TestSeamRowH_UnreadableEnvironmentAsOnWindows(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(mine Owner) []fakeProc {
		p := stamped(995, 1, Owner{Lineage: mine.Lineage, PID: 999, Start: 9990}, nameStamp("recall"))
		p.envErr = errors.New("access denied")
		return []fakeProc{p}
	})
	prune(t, m, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake)
}

// TestSeamRowI_UnreadableOwnerTokenCountsAsAlive: the owner's pid is present but its start token cannot be read: it
// counts as alive, so S1 leaves its child; S2 still harvests the same-name child of its own lineage.
func TestSeamRowI_UnreadableOwnerTokenCountsAsAlive(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(mine Owner) []fakeProc {
		return []fakeProc{
			{info: ProcInfo{PID: 1000, PPID: 1, Start: 0}, env: []string{"PATH=/bin"}},
			stamped(1001, 1, Owner{Lineage: mine.Lineage, PID: 1000, Start: 10000}, nameStamp("recall")),
		}
	})
	prune(t, m, fake)
	wantSignalled(t, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake, 1001)
}

// TestSeamRowJ_OwnChildNotProvablyDetachedIsKept: processes stamped by this very process. A direct child is a
// descendant; one whose ancestry has an unknown link is not provably detached; only one that reaches pid 1 through
// known links is this process's orphan and is reaped.
func TestSeamRowJ_OwnChildNotProvablyDetachedIsKept(t *testing.T) {
	self := seamSelf()
	m, fake, mine := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		return []fakeProc{
			{info: self.ProcInfo, env: []string{"PATH=/bin"}},
			{info: ProcInfo{PID: 1011, PPID: -1, Start: 10110}, env: []string{"PATH=/bin"}},
			stamped(1010, 1011, mine),
			stamped(1012, 1, mine),
			stamped(1013, self.PID, mine),
		}
	})
	_ = mine
	prune(t, m, fake)
	wantSignalled(t, fake, 1012)
}

// TestSeamOwnLiveChildrenUnderAServiceManagerKept: the production shape under launchd. This process (parent pid 1)
// is in the table, so every ancestry link is known and its own children look detached by the walk alone; only the
// descendant check keeps them. Its direct child, a managed child and that child's own child, all stamped by this
// process, are never reaped. (Found by the plant "S1: descendants not excluded", which survived the fake tables whose
// self row had an unknown parent.)
func TestSeamOwnLiveChildrenUnderAServiceManagerKept(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 1, Start: 1}}
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		return []fakeProc{
			{info: self.ProcInfo, env: []string{"PATH=/bin"}},
			stamped(1300, self.PID, mine, nameStamp("recall")),
			stamped(1301, 1300, mine, nameStamp("recall")),
			stamped(1302, self.PID, mine),
		}
	})
	m.Servers["recall"] = &SubServer{Name: "recall", Process: &exec.Cmd{Process: &os.Process{Pid: 1300}}}
	prune(t, m, fake)
	wantSignalled(t, fake)
}

// TestSeamRulingOldStampedNeverTouched: the owner's ruling "Never touch them; the guide gives a one-time read-only
// check (Recommended)". Old-stamped processes, adopted by pid 1 under a service manager, with the server's name: S1
// and S2 leave every one alone.
func TestSeamRulingOldStampedNeverTouched(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 1, Start: 1}}
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(Owner) []fakeProc {
		return []fakeProc{oldStamped(1100, 1, nameStamp("recall")), oldStamped(1101, 1100, nameStamp("recall")),
			oldStamped(1102, 1)}
	})
	prune(t, m, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake)
}

// TestSeamRulingLookAlikesRemoved: the owner's ruling "Remove them (Recommended)". A process with the server's
// executable, the mcp-server-<name> suffix or the interpreter's command line, and no stamp or another lineage's
// stamp, is not harvested.
func TestSeamRulingLookAlikesRemoved(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(Owner) []fakeProc {
		other := Owner{Lineage: otherLineage, PID: 1199, Start: 11990}
		exe := func(p fakeProc, path, line string) fakeProc { p.exe, p.cmdline = path, line; return p }
		return []fakeProc{
			exe(plainProc(1110, 1), "/opt/bin/mcp-server-svc", ""),
			exe(plainProc(1111, 1), "/elsewhere/mcp-server-svc", ""),
			exe(plainProc(1112, 1), "/usr/bin/node", "node /srv/server.js"),
			exe(stamped(1113, 1, other, nameStamp("svc")), "/opt/bin/mcp-server-svc", ""),
		}
	})
	m.enforceSingleInstance(context.Background(), "svc", "/opt/bin/mcp-server-svc", nil)
	m.enforceSingleInstance(context.Background(), "svc", "/usr/bin/node", []string{"/srv/server.js"})
	wantSignalled(t, fake)
}

// TestSeamNeverAsksForPidOneSelfOrParent: pid 1, this process and its parent, each stamped as an orphan of this
// lineage with the server's name: client code never even asks the table to signal them (the table would refuse
// before any system call; the fake records the request).
func TestSeamNeverAsksForPidOneSelfOrParent(t *testing.T) {
	self := seamSelf()
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		gone := Owner{Lineage: mine.Lineage, PID: 1299, Start: 12990}
		return []fakeProc{
			stamped(1, 0, gone, nameStamp("svc")),
			stamped(self.PID, self.PPID, gone, nameStamp("svc")),
			stamped(self.PPID, 1, gone, nameStamp("svc")),
		}
	})
	prune(t, m, fake)
	enforce(t, m, fake, "svc")
	wantSignalled(t, fake)
}

// pidFile writes <PIDDir>/<name>.pid naming pid, as a crashed run would leave it.
func pidFile(t *testing.T, m *WarmRegistry, name, pid string) {
	t.Helper()
	m.PIDDir = t.TempDir()
	if err := os.WriteFile(filepath.Join(m.PIDDir, name+".pid"), []byte(pid), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestSeamParentNamedByPidFileNeverHarvested (the review's A07, plant M11): the pid file names this process's parent,
// which carries this lineage's stamp of a dead owner and the server's name. Neither the reaper nor the enforcer asks to
// signal it; the scan skips it and the pid-file candidate is refused by harvestable.
func TestSeamParentNamedByPidFileNeverHarvested(t *testing.T) {
	self := seamSelf()
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		return []fakeProc{stamped(self.PPID, 1, Owner{Lineage: mine.Lineage, PID: 1599, Start: 15990}, nameStamp("recall"))}
	})
	pidFile(t, m, "recall", strconv.Itoa(int(self.PPID)))
	prune(t, m, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake)
}

// TestSeamPidFileNamingPidOneNeverHarvested (plant M29): a pid file names pid 1, which (as data) carries this
// lineage's stamp of a dead owner and the server's name. It is never asked to be signalled.
func TestSeamPidFileNamingPidOneNeverHarvested(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(mine Owner) []fakeProc {
		return []fakeProc{stamped(1, 0, Owner{Lineage: mine.Lineage, PID: 1699, Start: 16990}, nameStamp("recall"))}
	})
	pidFile(t, m, "recall", "1")
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake)
}

// TestSeamManagedChildWithDeadOwnerStampKept (the review's A30, plant M07): a child the registry manages, adopted by
// pid 1 and stamped with a dead owner of this lineage (a stale value inherited across a restart). It is managed, so it
// is not reaped.
func TestSeamManagedChildWithDeadOwnerStampKept(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 1, Start: 1}}
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		return []fakeProc{stamped(1370, 1, Owner{Lineage: mine.Lineage, PID: 1379, Start: 13790}, nameStamp("recall"))}
	})
	m.Servers["recall"] = &SubServer{Name: "recall", Process: &exec.Cmd{Process: &os.Process{Pid: 1370}}}
	prune(t, m, fake)
	wantSignalled(t, fake)
}

// TestSeamPredecessorWithThisPidOrphanUnknownLinkReaped (plant M32): an earlier owner held this very pid (same pid,
// another start token) and left an orphan whose ancestry has an unknown link. The owner is not this process (the
// token differs), it is dead, so the orphan is reaped; matching the owner by pid alone would keep it as "this
// process's child, not provably detached".
func TestSeamPredecessorWithThisPidOrphanUnknownLinkReaped(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 1, Start: 1}}
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		return []fakeProc{
			{info: self.ProcInfo, env: []string{"PATH=/bin"}},
			{info: ProcInfo{PID: 1501, PPID: -1, Start: 15010}, env: []string{"PATH=/bin"}},
			stamped(1500, 1501, Owner{Lineage: mine.Lineage, PID: self.PID, Start: self.Start + 4}),
		}
	})
	prune(t, m, fake)
	wantSignalled(t, fake, 1500)
}

// TestSeamOwnerPidOneIsNotAnOwner (the review's A14, N1): a stamp naming pid 1 as its owner is not attributable, so
// it is never reaped or harvested, even though pid 1 is alive with another start token.
func TestSeamOwnerPidOneIsNotAnOwner(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 1, Start: 1}}
	m, fake, _ := newFakeProcessTableRegistry(t, self, func(mine Owner) []fakeProc {
		return []fakeProc{
			{info: ProcInfo{PID: 1, PPID: 0, Start: 7}, env: []string{"PATH=/bin"}},
			stamped(1700, 1, Owner{Lineage: mine.Lineage, PID: 1, Start: 9}, nameStamp("recall")),
		}
	})
	prune(t, m, fake)
	enforce(t, m, fake, "recall")
	wantSignalled(t, fake)
}
