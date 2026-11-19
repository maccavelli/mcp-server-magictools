//go:build linux || darwin || freebsd || openbsd || netbsd

package client

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// S3, the registry's own child (0010, D3; P4a step 5). These tests start a child of their own (a shell running sleep,
// in its own process group as a sub-server is) and signal only that child, through its own handle. They read no other
// process and signal no other process. They run only on the Linux machine (the plan's rule for this package).

type ownChild struct {
	cmd    *exec.Cmd
	exited chan struct{}
	err    chan error
}

func startOwnChild(t *testing.T, script string) *ownChild {
	t.Helper()
	cmd := exec.Command("sh", "-c", script)
	prepareCommand(cmd) // its own process group, as every sub-server
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	c := &ownChild{cmd: cmd, exited: make(chan struct{}), err: make(chan error, 1)}
	go func() {
		c.err <- cmd.Wait()
		close(c.exited)
	}()
	t.Cleanup(func() {
		select {
		case <-c.exited:
		default:
			_ = cmd.Process.Kill() // our own child, through its handle
			<-c.exited
		}
	})
	return c
}

func (c *ownChild) alive() bool {
	select {
	case <-c.exited:
		return false
	default:
	}
	return c.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (c *ownChild) waitSignal(t *testing.T, within time.Duration) syscall.Signal {
	t.Helper()
	select {
	case <-c.exited:
	case <-time.After(within):
		t.Fatalf("the child did not exit within %v", within)
	}
	ws, ok := c.cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !ws.Signaled() {
		t.Fatalf("the child did not end by a signal: %v", c.cmd.ProcessState)
	}
	return ws.Signal()
}

// TestSeamOwnChildAlreadyReapedIsNotSignalled: once the child's Wait has returned (the exited channel is closed), its
// pid may belong to another process; nothing is sent. The old code signalled the raw pid regardless. Simulated with the
// seam: the channel is closed while the child is in fact still running, and it must survive.
func TestSeamOwnChildAlreadyReapedIsNotSignalled(t *testing.T) {
	c := startOwnChild(t, "exec sleep 30")
	reaped := make(chan struct{})
	close(reaped)
	if err := killProcessGroup(c.cmd.Process, reaped, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if !c.alive() {
		t.Fatal("a child reported as already reaped was signalled")
	}
}

func TestSeamOwnChildTermEndsIt(t *testing.T) {
	c := startOwnChild(t, "exec sleep 30")
	if err := killProcessGroup(c.cmd.Process, c.exited, 2*time.Second); err != nil {
		t.Fatal(err)
	}
	if sig := c.waitSignal(t, 5*time.Second); sig != syscall.SIGTERM {
		t.Fatalf("the child ended by %v, want SIGTERM", sig)
	}
}

func TestSeamOwnChildEscalatesToKill(t *testing.T) {
	c := startOwnChild(t, `trap "" TERM; exec sleep 30`)
	time.Sleep(100 * time.Millisecond) // let the shell install the trap before exec
	start := time.Now()
	if err := killProcessGroup(c.cmd.Process, c.exited, 300*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if sig := c.waitSignal(t, 5*time.Second); sig != syscall.SIGKILL {
		t.Fatalf("the child ended by %v, want SIGKILL", sig)
	}
	if d := time.Since(start); d < 300*time.Millisecond {
		t.Fatalf("KILL came after %v, before the grace period", d)
	}
}

// TestSeamStopOwnChildOnlyTracksItsOwn: the registry signals a command only while it holds that command's exit
// channel; an untracked command (never started by it, or already reaped) is left alone.
func TestSeamStopOwnChildOnlyTracksItsOwn(t *testing.T) {
	m := &WarmRegistry{}
	if err := m.stopOwnChild(nil); err != nil {
		t.Fatal(err)
	}
	untracked := startOwnChild(t, "exec sleep 30")
	if err := m.stopOwnChild(untracked.cmd); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if !untracked.alive() {
		t.Fatal("an untracked command was signalled")
	}
	tracked := startOwnChild(t, "exec sleep 30")
	m.childExits.Store(tracked.cmd, tracked.exited)
	if err := m.stopOwnChild(tracked.cmd); err != nil {
		t.Fatal(err)
	}
	if sig := tracked.waitSignal(t, 5*time.Second); sig != syscall.SIGTERM {
		t.Fatalf("the tracked child ended by %v, want SIGTERM", sig)
	}
}
