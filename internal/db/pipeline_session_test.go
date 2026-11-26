package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

// newSessionStore opens a temp-dir store and its pipeline-session store.
func newSessionStore(t *testing.T) (*Store, *PipelineSessionStore, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(filepath.Join(dir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, NewPipelineSessionStore(s), dir
}

func plannedSession(id string) *PipelineSessionRecord {
	now := time.Now()
	return &PipelineSessionRecord{
		SessionID: id,
		Target:    "/tmp/target",
		Intent:    "analyze the code",
		Status:    SessionPlanned,
		Stages:    []PipelineStage{{ToolName: "brainstorm:analyzer", Role: "ANALYZER", Phase: 1}},
		CreatedAt: now.Unix(),
		UpdatedAt: now.Unix(),
		ExpiresAt: now.Add(24 * time.Hour).Unix(),
	}
}

func TestPipelineSessionStore_CreateGet(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()

	rec := plannedSession("ep-create-1")
	if err := ps.CreatePlanned(ctx, rec); err != nil {
		t.Fatalf("CreatePlanned: %v", err)
	}
	got, err := ps.Get(ctx, "ep-create-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.SessionID != rec.SessionID || got.Status != SessionPlanned || got.Target != rec.Target {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if len(got.Stages) != 1 || got.Stages[0].ToolName != "brainstorm:analyzer" {
		t.Fatalf("stages lost: %+v", got.Stages)
	}
}

func TestPipelineSessionStore_UnknownSession(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	_, err := ps.Get(context.Background(), "ep-nonexistent")
	if !errors.Is(err, ErrSessionUnknown) {
		t.Fatalf("want ErrSessionUnknown, got %v", err)
	}
}

func TestPipelineSessionStore_ClaimOnce(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()
	if err := ps.CreatePlanned(ctx, plannedSession("ep-claim-1")); err != nil {
		t.Fatal(err)
	}

	claimed, err := ps.ClaimRunning(ctx, "ep-claim-1", "nonce-a")
	if err != nil {
		t.Fatalf("first claim: %v", err)
	}
	if claimed.Status != SessionRunning {
		t.Fatalf("want RUNNING, got %s", claimed.Status)
	}

	_, err = ps.ClaimRunning(ctx, "ep-claim-1", "nonce-b")
	if !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("second claim: want ErrSessionRunning, got %v", err)
	}
}

func TestPipelineSessionStore_ClaimRace(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()

	const rounds = 200
	const racers = 32
	for round := 0; round < rounds; round++ {
		id := fmt.Sprintf("ep-race-%d", round)
		if err := ps.CreatePlanned(ctx, plannedSession(id)); err != nil {
			t.Fatal(err)
		}
		var winners atomic.Int32
		var wg sync.WaitGroup
		for i := 0; i < racers; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				if _, err := ps.ClaimRunning(ctx, id, fmt.Sprintf("nonce-%d", n)); err == nil {
					winners.Add(1)
				}
			}(i)
		}
		wg.Wait()
		if winners.Load() != 1 {
			t.Fatalf("round %d: %d winners of %d claims, want exactly 1", round, winners.Load(), racers)
		}
	}
}

func TestPipelineSessionStore_ContinueRejectRace(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()

	const rounds = 200
	for round := 0; round < rounds; round++ {
		id := fmt.Sprintf("ep-cr-%d", round)
		if err := ps.CreatePlanned(ctx, plannedSession(id)); err != nil {
			t.Fatal(err)
		}
		var claimed, rejected atomic.Int32
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			if _, err := ps.ClaimRunning(ctx, id, "test-instance"); err == nil {
				claimed.Add(1)
			}
		}()
		go func() {
			defer wg.Done()
			if _, err := ps.Reject(ctx, id, "test-instance"); err == nil {
				rejected.Add(1)
			}
		}()
		wg.Wait()
		if claimed.Load()+rejected.Load() != 1 {
			t.Fatalf("round %d: claimed=%d rejected=%d, want exactly one winner", round, claimed.Load(), rejected.Load())
		}
	}
}

