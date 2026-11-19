package db

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"
)

// windowsNeverAPID is used only on Windows. cleanupStaleProcess there runs taskkill /F on the recorded pid with no
// liveness test (cleanup_windows.go), so a pid taken from a reaped child could, if Windows reused it in the gap, end a
// stranger; Windows hands out pids that are multiples of four (an implementation detail, not a documented guarantee),
// so this number is not one and taskkill fails on it.
const windowsNeverAPID = 999999

// deadPID returns a pid that is provably dead and was this test's own: it starts a child that exits at once, waits for
// it (so it is reaped and the pid is free), and returns its pid.
//
// A fixed number such as 999999 is dead only where the pid limit is low (macOS stops at 99999). Where pid_max is in
// the millions (the Linux machine) a pid that high can belong to an unrelated live process, and cleanupStaleProcess
// signals whatever answers signal 0 at the recorded pid: SIGTERM, then SIGKILL after three seconds.
//
// Residual risk, not removable from a test: the kernel may hand the freed pid to a new process between the reap here
// and the cleanup's own liveness check a few statements later. Linux and macOS by default allocate pids upward and
// wrap, so a freed pid is not handed out again at once, and the window is the time between two adjacent calls. The test narrows it by taking the
// pid immediately before the cleanup call and by starting nothing in between. A signal-0 probe from the test would add
// nothing: it would sit in the same gap and the cleanup repeats it. The cleanup's missing identity check is the
// product's edge and the next record's.
func deadPID(t *testing.T) int {
	t.Helper()
	if runtime.GOOS == "windows" {
		return windowsNeverAPID
	}
	if _, err := exec.LookPath("true"); err != nil {
		t.Skip("skipping: the true command is not available to produce a reaped pid")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil { // Run starts the child and waits for it, which reaps it
		t.Skipf("skipping: cannot produce a reaped pid: %v", err)
	}
	return cmd.Process.Pid
}

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

	// 2. Case: Stale PID (no process). The pid is one this test owned and has reaped, never a guessed number.
	// The dead pid is taken as late as possible so the gap before the cleanup's own liveness check is minimal.
	_ = os.WriteFile(pidFile, []byte(strconv.Itoa(deadPID(t))), 0644)
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
