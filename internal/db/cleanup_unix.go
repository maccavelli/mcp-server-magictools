//go:build !windows

// Package db provides functionality for the db subsystem.
package db

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var dbLockFile *os.File

// AcquireLock is undocumented but satisfies standard structural requirements.
func AcquireLock(path string) error {
	lockPath := filepath.Join(path, "LOCK.magic")
	f, err := os.OpenFile(filepath.Clean(lockPath), os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // lock path is under controlled database directory
	if err != nil {
		return err
	}

	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		closeFileOrWarn(f, "lock")
		return fmt.Errorf("database is locked by another instance: %w", err)
	}

	dbLockFile = f
	return nil
}

func releaseOSLock(f *os.File) error {
	return unix.Flock(int(f.Fd()), unix.LOCK_UN)
}

func cleanupStaleProcess(path string) error {
	pidFile := filepath.Join(path, "server.pid")
	data, err := os.ReadFile(filepath.Clean(pidFile)) //nolint:gosec // pid path is under controlled database directory
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return nil // Ignore corrupt pid file
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	// Sending signal 0 just checks if process is alive
	if err := process.Signal(syscall.Signal(0)); err == nil {
		slog.Warn("Stale instance detected; sending SIGTERM", "pid", pid)
		if err := process.Signal(syscall.SIGTERM); err != nil {
			slog.Warn("Failed to send SIGTERM to stale instance", "pid", pid, "error", err)
		}

		// Wait up to 3 seconds for it to exit
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		timeout := time.NewTimer(3 * time.Second)
		defer timeout.Stop()

		for {
			select {
			case <-ticker.C:
				if err := process.Signal(syscall.Signal(0)); err != nil {
					slog.Info("Stale instance exited successfully", "pid", pid)
					if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
						slog.Warn("Failed to remove stale PID file", "path", pidFile, "error", err)
					}
					return nil
				}
			case <-timeout.C:
				slog.Warn("Stale instance did not exit in time; enforcing hard shutdown (SIGKILL)", "pid", pid)
				if err := process.Signal(syscall.SIGKILL); err != nil {
					slog.Warn("Failed to send SIGKILL to stale instance", "pid", pid, "error", err)
				}
				// 🛡️ CRITICAL: Wait for the OS to actually kill the process and release file locks
				for range 10 {
					time.Sleep(100 * time.Millisecond)
					if err := process.Signal(syscall.Signal(0)); err != nil {
						slog.Info("Stale instance successfully killed", "pid", pid)
						break
					}
				}
				if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
					slog.Warn("Failed to remove stale PID file after SIGKILL", "path", pidFile, "error", err)
				}
				return nil
			}
		}
	}

	if err := os.Remove(pidFile); err != nil && !os.IsNotExist(err) {
		slog.Warn("Failed to remove PID file", "path", pidFile, "error", err)
	}
	return nil
}