func TestPipelineSessionStore_Reject(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()
	if err := ps.CreatePlanned(ctx, plannedSession("ep-rej-1")); err != nil {
		t.Fatal(err)
	}

	rec, err := ps.Reject(ctx, "ep-rej-1", "test-instance")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if rec.Status != SessionRejected {
		t.Fatalf("want REJECTED, got %s", rec.Status)
	}
	if len(rec.Stages) != 0 {
		t.Fatalf("rejected session must discard stages, got %d", len(rec.Stages))
	}

	// A repeated reject returns the same REJECTED record, not an error.
	again, err := ps.Reject(ctx, "ep-rej-1", "test-instance")
	if err != nil {
		t.Fatalf("repeated Reject: %v", err)
	}
	if again.Status != SessionRejected {
		t.Fatalf("repeated reject: want REJECTED, got %s", again.Status)
	}

	// A claim after rejection is refused.
	if _, err := ps.ClaimRunning(ctx, "ep-rej-1", "nonce-x"); !errors.Is(err, ErrSessionRejected) {
		t.Fatalf("claim after reject: want ErrSessionRejected, got %v", err)
	}
}

func TestPipelineSessionStore_RejectInterruptedRun(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()
	if err := ps.CreatePlanned(ctx, plannedSession("ep-int-rej")); err != nil {
		t.Fatal(err)
	}
	// Claimed by a previous server instance.
	if _, err := ps.ClaimRunning(ctx, "ep-int-rej", "old-instance"); err != nil {
		t.Fatal(err)
	}
	// The current instance may discard the interrupted run (owner ruling).
	rec, err := ps.Reject(ctx, "ep-int-rej", "new-instance")
	if err != nil {
		t.Fatalf("reject of interrupted run: %v", err)
	}
	if rec.Status != SessionRejected || len(rec.Stages) != 0 {
		t.Fatalf("want REJECTED with no stages, got %+v", rec)
	}
	// A same-instance RUNNING session still refuses reject.
	if err := ps.CreatePlanned(ctx, plannedSession("ep-live-rej")); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.ClaimRunning(ctx, "ep-live-rej", "this-instance"); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Reject(ctx, "ep-live-rej", "this-instance"); !errors.Is(err, ErrSessionRunning) {
		t.Fatalf("same-instance reject: want ErrSessionRunning, got %v", err)
	}
}

func TestPipelineSessionStore_Expiry(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()
	rec := plannedSession("ep-exp-1")
	rec.ExpiresAt = time.Now().Add(-time.Minute).Unix() // past expires_at, inside TTL grace
	if err := ps.CreatePlanned(ctx, rec); err != nil {
		t.Fatal(err)
	}

	got, err := ps.Get(ctx, "ep-exp-1")
	if err != nil {
		t.Fatalf("Get inside grace: %v", err)
	}
	if got.Status != SessionPlanned {
		t.Fatalf("stored status should still be PLANNED, got %s", got.Status)
	}

	if _, err := ps.ClaimRunning(ctx, "ep-exp-1", "nonce-x"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("claim past expiry: want ErrSessionExpired, got %v", err)
	}
	if _, err := ps.Reject(ctx, "ep-exp-1", "test-instance"); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("reject past expiry: want ErrSessionExpired, got %v", err)
	}
}

func TestPipelineSessionStore_CapEvictsOldest(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()

	for i := 0; i < PipelineSessionCap; i++ {
		rec := plannedSession(fmt.Sprintf("ep-cap-%02d", i))
		rec.CreatedAt = time.Now().Add(time.Duration(i) * time.Second).Unix()
		if err := ps.CreatePlanned(ctx, rec); err != nil {
			t.Fatal(err)
		}
	}
	// The 51st evicts the oldest (ep-cap-00).
	if err := ps.CreatePlanned(ctx, plannedSession("ep-cap-new")); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.Get(ctx, "ep-cap-00"); !errors.Is(err, ErrSessionUnknown) {
		t.Fatalf("oldest should be evicted, got %v", err)
	}
	if _, err := ps.Get(ctx, "ep-cap-new"); err != nil {
		t.Fatalf("newest should exist: %v", err)
	}
	if _, err := ps.Get(ctx, "ep-cap-01"); err != nil {
		t.Fatalf("second-oldest should survive: %v", err)
	}
}

