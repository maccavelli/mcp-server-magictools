package client

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// The reaper and the single-instance enforcer over the fake process table (0010, P4a step 4, rules of P4b step 7).
// The walk-through table of the MADR's D4 is in reap_rules_test.go; these are the flows around it.

func seamSelf() SelfInfo { return SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 99, Start: 1}} }

// newFakeProcessTableRegistry builds a registry with a fake table attached on a temporary store, and the processes
// build returns, given the owner stamp this registry's children carry.
func newFakeProcessTableRegistry(t *testing.T, self SelfInfo, build func(mine Owner) []fakeProc) (*WarmRegistry, *fakeProcessTable, Owner) {
	t.Helper()
	dir := t.TempDir()
	canon, err := CanonicalStoreDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	mine := Owner{Lineage: LineageFor(canon, self.Namespace), PID: self.PID, Start: self.Start}
	fake := newFakeProcessTable(t, self, build(mine)...)
	m := &WarmRegistry{Servers: map[string]*SubServer{}}
	m.clock = newFakeClock()
	if err := m.AttachProcessTable(fake, dir); err != nil {
		t.Fatal(err)
	}
	if got := m.procs.Load().owner; got != mine {
		t.Fatalf("the registry's owner is %+v, want %+v", got, mine)
	}
	return m, fake, mine
}

// stamped is a process carrying the sub-server flag and owner o's stamp, plus extra entries.
func stamped(pid, ppid int32, o Owner, extra ...string) fakeProc {
	return fakeProc{info: ProcInfo{PID: pid, PPID: ppid, Start: uint64(pid) * 10},
		env: append([]string{"PATH=/bin", EnvManaged + "=" + EnvManagedValue, o.EnvEntry()}, extra...), dies: SigTerm}
}

// oldStamped is a process as an old binary stamps its children: the flag and the name, no owner.
func oldStamped(pid, ppid int32, extra ...string) fakeProc {
	return fakeProc{info: ProcInfo{PID: pid, PPID: ppid, Start: uint64(pid) * 10},
		env: append([]string{"PATH=/bin", EnvManaged + "=" + EnvManagedValue}, extra...), dies: SigTerm}
}

// plainProc is a process with no orchestrator variable at all.
func plainProc(pid, ppid int32) fakeProc {
	return fakeProc{info: ProcInfo{PID: pid, PPID: ppid, Start: uint64(pid) * 10}, env: []string{"PATH=/bin"}, dies: SigTerm}
}

func nameStamp(name string) string { return EnvServerName + "=" + name }

