package client

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func TestMonitorResources_Empty(t *testing.T) {
	m := &WarmRegistry{
		Servers: make(map[string]*SubServer),
	}
	// Should not panic
	m.MonitorResources()
}

func TestPingAll_Empty(t *testing.T) {
	m := &WarmRegistry{
		Servers: make(map[string]*SubServer),
	}
	// Should not panic
	m.PingAll(context.Background())
}

// throwawayHome points every per-user directory the monitor reads or writes (servers.yaml through
// PollConfigChanges, the cache through WriteSnapshot) at a temporary directory, never the operator's (0010-MADR, F8).
func throwawayHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(home, ".local", "share"))
	t.Setenv(config.EnvConfigDir, filepath.Join(home, "config"))
}

// TestHealthMonitor_Lifecycle runs the monitor at a 10 ms interval over a fake process table (0010, P5). Before, it
// asserted only that a short run did not panic, and its first tick ran the reaper against the real machine. Now: the
// first tick runs at once and reaps through the table (one snapshot at least); a process carrying only the old
// constant is left alone; the monitor stops when its context is cancelled; no panic.
func TestHealthMonitor_Lifecycle(t *testing.T) {
	throwawayHome(t)
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 99, Start: 1}}
	fake := newFakeProcessTable(t, self, fakeProc{info: ProcInfo{PID: 200, PPID: 1, Start: 2000},
		env: []string{"PATH=/bin", EnvManaged + "=" + EnvManagedValue}, dies: SigTerm})
	m := &WarmRegistry{
		Servers: make(map[string]*SubServer),
		Config:  &config.Config{},
	}
	m.clock = newFakeClock()
	if err := m.AttachProcessTable(fake, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.StartHealthMonitor(ctx, 10*time.Millisecond)
	}()
	for deadline := time.Now().Add(10 * time.Second); fake.snapshotCount() == 0 && time.Now().Before(deadline); {
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the health monitor did not stop after its context was cancelled")
	}
	m.reapWG.Wait()
	if fake.snapshotCount() == 0 {
		t.Fatal("the monitor's first tick did not run the reaper over the table")
	}
	if s := fake.signalLog(); len(s) != 0 {
		t.Fatalf("the monitor signalled a process carrying only the old constant: %v", s)
	}
}
