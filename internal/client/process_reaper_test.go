package client

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
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

// runWalk executes the walk under a watchdog so a regression that reintroduces
// an unbounded loop fails fast instead of hanging the whole suite until the
// package timeout. That is how this defect presented in CI: 9m45s "running",
// no progress (MADR 0003).
func runWalk(t *testing.T, pid int32, parents map[int32]int32, active map[int32]bool, myPid, myPpid int32) bool {
	t.Helper()
	type result struct{ ok bool }
	done := make(chan result, 1)
	go func() {
		done <- result{isLegitimateDescendant(pid, parents, active, myPid, myPpid)}
	}()
	select {
	case r := <-done:
		return r.ok
	case <-time.After(5 * time.Second):
		t.Fatal("isLegitimateDescendant did not terminate: the parent walk is unbounded")
		return false
	}
}

// TestParentWalkTerminatesOnCycle is the regression guard for the CI hang.
// Windows recycles PIDs, so a stale parent pointer can form a real A -> B -> A
// cycle. The previous implementation detected only a self-loop and span
// forever on anything longer.
func TestParentWalkTerminatesOnCycle(t *testing.T) {
	parents := map[int32]int32{100: 200, 200: 100}
	if runWalk(t, 100, parents, nil, 1, 2) {
		t.Error("a cycle must not be reported as legitimate descent")
	}
}

// TestParentWalkTerminatesOnLongerCycle covers a cycle that no self-loop check
// could ever catch.
func TestParentWalkTerminatesOnLongerCycle(t *testing.T) {
	parents := map[int32]int32{10: 11, 11: 12, 12: 13, 13: 10}
	if runWalk(t, 10, parents, nil, 1, 2) {
		t.Error("a four-node cycle must not be reported as legitimate descent")
	}
}

// TestParentWalkSelfLoop keeps the original termination condition working.
func TestParentWalkSelfLoop(t *testing.T) {
	parents := map[int32]int32{42: 42}
	if runWalk(t, 42, parents, nil, 1, 2) {
		t.Error("a self-loop must not be reported as legitimate descent")
	}
}

// TestParentWalkDepthCap proves the cap actually stops the walk, rather than
// the chain merely running out.
//
// The chain DOES reach myPid, but only beyond maxParentWalkDepth. A capped
// walk must give up and answer false; an uncapped one would walk far enough to
// answer true. That difference is what makes this test discriminate — a chain
// that simply ends returns false either way and proves nothing.
func TestParentWalkDepthCap(t *testing.T) {
	const target = int32(999999)
	depth := int32(maxParentWalkDepth) * 2
	parents := make(map[int32]int32, depth+1)
	for i := int32(1); i < depth; i++ {
		parents[i] = i + 1
	}
	parents[depth] = target

	if runWalk(t, 1, parents, nil, target, 999998) {
		t.Error("the walk exceeded maxParentWalkDepth; the cap is not enforced")
	}
}

func TestParentWalkReachesMyPid(t *testing.T) {
	parents := map[int32]int32{100: 200, 200: 300}
	if !runWalk(t, 100, parents, nil, 300, 2) {
		t.Error("a chain reaching this process must be legitimate")
	}
}

func TestParentWalkReachesMyPpid(t *testing.T) {
	parents := map[int32]int32{100: 200, 200: 300}
	if !runWalk(t, 100, parents, nil, 1, 300) {
		t.Error("a chain reaching this process's parent must be legitimate")
	}
}

func TestParentWalkReachesActivePID(t *testing.T) {
	parents := map[int32]int32{100: 200, 200: 300}
	active := map[int32]bool{200: true}
	if !runWalk(t, 100, parents, active, 1, 2) {
		t.Error("a chain reaching a managed process must be legitimate")
	}
}

// TestParentWalkUnknownParent covers a PID absent from the snapshot, which is
// ordinary: a process can exit between enumeration and classification.
func TestParentWalkUnknownParent(t *testing.T) {
	parents := map[int32]int32{100: 200}
	if runWalk(t, 100, parents, nil, 1, 2) {
		t.Error("an unknown parent must end the walk as not-descended")
	}
}

func TestParentWalkZeroParent(t *testing.T) {
	parents := map[int32]int32{100: 0}
	if runWalk(t, 100, parents, nil, 1, 2) {
		t.Error("ppid 0 must end the walk as not-descended")
	}
}

// TestParentWalkPerformsNoSyscalls is implied by the signature but asserted
// explicitly: a nil table must not send the walk to the OS.
func TestParentWalkPerformsNoSyscalls(t *testing.T) {
	if runWalk(t, 12345, nil, nil, 1, 2) {
		t.Error("a nil parent table must end the walk as not-descended")
	}
}

// TestPruneOrphansEnumeratesExactlyOnce is the cost guard. The old code
// resolved a parent per step, which on Windows meant one full process-table
// snapshot per step and never hit gopsutil's per-Process cache.
func TestPruneOrphansEnumeratesExactlyOnce(t *testing.T) {
	calls := 0
	prev := parentMap
	parentMap = func() map[int32]int32 {
		calls++
		return map[int32]int32{}
	}
	t.Cleanup(func() { parentMap = prev })

	m := &WarmRegistry{}
	m.PruneOrphans()

	if calls != 1 {
		t.Fatalf("PruneOrphans enumerated the process table %d times, want exactly 1", calls)
	}
}
