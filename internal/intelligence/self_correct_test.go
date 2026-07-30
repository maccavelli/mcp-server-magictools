package intelligence

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestSelfCorrectIntentOutcome(t *testing.T) {
	dbPath := "test_badger_self_correct"
	defer os.RemoveAll(dbPath)

	store, err := db.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	intent := "Search the web for Go tutorials"
	tool := "ddg-search:search_web"

	// Initial score should be 0.0 (neutral)
	score := GetIntentToolScore(store, intent, tool)
	if score != 0.0 {
		t.Errorf("expected 0.0, got %f", score)
	}

	// Record success
	RecordIntentOutcome(store, intent, tool, true)
	score = GetIntentToolScore(store, intent, tool)
	if score <= 0.0 || score >= 1.0 {
		t.Errorf("expected score in (0, 1) due to Laplace smoothing, got %f", score)
	}

	// Record failure
	RecordIntentOutcome(store, intent, tool, false)
	score2 := GetIntentToolScore(store, intent, tool)
	if score2 >= score {
		t.Errorf("expected score to decrease after failure, got %f -> %f", score, score2)
	}
}

func TestExtractPRFTerms(t *testing.T) {
	topHits := []*db.ToolRecord{
		{
			URN:                    "server:tool1",
			Description:            "Search the internet using DuckDuckGo.",
			HighlightedDescription: "Search the <mark>internet</mark> using DuckDuckGo.",
		},
	}

	terms := ExtractPRFTerms(topHits, "search web", 5)
	if len(terms) == 0 {
		t.Fatal("expected PRF terms")
	}

	// Check that common words are filtered and new words like "internet" or "duckduckgo" are found
	found := false
	for _, term := range terms {
		if term == "internet" || term == "duckduckgo" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'internet' or 'duckduckgo' in PRF terms, got %v", terms)
	}
}

func TestExtractPRFTermsSyntheticIntents(t *testing.T) {
	topHits := []*db.ToolRecord{
		{
			URN:              "server:tool1",
			Description:      "Generic tool.",
			SyntheticIntents: []string{"duckduckgo internet search engine"},
		},
	}
	terms := ExtractPRFTerms(topHits, "search web", 5)
	found := false
	for _, term := range terms {
		if term == "duckduckgo" || term == "internet" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected synthetic intent terms in PRF, got %v", terms)
	}
}

func TestComputeResultOverlap(t *testing.T) {
	setA := []*db.ToolRecord{{URN: "A"}, {URN: "B"}}
	setB := []*db.ToolRecord{{URN: "B"}, {URN: "C"}}

	overlap := ComputeResultOverlap(setA, setB)
	if overlap != 0.5 {
		t.Errorf("expected 0.5 overlap, got %f", overlap)
	}

	overlap = ComputeResultOverlap(setA, setA)
	if overlap != 1.0 {
		t.Errorf("expected 1.0 overlap, got %f", overlap)
	}
}

func TestClassifyError(t *testing.T) {
	tests := []struct {
		err  string
		want string
	}{
		{"context deadline exceeded", "TIMEOUT"},
		{"validation failed: missing properties", "SCHEMA_MISMATCH"},
		{"connection refused", "SERVER_UNAVAILABLE"},
		{"broken pipe", "SERVER_UNAVAILABLE"},
		{"something else", "TOOL_ERROR"},
	}

	for _, tt := range tests {
		got := ClassifyError(tt.err)
		if got != tt.want {
			t.Errorf("ClassifyError(%q) = %s, want %s", tt.err, got, tt.want)
		}
	}
}

func TestFailureAnchorsNoVector(t *testing.T) {
	// Verify that failure anchor functions don't panic when vector engine is nil
	ctx := context.Background()
	RecordFailureAnchor(ctx, "tool1", "intent1", "TIMEOUT")

	penalty := CheckFailureProximity(ctx, nil, "intent1", "tool1")
	if penalty != 1.0 {
		t.Errorf("expected penalty 1.0 when vector is offline, got %f", penalty)
	}

	PruneFailureAnchors(nil, "tool1", 2.0)

	if isToolPruned(nil, "tool1") {
		t.Error("expected false for nil store")
	}
}

func TestPruneFailureAnchors(t *testing.T) {
	dbPath := "test_badger_prune"
	defer os.RemoveAll(dbPath)

	store, err := db.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	// Before pruning
	if isToolPruned(store, "tool1") {
		t.Error("expected false")
	}

	// Insufficient reliability
	PruneFailureAnchors(store, "tool1", 1.0)
	if isToolPruned(store, "tool1") {
		t.Error("expected false after pruning with low reliability")
	}

	// Sufficient reliability
	PruneFailureAnchors(store, "tool1", 2.0)
	if !isToolPruned(store, "tool1") {
		t.Error("expected true after pruning")
	}
}
