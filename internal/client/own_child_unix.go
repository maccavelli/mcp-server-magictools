//go:build linux || darwin || freebsd || openbsd || netbsd

package client

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// killProcessGroup ends a child this registry started (S3; 0010-MADR, D3). It never goes through the process table:
// the registry ends its own children even when it has no table.
//
// It signals nothing once the child's Wait has returned (exited is closed), because the pid may then be another
// process's. The leader is signalled through its own handle, which reports os.ErrProcessDone after Wait; the group the
// child leads is signalled only when that succeeded. The escalation waits on exited, not on the pid: KILL follows grace
// only if the child has not exited by then, again through the handle first.
func killProcessGroup(proc *os.Process, exited <-chan struct{}, grace time.Duration) error {
	if proc == nil || isClosed(exited) {
		return nil
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
	_ = syscall.Kill(-proc.Pid, syscall.SIGTERM) //nolint:errcheck // the child's own group; the leader answered just now

	go func() {
		timer := time.NewTimer(grace)
		defer timer.Stop()
		select {
		case <-exited:
			return
		case <-timer.C:
		}
		if err := proc.Signal(syscall.SIGKILL); err != nil {
			return // exited meanwhile: nothing more to end
		}
		_ = syscall.Kill(-proc.Pid, syscall.SIGKILL) //nolint:errcheck // stragglers of the child's own group
	}()
	return nil
}
