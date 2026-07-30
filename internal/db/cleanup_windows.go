//go:build windows

// Package db provides functionality for the db subsystem.
package db

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/windows"
)

var dbLockFile *os.File

// AcquireLock is undocumented but satisfies standard structural requirements.
func AcquireLock(path string) error {
	lockPath := filepath.Join(path, "LOCK.magic")

	// Open exclusively preventing concurrent reads via Windows filesystem bounds natively
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0666)
	if err != nil {
		return fmt.Errorf("database is locked by another instance (open error): %w", err)
	}

	h := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err != nil {
		f.Close()
		return fmt.Errorf("database is locked by another instance: %w", err)
	}

	dbLockFile = f
	return nil
}

func releaseOSLock(f *os.File) error {
	h := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(h, 0, 1, 0, &overlapped)
}

func cleanupStaleProcess(path string) error {
	pidFile := filepath.Join(path, "server.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return nil
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return nil // Ignore corrupt pid file natively
	}

	// Taskkill explicitly forcefully shutting down previous orchestrator trees on Windows perfectly
	killCmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(pid))
	if err := killCmd.Run(); err == nil {
		slog.Warn("Stale Windows instance detected and killed", "pid", pid)
	}

	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		slog.Warn("Failed to remove PID file", "path", pidFile, "error", err)
	}
	return nil
}
