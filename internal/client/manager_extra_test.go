package client

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// TestExtraManagerMethods exercises the registry's surface end to end (0010, P5: over a fake process table). Before,
// it asserted nothing explicitly and its PruneOrphans ran against the real machine. It keeps every call it made, and
// now also asserts that an unknown server is not reported present, that PruneOrphans consulted the fake table, and
// that nothing was signalled. Boot still spawns the test's own `echo` child for real.
func TestExtraManagerMethods(t *testing.T) {
	throwawayHome(t)
	tempDir := t.TempDir()

	store, err := db.NewStore(tempDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{}
	wr := NewWarmRegistry(tempDir, store, cfg)
	self := SelfInfo{ProcInfo: ProcInfo{PID: 100, PPID: 99, Start: 1}}
	fake := newFakeProcessTable(t, self)
	if err := wr.AttachProcessTable(fake, tempDir); err != nil {
		t.Fatal(err)
	}

	if wr.HasServer("unknown") {
		t.Error("an unknown server is reported present")
	}

	wr.RequestState("unknown", StatusOffline)
	_ = wr.GetFailedServers()

	// CallProxy with an unknown server to trigger fast path exit
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, _ = wr.CallProxy(ctx, "unknown", "tool", nil, 1*time.Second)

	wr.Boot(ctx, []config.ServerConfig{{Name: "test", Command: "echo"}})
	wr.AuditGlobalRegistry()
	wr.DisconnectAll()

	before := fake.snapshotCount()
	wr.PruneOrphans()
	wr.reapWG.Wait()
	if fake.snapshotCount() <= before {
		t.Error("PruneOrphans did not enumerate the fake table")
	}
	if s := fake.signalLog(); len(s) != 0 {
		t.Errorf("processes were signalled through the table: %v", s)
	}
}
