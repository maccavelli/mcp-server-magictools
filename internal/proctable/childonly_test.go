package proctable

import (
	"os"
	"os/exec"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
)

// childOnlyTable is the only real process table a test may hold (0010-MADR-make-tests-unable-to-signal-real-processes.md,
// D2). It wraps the operating system's table and is scoped to one child the test itself started, given as the
// *exec.Cmd that started it: every read (Environ, Exe, Cmdline), every liveness check and every signal is refused
// unless the pid is that child's and a fresh snapshot shows its parent is this test process. Snapshot (pids, parents
// and start tokens only) and Self pass through: they read and change nothing.
//
// The guard holds it to that shape: a literal of osProcessTable only here (rule A3); in this package's tests no
// reference to signalGroupAndPID or startToken, .inner only in this type's methods, newChildOnlyTable taking the
// *exec.Cmd, and each of the five methods beginning with the isOwnChild check (rule A4).
type childOnlyTable struct {
	inner *osProcessTable
	cmd   *exec.Cmd // nil permits nothing but Snapshot and Self
}

var _ client.ProcessTable = (*childOnlyTable)(nil)

// newChildOnlyTable returns a table scoped to the child cmd started; a nil cmd permits only Snapshot and Self.
func newChildOnlyTable(t *testing.T, cmd *exec.Cmd) *childOnlyTable {
	t.Helper()
	return &childOnlyTable{inner: &osProcessTable{armed: true}, cmd: cmd}
}

// isOwnChild reports whether p is the child this table was given, and a fresh snapshot shows this test process as its
// parent.
func (c *childOnlyTable) isOwnChild(p client.ProcInfo) bool {
	if c.cmd == nil || c.cmd.Process == nil || p.PID <= 1 || int(p.PID) != c.cmd.Process.Pid {
		return false
	}
	snap, err := c.inner.Snapshot()
	if err != nil {
		return false
	}
	me := safePID(os.Getpid())
	for _, row := range snap {
		if row.PID == p.PID {
			return row.PPID == me
		}
	}
	return false
}

func (c *childOnlyTable) Snapshot() ([]client.ProcInfo, error) { return c.inner.Snapshot() }
func (c *childOnlyTable) Self() (client.SelfInfo, error)       { return c.inner.Self() }

func (c *childOnlyTable) Environ(p client.ProcInfo) ([]string, error) {
	if !c.isOwnChild(p) {
		return nil, errRefused
	}
	return c.inner.Environ(p)
}

func (c *childOnlyTable) Exe(p client.ProcInfo) (string, error) {
	if !c.isOwnChild(p) {
		return "", errRefused
	}
	return c.inner.Exe(p)
}

func (c *childOnlyTable) Cmdline(p client.ProcInfo) (string, error) {
	if !c.isOwnChild(p) {
		return "", errRefused
	}
	return c.inner.Cmdline(p)
}

func (c *childOnlyTable) Alive(p client.ProcInfo) bool {
	if !c.isOwnChild(p) {
		return false
	}
	return c.inner.Alive(p)
}

// Signal reaches the child only, and only while it is still this test process's direct child.
func (c *childOnlyTable) Signal(p client.ProcInfo, s client.Sig) error {
	if !c.isOwnChild(p) {
		return errRefused
	}
	return c.inner.Signal(p, s)
}
