//go:build !windows

package proctable

import (
	"syscall"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
)

// signalGroupAndPID sends the signal to the group pid leads and to pid, as internal/client's killPIDGroup did for
// foreign pids. The group signal is best effort: a pid that leads no group answers ESRCH.
func signalGroupAndPID(pid int, s client.Sig) error {
	if err := groupSignalTarget(pid); err != nil {
		return err
	}
	sig := syscall.SIGTERM
	if s == client.SigKill {
		sig = syscall.SIGKILL
	}
	_ = syscall.Kill(-pid, sig) //nolint:errcheck // best-effort group signal
	return syscall.Kill(pid, sig)
}
