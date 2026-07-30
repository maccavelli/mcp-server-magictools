package intelligence

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

type mockEmbedder struct{}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	// Simple deterministic mock
	vec := make([]float32, 384)
	if strings.Contains(text, "intent") {
		vec[0] = 1.0
	} else if strings.Contains(text, "error") {
		vec[1] = 1.0
	} else {
		vec[2] = 1.0
	}
	return vec, nil
}
func (m *mockEmbedder) Provider() string { return "mock" }

func TestSelfCorrectExtraCoverage(t *testing.T) {
	// init store
	tmpDir, _ := os.MkdirTemp("", "testdb-*")
	defer os.RemoveAll(tmpDir)
	store, _ := db.NewStore(tmpDir)
	defer store.Close()

	// init vector engine
	cfg := &config.Config{}
	cfg.Intelligence.VectorEnabled = true
	vector.InitGlobalEngine(tmpDir, cfg)
	origEngine := vector.GlobalEngine
	vector.GlobalEngine = vector.NewTestEngine(&mockEmbedder{}, 384)
	defer func() { vector.GlobalEngine = origEngine }()

	// CheckFailureProximity
	RecordIntentOutcome(store, "test", "urn", true)
	_ = CheckFailureProximity(context.Background(), store, "test", "urn")

	// RecordFailureAnchor
	PruneFailureAnchors(store, "tool1", 1.0)
}

func TestSelfCorrect_CheckFailureProximityBatch(t *testing.T) {
	d, err := os.MkdirTemp("", "sc_batch_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	origEngine := vector.GlobalEngine
	vector.GlobalEngine = vector.NewTestEngine(&mockEmbedder{}, 384)
	defer func() { vector.GlobalEngine = origEngine }()

	RecordFailureAnchor(context.Background(), "tool_a", "test failed", "err")
	RecordFailureAnchor(context.Background(), "tool_b", "another error", "err")

	tools := []string{"tool_a", "tool_b", "tool_c"}

	filtered := CheckFailureProximityBatch(context.Background(), store, "test failed here", tools)

	if filtered == nil {
		t.Errorf("expected map, got nil")
	}
}

func TestSelfCorrect_CheckFailureProximity(t *testing.T) {
	d, err := os.MkdirTemp("", "sc_prox_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	origEngine := vector.GlobalEngine
	vector.GlobalEngine = vector.NewTestEngine(&mockEmbedder{}, 384)
	defer func() { vector.GlobalEngine = origEngine }()

	RecordFailureAnchor(context.Background(), "tool_a", "test intent", "err")

	score := CheckFailureProximity(context.Background(), store, "test intent", "tool_a")
	if score <= 0.0 || score > 1.0 {
		t.Errorf("expected score in (0, 1], got %v", score)
	}
}

func TestSelfCorrect_PruneFailureAnchors(t *testing.T) {
	d, err := os.MkdirTemp("", "sc_prune_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	origEngine := vector.GlobalEngine
	vector.GlobalEngine = vector.NewTestEngine(&mockEmbedder{}, 384)
	defer func() { vector.GlobalEngine = origEngine }()

	RecordFailureAnchor(context.Background(), "tool_to_prune", "test intent", "err")

	PruneFailureAnchors(store, "tool_to_prune", 1.5)

	if !isToolPruned(store, "tool_to_prune") {
		t.Error("expected tool to be pruned")
	}

	// Not pruned
	PruneFailureAnchors(store, "tool_not_pruned", 1.0)
	if isToolPruned(store, "tool_not_pruned") {
		t.Error("expected tool not to be pruned")
	}
}