func TestPipelineSessionStore_InterruptedRun(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()
	if err := ps.CreatePlanned(ctx, plannedSession("ep-int-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.ClaimRunning(ctx, "ep-int-1", "nonce-old-instance"); err != nil {
		t.Fatal(err)
	}

	// A read with a different instance nonce reports interrupted, never resumable.
	_, err := ps.GetForInstance(ctx, "ep-int-1", "nonce-new-instance")
	if !errors.Is(err, ErrSessionInterrupted) {
		t.Fatalf("want ErrSessionInterrupted, got %v", err)
	}
	// The same instance still sees RUNNING.
	got, err := ps.GetForInstance(ctx, "ep-int-1", "nonce-old-instance")
	if err != nil {
		t.Fatalf("same instance: %v", err)
	}
	if got.Status != SessionRunning {
		t.Fatalf("want RUNNING, got %s", got.Status)
	}
}

func TestPipelineSessionStore_TerminalStates(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()

	cases := []struct {
		status   string
		complete func(context.Context, *PipelineSessionStore, string) error
		wantErr  error
	}{
		{SessionCompleted, func(ctx context.Context, ps *PipelineSessionStore, id string) error {
			return ps.Complete(ctx, id, []string{"mcp://magictools/raw/pipeline_" + id + "_plan"})
		}, ErrSessionAlreadyRun},
		{SessionFailed, func(ctx context.Context, ps *PipelineSessionStore, id string) error {
			return ps.Fail(ctx, id)
		}, ErrSessionFailed},
	}
	for _, tc := range cases {
		id := "ep-term-" + tc.status
		if err := ps.CreatePlanned(ctx, plannedSession(id)); err != nil {
			t.Fatal(err)
		}
		if _, err := ps.ClaimRunning(ctx, id, "nonce-t"); err != nil {
			t.Fatal(err)
		}
		if err := tc.complete(ctx, ps, id); err != nil {
			t.Fatalf("complete %s: %v", tc.status, err)
		}
		if _, err := ps.ClaimRunning(ctx, id, "nonce-t2"); !errors.Is(err, tc.wantErr) {
			t.Fatalf("claim after %s: want %v, got %v", tc.status, tc.wantErr, err)
		}
	}
}

func TestPipelineSessionStore_CompletedCarriesArtifacts(t *testing.T) {
	_, ps, _ := newSessionStore(t)
	ctx := context.Background()
	id := "ep-art-1"
	if err := ps.CreatePlanned(ctx, plannedSession(id)); err != nil {
		t.Fatal(err)
	}
	if _, err := ps.ClaimRunning(ctx, id, "nonce-a"); err != nil {
		t.Fatal(err)
	}
	uris := []string{"mcp://magictools/raw/pipeline_" + id + "_plan", "mcp://magictools/raw/pipeline_" + id + "_audit"}
	if err := ps.Complete(ctx, id, uris); err != nil {
		t.Fatal(err)
	}
	got, err := ps.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ArtifactURIs) != 2 || got.ArtifactURIs[0] != uris[0] {
		t.Fatalf("artifact URIs lost: %+v", got.ArtifactURIs)
	}
}

func TestPipelineSessionStore_PrefixIsolation(t *testing.T) {
	s, ps, _ := newSessionStore(t)
	ctx := context.Background()
	id := "ep-iso-1"
	if err := ps.CreatePlanned(ctx, plannedSession(id)); err != nil {
		t.Fatal(err)
	}

	// The session must not be readable through the public raw: resource path.
	if _, err := s.GetRawResource(id); err == nil {
		t.Fatal("session readable via raw: prefix — must be isolated")
	}
	if _, err := s.GetRawResource("pipeline-session:" + id); err == nil {
		t.Fatal("session readable via raw:pipeline-session: — must be isolated")
	}

	// The stored value is compressed, not plain JSON.
	raw, err := ps.rawValue(id)
	if err != nil {
		t.Fatalf("rawValue: %v", err)
	}
	if _, derr := util.Decompress(raw); derr != nil {
		t.Fatalf("stored value is not compressed: %v", derr)
	}
	var check map[string]any
	if jerr := json.Unmarshal(raw, &check); jerr == nil {
		t.Fatal("stored value is plain JSON — must be compressed")
	}
}
