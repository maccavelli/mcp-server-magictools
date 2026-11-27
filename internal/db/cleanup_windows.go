//go:build windows

// Package db provides functionality for the db subsystem.
package db

import (
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"

	"github.com/shirou/gopsutil/v4/process"
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

// storeLockHeld reports whether another process holds LOCK.magic.
// A lock this process acquires during the probe is released. A probe that
// cannot be made is treated as held, so the caller does not signal.
func storeLockHeld(path string) bool {
	f, err := os.Open(filepath.Join(path, "LOCK.magic"))
	if err != nil {
		return !os.IsNotExist(err)
	}
	defer f.Close()
	h := windows.Handle(f.Fd())
	var overlapped windows.Overlapped
	err = windows.LockFileEx(h, windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
	if err != nil {
		return true
	}
	_ = windows.UnlockFileEx(h, 0, 1, 0, &overlapped)
	return false
}

func cleanupStaleProcess(path string) error {
	pidFile := filepath.Join(path, "server.pid")
	data, err := os.ReadFile(pidFile) //nolint:gosec // pid path is under the store directory
	if err != nil {
		return nil
	}

	rec, ok := parsePIDFile(data)
	if !ok {
		return nil
	}

	selfExe, err := selfExecutable()
	if err != nil {
		selfExe = ""
	}
	alive, exe, exeErr := false, "", error(nil)
	if rec.PID <= math.MaxInt32 {
		if found, err := process.NewProcess(int32(rec.PID)); err == nil { //nolint:gosec // range checked above
			running, runErr := found.IsRunning()
			alive = runErr == nil && running
			if alive {
				exe, exeErr = lookupExecutable(rec.PID)
			}
		}
	}
	switch decideCleanup(rec.PID, os.Getpid(), storeLockHeld(path), alive, exe, selfExe, exeErr) {
	case cleanupLeave:
		return nil
	case cleanupRemove:
		return removePIDFile(pidFile)
	default:
	}

	// Alive, not us, lock not held, executable matches. /PID, not a tree kill.
	killCmd := exec.Command("taskkill", "/F", "/PID", strconv.Itoa(rec.PID))
	if err := killCmd.Run(); err == nil {
		slog.Warn("Stale Windows instance detected and killed", "pid", rec.PID)
	}

	return removePIDFile(pidFile)
}
