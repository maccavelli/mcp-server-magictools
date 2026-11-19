package client

import (
	"slices"
	"testing"
	"time"
)

// The TERM, wait, KILL escalation over the fake table and the fake clock: no process, no sleeping.

func escalateFixture(t *testing.T, dies Sig) (*fakeProcessTable, *fakeClock, ProcInfo) {
	t.Helper()
	target := ProcInfo{PID: 300, PPID: 1, Start: 42}
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 99, Start: 1}}
	return newFakeProcessTable(t, self, fakeProc{info: target, dies: dies}), newFakeClock(), target
}

func TestSeamEscalateTermSuffices(t *testing.T) {
	fake, clk, target := escalateFixture(t, SigTerm)
	killed, err := Escalate(fake, clk, target, escalateGrace, escalatePoll)
	if err != nil || killed {
		t.Fatalf("Escalate = %v, %v; want false, nil", killed, err)
	}
	if got, want := fake.signalLog(), []fakeSignal{{300, SigTerm}}; !slices.Equal(got, want) {
		t.Fatalf("signals = %v, want %v", got, want)
	}
	if clk.sleptFor() != 0 {
		t.Fatalf("waited %v for a process that exited on TERM", clk.sleptFor())
	}
}

func TestSeamEscalateKillsAfterGraceWithoutSleeping(t *testing.T) {
	fake, clk, target := escalateFixture(t, SigKill)
	start := time.Now()
	killed, err := Escalate(fake, clk, target, escalateGrace, escalatePoll)
	if err != nil || !killed {
		t.Fatalf("Escalate = %v, %v; want true, nil", killed, err)
	}
	if got, want := fake.signalLog(), []fakeSignal{{300, SigTerm}, {300, SigKill}}; !slices.Equal(got, want) {
		t.Fatalf("signals = %v, want %v", got, want)
	}
	if clk.sleptFor() < escalateGrace {
		t.Fatalf("KILL after %v of fake time, want at least %v", clk.sleptFor(), escalateGrace)
	}
	if wall := time.Since(start); wall > time.Second {
		t.Fatalf("the escalation took %v of wall time: it slept", wall)
	}
}

func TestSeamEscalateNeverDyingProcessEndsAfterKill(t *testing.T) {
	fake, clk, target := escalateFixture(t, 0)
	killed, err := Escalate(fake, clk, target, escalateGrace, escalatePoll)
	if err != nil || !killed {
		t.Fatalf("Escalate = %v, %v; want true, nil", killed, err)
	}
	if n := len(fake.signalLog()); n != 2 {
		t.Fatalf("%d signals, want TERM and KILL only", n)
	}
}

func TestSeamEscalateRefusedTermSendsNothingMore(t *testing.T) {
	fake, clk, target := escalateFixture(t, SigKill)
	stale := target
	stale.Start++
	if _, err := Escalate(fake, clk, stale, escalateGrace, escalatePoll); err == nil {
		t.Fatal("a TERM to a replaced process was not reported")
	}
	if got := fake.signalLog(); len(got) != 0 {
		t.Fatalf("signals were sent to a replaced process: %v", got)
	}
}

// TestSeamEscalatePidReusedDuringGrace: the process dies during the grace period and an unrelated process takes its
// pid; the KILL must not reach the newcomer.
func TestSeamEscalatePidReusedDuringGrace(t *testing.T) {
	fake, clk, target := escalateFixture(t, 0)
	clk.onSleep = func() { fake.reuse(target.PID, target.Start+1000) }
	killed, err := Escalate(fake, clk, target, escalateGrace, escalatePoll)
	if err != nil || killed {
		t.Fatalf("Escalate = %v, %v; want false, nil (the target is gone)", killed, err)
	}
	if got, want := fake.signalLog(), []fakeSignal{{300, SigTerm}}; !slices.Equal(got, want) {
		t.Fatalf("signals = %v, want only the first TERM", got)
	}
}

// TestSeamEscalateFinalIdentityCheckBeforeKill (plant M23): the pid is reused during the LAST sleep of the grace
// period, after the loop's last liveness check. Only the identity check after the loop stands between the KILL and
// the newcomer: no KILL may be asked for, not even one the table would refuse.
func TestSeamEscalateFinalIdentityCheckBeforeKill(t *testing.T) {
	fake, clk, target := escalateFixture(t, 0)
	sleeps, last := 0, int(escalateGrace/escalatePoll)
	clk.onSleep = func() {
		if sleeps++; sleeps == last {
			fake.reuse(target.PID, target.Start+1000)
		}
	}
	killed, err := Escalate(fake, clk, target, escalateGrace, escalatePoll)
	if err != nil || killed {
		t.Fatalf("Escalate = %v, %v; want false, nil (the target is gone)", killed, err)
	}
	if sleeps != last {
		t.Fatalf("the grace period took %d polls, want %d: the reuse did not land in the last one", sleeps, last)
	}
	if got, want := fake.signalLog(), []fakeSignal{{300, SigTerm}}; !slices.Equal(got, want) {
		t.Fatalf("signals = %v, want only the first TERM", got)
	}
	if r := fake.refusedLog(); len(r) != 0 {
		t.Fatalf("a KILL was asked for after the pid was reused: %v", r)
	}
}
