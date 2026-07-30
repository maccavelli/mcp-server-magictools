//go:build !linux && (darwin || freebsd || openbsd || netbsd)

// Package client provides functionality for the client subsystem.
package client

import (
	"os/exec"
	"syscall"
	"time"
)

func prepareCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
}

func killProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return killPIDGroup(cmd.Process.Pid)
}

func killPIDGroup(pid int) error {
	// Send SIGTERM first for graceful shutdown, matching Linux kill discipline.
	_ = syscall.Kill(-pid, syscall.SIGTERM) //nolint:errcheck // best-effort group signal
	_ = syscall.Kill(pid, syscall.SIGTERM)  //nolint:errcheck // fallback for individual process

	// 🛡️ HARDEN-5: Poll for process exit before escalating to SIGKILL.
	// If the process exits cleanly after SIGTERM, the goroutine returns immediately
	// instead of lingering for the full grace period.
	go func() {
		const gracePeriod = 2 * time.Second
		const pollInterval = 100 * time.Millisecond
		deadline := time.Now().Add(gracePeriod)
		for time.Now().Before(deadline) {
			if err := syscall.Kill(pid, 0); err != nil {
				return // Process already gone, no SIGKILL needed
			}
			time.Sleep(pollInterval)
		}
		_ = syscall.Kill(-pid, syscall.SIGKILL) //nolint:errcheck // best-effort group signal
		_ = syscall.Kill(pid, syscall.SIGKILL)  //nolint:errcheck // fallback for individual process
	}()

	return nil
}
