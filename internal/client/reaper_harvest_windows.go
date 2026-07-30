//go:build windows

// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"log/slog"
	"os/exec"
	"strconv"
)

// harvestProcess gracefully terminates a process by PID utilizing Win32 taskkill natively.
func (m *WarmRegistry) harvestProcess(_ context.Context, name string, pid int) {
	slog.Warn("lifecycle: harvesting existing process instance natively across Windows", "server", name, "pid", pid)

	// Taskkill inherently bypasses POSIX signal trees sweeping Windows hierarchies cleanly.
	killCmd := exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	if err := killCmd.Run(); err != nil {
		slog.Debug("lifecycle: taskkill execution failed", "pid", pid, "error", err)
	}
}