func pidsSignalled(fake *fakeProcessTable) []int32 {
	out := []int32{}
	for _, s := range fake.signalLog() {
		out = append(out, s.PID)
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// holdsFake fails the test unless m's attached table is fake: the helpers below act only on a registry whose table is
// the test's fake.
func holdsFake(t *testing.T, m *WarmRegistry, fake *fakeProcessTable) {
	t.Helper()
	if b := m.procs.Load(); b == nil || b.table != ProcessTable(fake) {
		t.Fatal("the registry does not hold the test's fake process table")
	}
}

func prune(t *testing.T, m *WarmRegistry, fake *fakeProcessTable) {
	t.Helper()
	holdsFake(t, m, fake)
	m.PruneOrphans()
	m.reapWG.Wait()
}

func enforce(t *testing.T, m *WarmRegistry, fake *fakeProcessTable, name string, exclude ...int) {
	t.Helper()
	holdsFake(t, m, fake)
	m.enforceSingleInstance(context.Background(), name, "/opt/bin/mcp-server-"+name, nil, exclude...)
}

func wantSignalled(t *testing.T, fake *fakeProcessTable, want ...int32) {
	t.Helper()
	if want == nil {
		want = []int32{}
	}
	if got := pidsSignalled(fake); !slices.Equal(got, want) {
		t.Fatalf("signalled %v, want %v", got, want)
	}
	if r := fake.refusedLog(); len(r) != 0 {
		t.Fatalf("client code asked the table for refused signals: %v", r)
	}
}

// TestSeamRegistryWithoutTableTouchesNothing: a registry that was never given a table enumerates nothing; the fake
// here is built and never attached, and is never consulted.
func TestSeamRegistryWithoutTableTouchesNothing(t *testing.T) {
	fake := newFakeProcessTable(t, seamSelf(), oldStamped(200, 1))
	m := &WarmRegistry{Servers: map[string]*SubServer{}, PIDDir: t.TempDir()}
	start := time.Now()
	m.PruneOrphans()
	m.enforceSingleInstance(context.Background(), "svc", "/bin/svc", nil)
	m.reapWG.Wait()
	if fake.snapshotCount() != 0 || len(fake.signalLog()) != 0 {
		t.Fatal("an unattached table was consulted")
	}
	if m.ProcessTableAttached() {
		t.Fatal("a zero registry reports a table")
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("an inert registry took %v: it waited for something", d)
	}
}

// TestSeamEnforceOneSnapshotAndExclusions: one enumeration per enforcement; the caller's exclusions are honoured;
// the settle delay runs on the clock.
func TestSeamEnforceOneSnapshotAndExclusions(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(mine Owner) []fakeProc {
		gone := Owner{Lineage: mine.Lineage, PID: 9999, Start: 1}
		return []fakeProc{stamped(400, 1, gone, nameStamp("svc")), stamped(405, 1, gone, nameStamp("svc"))}
	})
	clk := newFakeClock()
	m.clock = clk
	enforce(t, m, fake, "svc", 405)
	wantSignalled(t, fake, 400)
	if fake.snapshotCount() != 1 {
		t.Fatalf("%d snapshots, want 1", fake.snapshotCount())
	}
	if clk.sleptFor() < 250*time.Millisecond {
		t.Fatalf("the settle delay did not run on the clock: %v", clk.sleptFor())
	}
}

// TestSeamPidFileCandidateMustPassTheRule: a pid named by <PIDDir>/<name>.pid is harvested only if it is in the
// snapshot and carries this lineage's stamp and the name; a recycled pid is left alone. 1021 is this instance's
// stale child of the server and is found by the scan in every case; the pid file must add nothing the rule refuses.
func TestSeamPidFileCandidateMustPassTheRule(t *testing.T) {
	for _, tc := range []struct{ name, pidFile string }{
		{"recycled pid, no stamp", "1020"},
		{"another server's child of this instance", "1023"},
		{"this instance's stale child of the server", "1021"},
		{"pid not running", "1022"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(mine Owner) []fakeProc {
				gone := Owner{Lineage: mine.Lineage, PID: 9999, Start: 1}
				return []fakeProc{plainProc(1020, 1), stamped(1021, 1, gone, nameStamp("svc")),
					stamped(1023, 1, gone, nameStamp("other"))}
			})
			m.PIDDir = t.TempDir()
			if err := os.WriteFile(filepath.Join(m.PIDDir, "svc.pid"), []byte(tc.pidFile), 0o600); err != nil {
				t.Fatal(err)
			}
			enforce(t, m, fake, "svc")
			wantSignalled(t, fake, 1021)
		})
	}
}

func TestSeamHarvestEscalatesWithoutSleeping(t *testing.T) {
	m, fake, _ := newFakeProcessTableRegistry(t, seamSelf(), func(mine Owner) []fakeProc {
		p := stamped(420, 1, Owner{Lineage: mine.Lineage, PID: 9999, Start: 1}, nameStamp("svc"))
		p.dies = SigKill
		return []fakeProc{p}
	})
	clk := newFakeClock()
	m.clock = clk
	start := time.Now()
	enforce(t, m, fake, "svc")
	if got, want := fake.signalLog(), []fakeSignal{{420, SigTerm}, {420, SigKill}}; !slices.Equal(got, want) {
		t.Fatalf("signals %v, want %v", got, want)
	}
	if clk.sleptFor() < escalateGrace {
		t.Fatalf("KILL after %v of fake time, want at least %v", clk.sleptFor(), escalateGrace)
	}
	if wall := time.Since(start); wall > time.Second {
		t.Fatalf("the harvest took %v of wall time: it slept", wall)
	}
}
