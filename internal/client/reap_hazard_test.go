package client

import (
	"context"
	"slices"
	"testing"
)

// The hazard this record exists for, as data (0010-MADR, Context): the live service's sub-server as the old code saw
// it, an unrelated process stamped MCP_ORCHESTRATOR_OWNED=true and its server name, with no owner stamp, whose parent
// is the live service (pid 500, adopted by pid 1). The old rule selects it in both S1 and S2; the new rule does not.
// This file compiles against the code of P4a step 4 too, where it fails: that is the seen-failing of the product rule.

func newFakeProcessTableForHazard(t *testing.T) *fakeProcessTable {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 99, Start: 1}}
	return newFakeProcessTable(t, self,
		fakeProc{info: ProcInfo{PID: 500, PPID: 1, Start: 5000}, env: []string{"PATH=/bin"}, dies: SigTerm},
		fakeProc{info: ProcInfo{PID: 501, PPID: 500, Start: 5010},
			env:  []string{"PATH=/bin", EnvManaged + "=" + EnvManagedValue, EnvServerName + "=recall"},
			dies: SigTerm},
	)
}

func TestSeamHazardLiveServiceSubServerNotReaped(t *testing.T) {
	fake := newFakeProcessTableForHazard(t)
	m := &WarmRegistry{Servers: map[string]*SubServer{}}
	m.clock = newFakeClock()
	if err := m.AttachProcessTable(fake, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m.PruneOrphans()
	m.reapWG.Wait()
	if s := fake.signalLog(); len(s) != 0 {
		t.Fatalf("the live service's sub-server was signalled by the reaper: %v", s)
	}
}

func TestSeamHazardLiveServiceSubServerNotHarvested(t *testing.T) {
	fake := newFakeProcessTableForHazard(t)
	m := &WarmRegistry{Servers: map[string]*SubServer{}}
	m.clock = newFakeClock()
	if err := m.AttachProcessTable(fake, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	m.enforceSingleInstance(context.Background(), "recall", "/opt/bin/mcp-server-recall", nil)
	if s := fake.signalLog(); slices.ContainsFunc(s, func(x fakeSignal) bool { return x.PID == 501 }) {
		t.Fatalf("the live service's sub-server was harvested: %v", s)
	}
}
