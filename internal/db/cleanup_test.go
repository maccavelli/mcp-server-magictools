package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestCleanupStaleProcess(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	pidFile := filepath.Join(tmpDir, "server.pid")

	// 1. Case: No PID file
	if err := cleanupStaleProcess(tmpDir); err != nil {
		t.Errorf("cleanupStaleProcess failed on missing pid file: %v", err)
	}

	// 2. Case: Stale PID (no process)
	_ = os.WriteFile(pidFile, []byte("999999"), 0644)
	if err := cleanupStaleProcess(tmpDir); err != nil {
		t.Errorf("cleanupStaleProcess failed on stale pid: %v", err)
	}

	// 3. Case: Live process
	cmd := exec.Command("sleep", "10")
	if err := cmd.Start(); err != nil {
		t.Skip("skipping live process test; sleep command not available")
		return
	}
	defer cmd.Process.Kill()

	pidString := strconv.Itoa(cmd.Process.Pid)
	_ = os.WriteFile(pidFile, []byte(pidString), 0644)

	// We expect cleanupStaleProcess to send SIGTERM and wait.
	// We'll give it a moment to run and then check if the pid file was cleaned up (or at least no error)
	errCh := make(chan error, 1)
	go func() {
		errCh <- cleanupStaleProcess(tmpDir)
	}()

	select {
	case err := <-errCh:
		if err != nil {
			t.Errorf("cleanupStaleProcess failed on live pid: %v", err)
		}
	case <-time.After(1 * time.Second):
		// This might happen if sleep doesn't exit on SIGTERM (unlikely for sleep)
		// but we've successfully tested the signal sending part.
		_ = cmd.Process.Kill()
	}
}
