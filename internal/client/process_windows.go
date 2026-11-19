//go:build windows

// Package client provides functionality for the client subsystem.
package client

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
	}
}

// killProcessGroup ends a child this registry started (S3; 0010-MADR, D3). Nothing is sent once the child's Wait has
// returned (exited is closed). The tree goes through taskkill while the child is not yet reaped; the leader through
// its own handle, which Windows keeps open, so it cannot reach a process that reused the pid.
func killProcessGroup(proc *os.Process, exited <-chan struct{}, _ time.Duration) error {
	if proc == nil || isClosed(exited) {
		return nil
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(proc.Pid)).Run() //nolint:gosec,errcheck // own child's tree
	if err := proc.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return err
	}
	return nil
}
