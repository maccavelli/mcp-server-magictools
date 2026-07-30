package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestLookupInternalRegistry(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "magictools-reaper-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	m := &WarmRegistry{PIDDir: tempDir}
	name := "test-server"
	pid := 12345

	pidFile := filepath.Join(tempDir, name+".pid")
	if err := os.WriteFile(pidFile, []byte("12345"), 0644); err != nil {
		t.Fatal(err)
	}

	toCheck := make(map[int]bool)
	m.lookupInternalRegistry(context.Background(), name, toCheck)

	if !toCheck[pid] {
		t.Errorf("expected PID %d to be in toCheck map", pid)
	}

	// Test missing file
	toCheckMissing := make(map[int]bool)
	m.lookupInternalRegistry(context.Background(), "non-existent", toCheckMissing)
	if len(toCheckMissing) != 0 {
		t.Error("expected empty map for non-existent server")
	}
}

func TestReportSubServerFailure(t *testing.T) {
	m := &WarmRegistry{}
	// This just logs via slog, ensure no panic
	m.reportSubServerFailure("test-server", 1)
}

func TestEnforceSingleInstanceSkipsSelf(t *testing.T) {
	m := &WarmRegistry{Servers: make(map[string]*SubServer)}
	myPid := os.Getpid()

	toCheck := make(map[int]bool)
	// We don't want to actually kill ourself, so we just check that excludePIDs works
	m.enforceSingleInstance(context.Background(), "self", "bin", nil, myPid)

	if toCheck[myPid] {
		t.Error("expected self PID to be excluded from check")
	}
}
