//go:build !windows

package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func init() {
	if os.Getenv("CLEANUP_HOLD_LOCK") != "1" {
		return
	}
	f, err := os.OpenFile(os.Getenv("CLEANUP_LOCK_PATH"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		os.Exit(2)
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		os.Exit(3)
	}
	time.Sleep(30 * time.Second)
	os.Exit(0)
}

func TestCleanupLockHolderNotSignalled_0016(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "LOCK.magic")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), "CLEANUP_HOLD_LOCK=1", "CLEANUP_LOCK_PATH="+lockPath)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && !storeLockHeld(tmpDir) {
		time.Sleep(20 * time.Millisecond)
	}
	if !storeLockHeld(tmpDir) {
		t.Fatal("helper did not take the store lock")
	}

	pidFile := filepath.Join(tmpDir, "server.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cleanupStaleProcess(tmpDir); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Process.Signal(unix.Signal(0)); err != nil {
		t.Fatal("cleanup signalled the process that holds the store lock")
	}
}
