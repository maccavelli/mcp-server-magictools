//go:build windows

package proctable

import (
	"os"
	"os/exec"
	"testing"
)

func prepareChild(*exec.Cmd) {}

func assertKilledByKill(t *testing.T, ps *os.ProcessState) {
	t.Helper()
	if ps == nil || ps.Success() {
		t.Fatalf("the child did not end by a kill: %v", ps)
	}
}
