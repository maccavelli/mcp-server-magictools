package handler

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// minifyLogCapture is a slog.Handler that keeps every record, so a test can read what a background task logged.
type minifyLogCapture struct {
	mu      sync.Mutex
	records []slog.Record
}

func (c *minifyLogCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *minifyLogCapture) Handle(_ context.Context, r slog.Record) error {
	c.mu.Lock()
	c.records = append(c.records, r.Clone())
	c.mu.Unlock()
	return nil
}
func (c *minifyLogCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *minifyLogCapture) WithGroup(string) slog.Handler      { return c }

// isStoreClosingRefusal reports whether err is the refusal a write gets once the store is closing. Plan 0009 phase P1
// wrote it as a text match, before db.ErrStoreClosing existed; phase P2 made it the sentinel test.
func isStoreClosingRefusal(err error) bool {
	return errors.Is(err, db.ErrStoreClosing)
}

// drainBackgroundPool returns once every pool worker is idle and every task queued before the call has finished: it
// needs bgWorkers distinct workers to meet at a barrier, and a worker running an earlier task cannot attend.
func drainBackgroundPool(t *testing.T) {
	t.Helper()
	var arrived, release sync.WaitGroup
	arrived.Add(bgWorkers)
	release.Add(1)
	for range bgWorkers {
		submitBackground(func() {
			arrived.Done()
			release.Wait()
		})
	}
	done := make(chan struct{})
	go func() { arrived.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		release.Done()
		t.Fatal("background pool did not drain within 10s")
	}
	release.Done()
}

// TestMinifyResponse_SaveAfterStoreCloseIsRefused is the deterministic form of the gate's race: the save that
// MinifyResponse queued runs only after the store has been closed completely. It must be refused with the
// store-closing refusal (logged by the task), not run against a closed database.
func TestMinifyResponse_SaveAfterStoreCloseIsRefused(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	ps := NewProxyService(h)

	logs := &minifyLogCapture{}
	old := slog.Default()
	slog.SetDefault(slog.New(logs))
	defer slog.SetDefault(old)

	var parked sync.WaitGroup
	parked.Add(bgWorkers)
	release := make(chan struct{})
	for range bgWorkers {
		submitBackground(func() {
			parked.Done()
			<-release
		})
	}
	parked.Wait()

	ps.MinifyResponse(context.Background(), buildMinifyCase("large"), "srv", "tool", 1000, 500, nil)
	if err := store.Close(); err != nil {
		close(release)
		t.Fatalf("store.Close: %v", err)
	}
	close(release)
	drainBackgroundPool(t)

	logs.mu.Lock()
	defer logs.mu.Unlock()
	var saveErr error
	found := false
	for _, r := range logs.records {
		if r.Message != "gateway: failed to session-cache raw response" {
			continue
		}
		found = true
		r.Attrs(func(a slog.Attr) bool {
			if err, ok := a.Value.Any().(error); ok {
				saveErr = err
			}
			return true
		})
	}
	if !found {
		t.Fatal("the queued save was not refused: no failure was logged, so it ran against a closed store")
	}
	if !isStoreClosingRefusal(saveErr) {
		t.Errorf("the queued save failed with %v, want an error satisfying errors.Is(err, db.ErrStoreClosing)", saveErr)
	}
}
