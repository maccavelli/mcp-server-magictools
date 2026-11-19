package client

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

// The fake process table of 0010-MADR-make-tests-unable-to-signal-real-processes.md (D1, D7). Tests of the reaper and
// of the single-instance enforcer use it instead of the operating system: it holds a table of processes that exist
// only as data, records every enumeration, read and signal, and never touches a real process.

var errFakeRefused = errors.New("fake process table: refused")

// fakeProc is one process of the fake table.
type fakeProc struct {
	info    ProcInfo
	env     []string
	envErr  error
	exe     string
	exeErr  error
	cmdline string
	// dies says which signal ends it: SigTerm (exits on TERM), SigKill (ignores TERM, dies on KILL), 0 (never dies).
	dies Sig
}

// fakeSignal is one signal the table was asked to send.
type fakeSignal struct {
	PID int32
	Sig Sig
}

// fakeProcessTable implements ProcessTable over data.
type fakeProcessTable struct {
	t     testing.TB
	mu    sync.Mutex
	procs map[int32]*fakeProc
	dead  map[int32]bool
	self  SelfInfo

	selfErr error
	snapErr error

	snapshots int
	envReads  []int32
	signals   []fakeSignal
	refused   []fakeSignal
}

// newFakeProcessTable builds a fake table holding procs, with self as the calling process.
func newFakeProcessTable(t testing.TB, self SelfInfo, procs ...fakeProc) *fakeProcessTable {
	f := &fakeProcessTable{t: t, procs: make(map[int32]*fakeProc, len(procs)), dead: map[int32]bool{}, self: self}
	for i := range procs {
		p := procs[i]
		f.procs[p.info.PID] = &p
	}
	return f
}

func (f *fakeProcessTable) current(p ProcInfo) (*fakeProc, bool) {
	fp, ok := f.procs[p.PID]
	if !ok || f.dead[p.PID] || p.Start == 0 || fp.info.Start != p.Start {
		return nil, false
	}
	return fp, true
}

func (f *fakeProcessTable) Snapshot() ([]ProcInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.snapshots++
	if f.snapErr != nil {
		return nil, f.snapErr
	}
	out := make([]ProcInfo, 0, len(f.procs))
	for pid, p := range f.procs {
		if !f.dead[pid] {
			out = append(out, p.info)
		}
	}
	slices.SortFunc(out, func(a, b ProcInfo) int { return int(a.PID) - int(b.PID) })
	return out, nil
}

func (f *fakeProcessTable) Self() (SelfInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.self, f.selfErr
}

func (f *fakeProcessTable) Environ(p ProcInfo) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.envReads = append(f.envReads, p.PID)
	fp, ok := f.current(p)
	if !ok {
		return nil, errFakeRefused
	}
	if fp.envErr != nil {
		return nil, fp.envErr
	}
	return slices.Clone(fp.env), nil
}

func (f *fakeProcessTable) Exe(p ProcInfo) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.current(p)
	if !ok {
		return "", errFakeRefused
	}
	return fp.exe, fp.exeErr
}

func (f *fakeProcessTable) Cmdline(p ProcInfo) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fp, ok := f.current(p)
	if !ok {
		return "", errFakeRefused
	}
	return fp.cmdline, nil
}

// Signal records the request. Client code must never ask for pid <= 1, itself or its parent: that fails the test.
func (f *fakeProcessTable) Signal(p ProcInfo, s Sig) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p.PID <= 1 || p.PID == f.self.PID || p.PID == f.self.PPID {
		f.t.Errorf("client code asked to signal pid %d (self %d, parent %d): it must never ask", p.PID, f.self.PID, f.self.PPID)
		f.refused = append(f.refused, fakeSignal{p.PID, s})
		return errFakeRefused
	}
	fp, ok := f.current(p)
	if !ok {
		f.refused = append(f.refused, fakeSignal{p.PID, s})
		return errFakeRefused
	}
	f.signals = append(f.signals, fakeSignal{p.PID, s})
	switch {
	case fp.dies == SigTerm:
		f.dead[p.PID] = true
	case fp.dies == SigKill && s == SigKill:
		f.dead[p.PID] = true
	}
	return nil
}

func (f *fakeProcessTable) Alive(p ProcInfo) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.current(p)
	return ok
}

// reuse gives pid a new start token: the process it held has gone and another holds the pid.
func (f *fakeProcessTable) reuse(pid int32, start uint64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.procs[pid]; ok {
		p.info.Start = start
	}
}

func (f *fakeProcessTable) signalLog() []fakeSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.signals)
}

func (f *fakeProcessTable) refusedLog() []fakeSignal {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.refused)
}

func (f *fakeProcessTable) snapshotCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshots
}

// fakeClock is a Clock whose Sleep advances time without sleeping.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	slept   time.Duration
	onSleep func()
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(1_700_000_000, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.slept += d
	hook := c.onSleep
	c.mu.Unlock()
	if hook != nil {
		hook()
	}
}

func (c *fakeClock) sleptFor() time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.slept
}

// TestSeamFakeTableRefusesUnknownAndStale pins the fake's own contract: a signal to a pid it does not hold, or with a
// start token that no longer matches, is refused and recorded, and nothing is "sent".
func TestSeamFakeTableRefusesUnknownAndStale(t *testing.T) {
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 99, Start: 1}}
	fake := newFakeProcessTable(t, self, fakeProc{info: ProcInfo{PID: 200, PPID: 1, Start: 7}, dies: SigTerm})
	if err := fake.Signal(ProcInfo{PID: 201, Start: 7}, SigTerm); err == nil {
		t.Fatal("a signal to an unknown pid was not refused")
	}
	if err := fake.Signal(ProcInfo{PID: 200, Start: 8}, SigTerm); err == nil {
		t.Fatal("a signal with a stale start token was not refused")
	}
	if got := fake.signalLog(); len(got) != 0 {
		t.Fatalf("refused signals were recorded as sent: %v", got)
	}
	if got := fake.refusedLog(); len(got) != 2 {
		t.Fatalf("refused log = %v, want two entries", got)
	}
	if err := fake.Signal(ProcInfo{PID: 200, Start: 7}, SigTerm); err != nil {
		t.Fatalf("a signal to a held process was refused: %v", err)
	}
	if fake.Alive(ProcInfo{PID: 200, Start: 7}) {
		t.Fatal("a process that exits on TERM is still alive after TERM")
	}
}
