package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/mcplib/selfupdate"
)

func TestLifecycleDelegates(t *testing.T) {
	var stopped, started bool
	l := &updateLifecycle{
		target:      "/bin/x",
		installedFn: func(context.Context, string) (bool, error) { return true, nil },
		runningFn:   func(context.Context) (bool, error) { return true, nil },
		stopFn:      func(context.Context) error { stopped = true; return nil },
		startFn:     func(context.Context) error { started = true; return nil },
	}
	ctx := context.Background()
	if ok, err := l.Installed(ctx, cmdName); err != nil || !ok {
		t.Fatalf("Installed = %v, %v", ok, err)
	}
	if err := l.Stop(ctx, cmdName); err != nil || !stopped {
		t.Fatalf("Stop = %v", err)
	}
	if err := l.Start(ctx, cmdName); err != nil || !started {
		t.Fatalf("Start = %v", err)
	}
}

// TestLifecycleHonoursCancellation proves an interrupted update stops talking
// to the service manager instead of running to its own deadline.
func TestLifecycleHonoursCancellation(t *testing.T) {
	fail := func(context.Context) error { t.Fatal("called after cancel"); return nil }
	l := &updateLifecycle{
		target:      "/bin/x",
		installedFn: func(context.Context, string) (bool, error) { t.Fatal("called after cancel"); return false, nil },
		runningFn:   func(context.Context) (bool, error) { t.Fatal("called after cancel"); return false, nil },
		stopFn:      fail,
		startFn:     fail,
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := l.Installed(ctx, cmdName); !errors.Is(err, context.Canceled) {
		t.Errorf("Installed err = %v", err)
	}
	if _, err := l.Running(ctx, cmdName); !errors.Is(err, context.Canceled) {
		t.Errorf("Running err = %v", err)
	}
	if err := l.Stop(ctx, cmdName); !errors.Is(err, context.Canceled) {
		t.Errorf("Stop err = %v", err)
	}
	if err := l.Start(ctx, cmdName); !errors.Is(err, context.Canceled) {
		t.Errorf("Start err = %v", err)
	}
}

func TestWaitHealthyReturnsWhenServiceComesUp(t *testing.T) {
	calls := 0
	l := &updateLifecycle{
		runningFn: func(context.Context) (bool, error) { calls++; return calls >= 3, nil },
		Poll:      time.Millisecond,
	}
	if err := l.WaitHealthy(context.Background(), cmdName); err != nil {
		t.Fatalf("WaitHealthy = %v", err)
	}
	if calls < 3 {
		t.Fatalf("polled %d times, expected to wait", calls)
	}
}

// TestWaitHealthyTimesOut is the case that must stay fatal: a service manager
// accepting `start` is not evidence the unit came up.
func TestWaitHealthyTimesOut(t *testing.T) {
	l := &updateLifecycle{
		runningFn:     func(context.Context) (bool, error) { return false, nil },
		Poll:          time.Millisecond,
		HealthTimeout: 20 * time.Millisecond,
	}
	if err := l.WaitHealthy(context.Background(), cmdName); err == nil {
		t.Fatal("expected a health timeout to be fatal")
	}
}

func TestWaitHealthyReportsProbeFailure(t *testing.T) {
	probe := errors.New("systemctl exploded")
	l := &updateLifecycle{
		runningFn:     func(context.Context) (bool, error) { return false, probe },
		Poll:          time.Millisecond,
		HealthTimeout: 20 * time.Millisecond,
	}
	if err := l.WaitHealthy(context.Background(), cmdName); !errors.Is(err, probe) {
		t.Fatalf("error = %v, want it to wrap %v", err, probe)
	}
}

// TestServiceDefinitionTargets is the rule that keeps an update from touching
// a service it does not own: a definition naming ANOTHER binary is reported
// absent, so it is never stopped, reconciled or restarted.
func TestServiceDefinitionTargets(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "mine")
	theirs := filepath.Join(dir, "theirs")
	for _, p := range []string{mine, theirs} {
		if err := os.WriteFile(p, []byte("binary"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	def := filepath.Join(dir, "unit.service")
	if err := os.WriteFile(def, []byte("ExecStart="+mine+" serve\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	prev := serviceDefinitionPathFn
	serviceDefinitionPathFn = func() (string, bool) { return def, true }
	t.Cleanup(func() { serviceDefinitionPathFn = prev })

	if ok, err := serviceDefinitionTargets(mine); err != nil || !ok {
		t.Errorf("a definition naming this binary must be Installed: %v %v", ok, err)
	}
	if ok, err := serviceDefinitionTargets(theirs); err != nil || ok {
		t.Errorf("a definition naming ANOTHER binary must read as absent: %v %v", ok, err)
	}
}

func TestServiceDefinitionAbsent(t *testing.T) {
	prev := serviceDefinitionPathFn
	serviceDefinitionPathFn = func() (string, bool) { return filepath.Join(t.TempDir(), "missing"), true }
	t.Cleanup(func() { serviceDefinitionPathFn = prev })

	ok, err := serviceDefinitionTargets("/bin/x")
	if err != nil || ok {
		t.Fatalf("a missing definition must read as absent: %v %v", ok, err)
	}
}

// TestRefreshNeverInstallsAMissingDefinition is the no-implicit-install rule:
// an update must not create a service the user never asked for.
func TestRefreshNeverInstallsAMissingDefinition(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "absent.service")
	prev := serviceDefinitionPathFn
	serviceDefinitionPathFn = func() (string, bool) { return missing, true }
	t.Cleanup(func() { serviceDefinitionPathFn = prev })

	receipt, err := refreshServiceDefinition()
	if err != nil {
		t.Fatalf("refresh = %v", err)
	}
	if receipt.Changed {
		t.Fatal("refresh reported a change for a service that is not installed")
	}
	if _, statErr := os.Stat(missing); !os.IsNotExist(statErr) {
		t.Fatal("refresh created a definition that did not exist")
	}
}

var _ selfupdate.Lifecycle = (*updateLifecycle)(nil)
