//go:build !windows

package proctable

import (
	"os"
	"os/exec"
	"syscall"
	"testing"
)

// prepareChild makes the helper child the leader of its own process group, so the table's group signal reaches it
// (and only it) as it reaches a sub-server.
func prepareChild(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func assertKilledByKill(t *testing.T, ps *os.ProcessState) {
	t.Helper()
	ws, ok := ps.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() || ws.Signal() != syscall.SIGKILL {
		t.Fatalf("the child did not end by SIGKILL: %v", ps)
	}
}
