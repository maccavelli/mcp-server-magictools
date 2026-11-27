package db

import (
	"errors"
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

	// 3. A live process whose executable is not this binary (S7). It must not be signalled,
	// and the pid file is removed so a stranger's pid does not block the next open.
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("skipping live process test; sleep command not available")
		return
	}
	defer cmd.Process.Kill()

	pidString := strconv.Itoa(cmd.Process.Pid)
	_ = os.WriteFile(pidFile, []byte(pidString), 0644)

	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()

	if err := cleanupStaleProcess(tmpDir); err != nil {
		t.Fatalf("cleanupStaleProcess failed on a foreign executable: %v", err)
	}
	select {
	case <-exited:
		t.Fatal("cleanup signalled a live process whose executable is not this binary")
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatal("pid file of a foreign executable was left in place")
	}
}

func TestCleanupUnreadableExecutableLeft_0016(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "cleanup-unread")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Skip("skipping; sleep command not available")
	}
	defer cmd.Process.Kill()

	pidFile := filepath.Join(tmpDir, "server.pid")
	if err := os.WriteFile(pidFile, []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		t.Fatal(err)
	}
	prev := lookupExecutable
	lookupExecutable = func(int) (string, error) { return "", errors.New("unreadable") }
	t.Cleanup(func() { lookupExecutable = prev })

	if err := cleanupStaleProcess(tmpDir); err != nil {
		t.Fatal(err)
	}
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	select {
	case <-exited:
		t.Fatal("cleanup signalled a process whose executable could not be read")
	case <-time.After(200 * time.Millisecond):
	}
	if _, err := os.Stat(pidFile); err != nil {
		t.Fatal("pid file was removed when the executable could not be read")
	}
}
