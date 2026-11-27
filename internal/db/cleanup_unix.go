//go:build !windows

// Package db provides functionality for the db subsystem.
package db

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
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

// storeLockHeld reports whether another process holds LOCK.magic.
// A lock this process acquires during the probe is released. A probe that
// cannot be made is treated as held, so the caller does not signal.
func storeLockHeld(path string) bool {
	f, err := os.Open(filepath.Join(path, "LOCK.magic")) //nolint:gosec // lock path is under the store directory
	if err != nil {
		return !os.IsNotExist(err)
	}
	defer closeFileOrWarn(f, "lock-probe")
	err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
	if err != nil {
		return true
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_UN); err != nil {
		slog.Warn("failed to release lock probe", "error", err)
		return true
	}
	return false
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

	rec, ok := parsePIDFile(data)
	if !ok {
		return nil // Ignore corrupt pid file
	}

	selfExe, err := selfExecutable()
	if err != nil {
		selfExe = ""
	}
	alive := false
	var process *os.Process
	if found, err := os.FindProcess(rec.PID); err == nil {
		process = found
		alive = process.Signal(syscall.Signal(0)) == nil
	}
	exe, exeErr := "", error(nil)
	if alive {
		exe, exeErr = lookupExecutable(rec.PID)
	}
	switch decideCleanup(rec.PID, os.Getpid(), storeLockHeld(path), alive, exe, selfExe, exeErr) {
	case cleanupLeave:
		return nil
	case cleanupRemove:
		return removePIDFile(pidFile)
	default:
	}
	if process == nil {
		return nil
	}

	// The pid is alive, is not us, does not hold the lock, and its executable matches.
	slog.Warn("Stale instance detected; sending SIGTERM", "pid", rec.PID)
	if err := process.Signal(syscall.SIGTERM); err != nil {
		slog.Warn("Failed to send SIGTERM to stale instance", "pid", rec.PID, "error", err)
	}

	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case <-ticker.C:
			if err := process.Signal(syscall.Signal(0)); err != nil {
				slog.Info("Stale instance exited successfully", "pid", rec.PID)
				return removePIDFile(pidFile)
			}
		case <-timeout.C:
			slog.Warn("Stale instance did not exit in time; enforcing hard shutdown (SIGKILL)", "pid", rec.PID)
			if err := process.Signal(syscall.SIGKILL); err != nil {
				slog.Warn("Failed to send SIGKILL to stale instance", "pid", rec.PID, "error", err)
			}
			for range 10 {
				time.Sleep(100 * time.Millisecond)
				if err := process.Signal(syscall.Signal(0)); err != nil {
					slog.Info("Stale instance successfully killed", "pid", rec.PID)
					break
				}
			}
			return removePIDFile(pidFile)
		}
	}
}
