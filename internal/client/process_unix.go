//go:build !linux && (darwin || freebsd || openbsd || netbsd)

// Package client provides functionality for the client subsystem.
package client

import (
	"os/exec"
	"syscall"
)

// prepareCommand puts a sub-server in its own process group. Ending it is killProcessGroup (own_child_unix.go).
func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}
