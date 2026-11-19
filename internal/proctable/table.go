// Package proctable is the operating system's process table behind internal/client's ProcessTable seam
// (0010-MADR-make-tests-unable-to-signal-real-processes.md, "Design as approved", D2).
//
// It imports internal/client for the seam's types; internal/client never imports it, so no test of that package can
// link this code. Its one constructor, NewOSProcessTable, panics in a test binary, and only the product's ServeFunc
// calls it. The type is unexported and inert unless armed by a sanctioned constructor, so nothing outside this
// package can obtain a working one another way.
package proctable

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/shirou/gopsutil/v4/process"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

var (
	errRefused        = errors.New("proctable: refused: not a process this table may read or signal")
	errNotSameProcess = errors.New("proctable: the pid no longer names the process that was read")
)

// NewOSProcessTable returns the operating system's process table. It panics in a test binary: tests use a fake
// table (internal/client) or, in this package only, the child-only table.
func NewOSProcessTable() client.ProcessTable {
	if testing.Testing() {
		panic("proctable.NewOSProcessTable called in a test binary: tests use a fake process table " +
			"(0010-MADR-make-tests-unable-to-signal-real-processes.md)")
	}
	return &osProcessTable{armed: true}
}

// osProcessTable reads and signals real processes; every answer comes from the operating system. armed is set only by
// NewOSProcessTable and, in this package's tests, by newChildOnlyTable: a zero value or a nil pointer refuses
// everything, so the type does nothing unless one of those two built it.
type osProcessTable struct{ armed bool }

// gateOpen is the table's one gate, kept pure so that a test can show it shut for a nil table without building one:
// the owner's ruling, as relayed, "The reviewer's third way: the gate as a pure function, plus a guard clause on every
// method (Recommended)". The guard requires every exported method to begin by checking live().
func gateOpen(t *osProcessTable) bool { return t != nil && t.armed }

func (t *osProcessTable) live() bool { return gateOpen(t) }

// Snapshot enumerates the process table once (moved from internal/client's osParentMap, MADR 0003 F1) and adds each
// process's start token.
func (t *osProcessTable) Snapshot() ([]client.ProcInfo, error) {
	if !t.live() {
		return nil, errRefused
	}
	procs, err := process.Processes()
	if err != nil {
		return nil, err
	}
	out := make([]client.ProcInfo, 0, len(procs))
	for _, p := range procs {
		info := client.ProcInfo{PID: p.Pid, PPID: -1}
		if ppid, err := p.Ppid(); err == nil {
			info.PPID = ppid
		}
		if tok, err := startToken(p.Pid); err == nil {
			info.Start = tok
		}
		out = append(out, info)
	}
	return out, nil
}

// Self describes this process, with its start token read by the same reader that judges every other process.
func (t *osProcessTable) Self() (client.SelfInfo, error) {
	if !t.live() {
		return client.SelfInfo{}, errRefused
	}
	pid := safePID(os.Getpid())
	tok, err := startToken(pid)
	if err != nil {
		return client.SelfInfo{}, fmt.Errorf("proctable: own start token: %w", err)
	}
	return client.SelfInfo{
		ProcInfo:  client.ProcInfo{PID: pid, PPID: safePID(os.Getppid()), Start: tok},
		Namespace: pidNamespace(),
	}, nil
}

func (t *osProcessTable) Environ(p client.ProcInfo) ([]string, error) {
	if !t.live() || !readable(p) {
		return nil, errRefused
	}
	env, err := util.GetProcessEnviron(int(p.PID))
	if err != nil {
		return nil, err
	}
	if !t.same(p) {
		return nil, errNotSameProcess
	}
	return env, nil
}

func (t *osProcessTable) Exe(p client.ProcInfo) (string, error) {
	if !t.live() || !readable(p) {
		return "", errRefused
	}
	proc, err := process.NewProcess(p.PID)
	if err != nil {
		return "", err
	}
	exe, err := proc.Exe()
	if err != nil {
		return "", err
	}
	if !t.same(p) {
		return "", errNotSameProcess
	}
	return exe, nil
}

func (t *osProcessTable) Cmdline(p client.ProcInfo) (string, error) {
	if !t.live() || !readable(p) {
		return "", errRefused
	}
	proc, err := process.NewProcess(p.PID)
	if err != nil {
		return "", err
	}
	line, err := proc.Cmdline()
	if err != nil {
		return "", err
	}
	if !t.same(p) {
		return "", errNotSameProcess
	}
	return line, nil
}

// Signal checks the target before any system call (validateTarget), re-reads its start token, and only then signals
// the group it leads and the pid itself.
func (t *osProcessTable) Signal(p client.ProcInfo, s client.Sig) error {
	if err := validateTarget(p, safePID(os.Getpid()), safePID(os.Getppid())); err != nil {
		return err
	}
	if !t.live() || (s != client.SigTerm && s != client.SigKill) {
		return errRefused
	}
	if !t.same(p) {
		return errNotSameProcess
	}
	return signalGroupAndPID(int(p.PID), s)
}

// Alive reports whether p.PID still names the process p describes.
func (t *osProcessTable) Alive(p client.ProcInfo) bool {
	if !t.live() {
		return false
	}
	return t.same(p)
}

func (t *osProcessTable) same(p client.ProcInfo) bool {
	if !t.live() || !readable(p) {
		return false
	}
	tok, err := startToken(p.PID)
	return err == nil && tok == p.Start
}

func readable(p client.ProcInfo) bool { return p.PID > 0 && p.Start != 0 }

// validateTarget is the refusal every signal passes first. On unix kill(-1) reaches every process the user may
// signal and kill(0) the caller's own group, so pid <= 1 is refused before anything else.
func validateTarget(p client.ProcInfo, self, parent int32) error {
	switch {
	case p.PID <= 1:
		return fmt.Errorf("%w: pid %d", errRefused, p.PID)
	case p.PID == self:
		return fmt.Errorf("%w: pid %d is this process", errRefused, p.PID)
	case p.PID == parent:
		return fmt.Errorf("%w: pid %d is this process's parent", errRefused, p.PID)
	case p.Start == 0:
		return fmt.Errorf("%w: pid %d has no start token", errRefused, p.PID)
	}
	return nil
}

// groupSignalTarget is signalGroupAndPID's first check, kept pure so that it can be tested without sending anything:
// pid 1 or less is never signalled, because on unix kill(-1) reaches every process the user may signal and kill(0)
// the caller's own group.
func groupSignalTarget(pid int) error {
	if pid <= 1 {
		return fmt.Errorf("%w: pid %d", errRefused, pid)
	}
	return nil
}

// safePID narrows a pid to int32; anything out of range becomes -1, which every check refuses.
func safePID(n int) int32 {
	const maxInt32 = int(^uint32(0) >> 1)
	if n > maxInt32 || n < 0 {
		return -1
	}
	return int32(n)
}
