package dag

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestGetEdgeScore_NoData(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	score := GetEdgeScore(store, "brainstorm:analyze", "go-modernizer:transform")
	if score != 0.0 {
		t.Errorf("expected 0.0 for unknown transition, got %f", score)
	}
}

func TestGetEdgeScore_NilStore(t *testing.T) {
	score := GetEdgeScore(nil, "a", "b")
	if score != 0.0 {
		t.Errorf("expected 0.0 for nil store, got %f", score)
	}
}

func TestGetEdgeScore_SuccessOnly(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	from, to := "brainstorm:analyze", "go-modernizer:ast_transform"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(from+"->"+to)))

	// Record 5 successes
	for range 5 {
		store.RecordSynergy(hash, true)
	}

	score := GetEdgeScore(store, from, to)
	// Expected: 5 / (5 + 0 + 1) = 0.8333...
	if score < 0.83 || score > 0.84 {
		t.Errorf("expected ~0.833 for 5 successes, got %f", score)
	}
}

func TestGetEdgeScore_MixedResults(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	from, to := "brainstorm:critique", "go-modernizer:complexity"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(from+"->"+to)))

	// Record 3 successes, 2 penalties
	for range 3 {
		store.RecordSynergy(hash, true)
	}
	for range 2 {
		store.RecordSynergy(hash, false)
	}

	score := GetEdgeScore(store, from, to)
	// Expected: 3 / (3 + 2 + 1) = 0.5
	if score < 0.49 || score > 0.51 {
		t.Errorf("expected ~0.5 for 3s/2f, got %f", score)
	}
}

func TestGetEdgeScore_PenaltyOnly(t *testing.T) {
	store := setupTestStore(t)
	defer store.Close()

	from, to := "go-modernizer:a", "go-modernizer:b"
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(from+"->"+to)))

	// Record 4 penalties
	for range 4 {
		store.RecordSynergy(hash, false)
	}

	score := GetEdgeScore(store, from, to)
	// Expected: 0 / (0 + 4 + 1) = 0.0
	if score != 0.0 {
		t.Errorf("expected 0.0 for penalty-only, got %f", score)
	}
}

func TestTransitionHash_MatchesAuditFormat(t *testing.T) {
	// Verify our hash matches the exact format from generate_audit_report.go
	from := "brainstorm:analyze"
	to := "go-modernizer:transform"

	expected := fmt.Sprintf("%x", sha256.Sum256([]byte(from+"->"+to)))
	got := transitionHash(from, to)

	if got != expected {
		t.Errorf("hash mismatch:\n  expected: %s\n  got:      %s", expected, got)
	}
}

func setupTestStore(t *testing.T) *db.Store {
	t.Helper()
	tmpDir, err := os.MkdirTemp("", "edge-weight-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })

	store, err := db.NewStore(filepath.Join(tmpDir, "db"))
	if err != nil {
		t.Fatal(err)
	}
	return store
}
