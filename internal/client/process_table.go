package client

import (
	"errors"
	"fmt"
	"time"
)

// The process-table seam of 0010-MADR-make-tests-unable-to-signal-real-processes.md ("Design as approved", D1).
//
// ProcessTable is the only way this package enumerates, reads or signals a process it did not start in the same
// call. The operating system's implementation lives in internal/proctable, which imports this package; this package
// never imports it, so no test of this package can link it. A WarmRegistry that was never given a table enumerates
// nothing and signals nothing but its own children.

// Sig is a signal the table may send to a process the caller did not start.
type Sig uint8

const (
	// SigTerm asks the process to end: SIGTERM on unix, taskkill /T /F on Windows.
	SigTerm Sig = iota + 1
	// SigKill ends the process: SIGKILL on unix, taskkill /T /F on Windows.
	SigKill
)

// String names the signal for logs: TERM, KILL, or none.
func (s Sig) String() string {
	switch s {
	case SigTerm:
		return "TERM"
	case SigKill:
		return "KILL"
	}
	return "none"
}

// ProcInfo is one process as one Snapshot saw it.
type ProcInfo struct {
	PID  int32
	PPID int32 // -1 when the parent could not be read
	// Start is the operating system's start token for the process, compared for equality only; 0 when unreadable.
	// A pid whose current token differs from Start is another process.
	Start uint64
}

// SelfInfo is the calling process as its own table sees it.
type SelfInfo struct {
	ProcInfo
	// Namespace identifies the pid namespace on Linux (the link of /proc/self/ns/pid); empty elsewhere.
	Namespace string
}

// ProcessTable enumerates, reads and signals processes. Implementations refuse, before any system call, to signal a
// pid <= 1, the calling process, its parent, or a process whose start token is unknown or no longer matches p.Start.
type ProcessTable interface {
	// Snapshot lists every visible process once: pid, parent and start token.
	Snapshot() ([]ProcInfo, error)
	// Self describes the calling process.
	Self() (SelfInfo, error)
	// Environ returns p's environment as KEY=VALUE entries; it fails if p.PID is no longer the process p describes.
	Environ(p ProcInfo) ([]string, error)
	// Exe returns p's executable path.
	Exe(p ProcInfo) (string, error)
	// Cmdline returns p's command line.
	Cmdline(p ProcInfo) (string, error)
	// Signal sends s to the process group led by p.PID, then to p.PID.
	Signal(p ProcInfo, s Sig) error
	// Alive reports whether p.PID exists and is still the process p describes.
	Alive(p ProcInfo) bool
}

// Clock is the time source of the escalation, so that tests run it without sleeping.
type Clock interface {
	Now() time.Time
	Sleep(d time.Duration)
}

type realClock struct{}

func (realClock) Now() time.Time        { return time.Now() }
func (realClock) Sleep(d time.Duration) { time.Sleep(d) }

// RealClock returns the wall clock.
func RealClock() Clock { return realClock{} }

// tableBinding is what AttachProcessTable gives the registry: the table, this process as the table saw it, and the
// owner stamp this instance's children carry.
type tableBinding struct {
	table ProcessTable
	self  SelfInfo
	owner Owner
}

// AttachProcessTable gives the registry its process table, once. storeDir is the store directory whose lock this
// process holds: it names the instance (its lineage). Until a table is attached the registry enumerates nothing and
// signals nothing but its own children. A second call, a nil table, a store directory that cannot be resolved, or a
// table that cannot give this process's start token is an error, and nothing is attached.
func (m *WarmRegistry) AttachProcessTable(t ProcessTable, storeDir string) error {
	if t == nil {
		return errors.New("client: AttachProcessTable: nil process table")
	}
	canon, err := CanonicalStoreDir(storeDir)
	if err != nil {
		return fmt.Errorf("client: AttachProcessTable: %w", err)
	}
	self, err := t.Self()
	if err != nil {
		return fmt.Errorf("client: AttachProcessTable: %w", err)
	}
	if self.PID <= 1 || self.Start == 0 {
		return fmt.Errorf("client: AttachProcessTable: this process (pid %d) has no start token", self.PID)
	}
	b := &tableBinding{
		table: t,
		self:  self,
		owner: Owner{Lineage: LineageFor(canon, self.Namespace), PID: self.PID, Start: self.Start},
	}
	if !m.procs.CompareAndSwap(nil, b) {
		return errors.New("client: AttachProcessTable: a process table is already attached")
	}
	return nil
}

// ProcessTableAttached reports whether AttachProcessTable has succeeded.
func (m *WarmRegistry) ProcessTableAttached() bool { return m.procs.Load() != nil }

// clk is the registry's clock: the wall clock unless a test set one.
func (m *WarmRegistry) clk() Clock {
	if m.clock != nil {
		return m.clock
	}
	return realClock{}
}
