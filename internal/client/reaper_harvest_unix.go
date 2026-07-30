//go:build !windows

// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"log/slog"
	"os"
	"syscall"
	"time"
)

// harvestProcess gracefully terminates a process by PID using SIGTERM→SIGKILL escalation.
func (m *WarmRegistry) harvestProcess(_ context.Context, name string, pid int) {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return
	}
	// Verify process exists and we can signal it
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return
	}

	slog.Warn("lifecycle: harvesting existing process instance", "server", name, "pid", pid)
	// SIGTERM first for graceful shutdown
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		slog.Debug("lifecycle: SIGTERM to process group failed", "pid", pid, "error", err)
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		slog.Debug("lifecycle: SIGTERM to process failed", "pid", pid, "error", err)
	}

	// Block until dead or timeout (2s)
	start := time.Now()
	for time.Since(start) < 2*time.Second {
		if err := proc.Signal(syscall.Signal(0)); err != nil {
			if releaseErr := proc.Release(); releaseErr != nil {
				slog.Debug("lifecycle: failed to release process resources", "pid", pid, "error", releaseErr)
			}
			return // Process is gone
		}
		time.Sleep(100 * time.Millisecond)
	}

	// Escalate to SIGKILL if still alive
	if err := proc.Signal(syscall.Signal(0)); err == nil {
		slog.Warn("lifecycle: SIGTERM ignored, escalating to SIGKILL", "server", name, "pid", pid)
		if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
			slog.Debug("lifecycle: SIGKILL to process group failed", "pid", pid, "error", err)
		}
		if err := syscall.Kill(pid, syscall.SIGKILL); err != nil {
			slog.Debug("lifecycle: SIGKILL to process failed", "pid", pid, "error", err)
		}
	}
	if releaseErr := proc.Release(); releaseErr != nil {
		slog.Debug("lifecycle: failed to release process resources at end of harvest", "pid", pid, "error", releaseErr)
	}
}
