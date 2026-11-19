package client

import (
	"os/exec"
	"time"
)

// ownChildGrace is the wait between TERM and KILL for a child of this registry.
const ownChildGrace = 2 * time.Second

// stopOwnChild ends cmd if, and only if, this registry started it and its Wait has not returned (S3; 0010-MADR, D3).
// spawnProcess records each child's exit channel in childExits and its Wait goroutine closes and removes it, so a
// command the map no longer holds (never started here, or already reaped) is never signalled.
func (m *WarmRegistry) stopOwnChild(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	v, ok := m.childExits.Load(cmd)
	if !ok {
		return nil
	}
	exited, ok := v.(chan struct{})
	if !ok {
		return nil
	}
	return killProcessGroup(cmd.Process, exited, ownChildGrace)
}

// isClosed reports whether ch is closed; a nil channel is never closed.
func isClosed(ch <-chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}
