package intelligence

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestDrainTriggers(t *testing.T) {
	ch := make(chan struct{}, 5)
	ch <- struct{}{}
	ch <- struct{}{}
	drainTriggers(ch)
	select {
	case <-ch:
		t.Error("channel not drained")
	default:
	}
}

func TestStartHydratorDaemon(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-intel-daemon-test")
	defer os.RemoveAll(tmpDir)

	store, err := db.NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{}

	triggerChan := make(chan struct{}, 1)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		StartHydratorDaemon(ctx, store, cfg, nil, triggerChan, nil)
		close(done)
	}()

	// Send a trigger
	triggerChan <- struct{}{}
	time.Sleep(100 * time.Millisecond) // Let it process

	// Cancel to stop daemon
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("daemon did not exit")
	}
}
