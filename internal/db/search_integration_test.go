package db

import (
	"context"
	"crypto/sha256"
	"math"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

type hashEmbedder struct {
	dims int
}

func (h *hashEmbedder) Embed(_ context.Context, text string) ([]float32, error) {
	vecOut := make([]float32, h.dims)
	for word := range strings.FieldsSeq(strings.ToLower(text)) {
		if len(word) < 2 {
			continue
		}
		sum := sha256.Sum256([]byte(word))
		for j := range 4 {
			idx := int(sum[j]) % h.dims
			vecOut[idx] += 1.0
		}
	}
	var norm float64
	for _, v := range vecOut {
		norm += float64(v * v)
	}
	if norm > 0 {
		scale := float32(1.0 / math.Sqrt(norm))
		for i := range vecOut {
			vecOut[i] *= scale
		}
	}
	return vecOut, nil
}

func (h *hashEmbedder) Provider() string { return "hash-mock" }

func goldenSearchTools() []*ToolRecord {
	return []*ToolRecord{
		{
			URN:            "recall:get_metrics",
			Name:           "get_metrics",
			Server:         "recall",
			Description:    "Returns recall health and search telemetry metrics",
			Intent:         "metrics health telemetry diagnostics",
			Category:       "diagnostic",
			ParameterNames: []string{"include_history", "window"},
			LexicalTokens:  []string{"telemetry"},
			Triggers:       []string{"telemetry", "metrics"},
		},
		{
			URN:            "filesystem:read_file",
			Name:           "read_file",
			Server:         "filesystem",
			Description:    "Read file contents from disk",
			Intent:         "read open file contents",
			Category:       "filesystem",
			ParameterNames: []string{"path"},
			EnumValues:     []string{"utf-8", "binary"},
		},
	}
}

func setupHybridSearchHarness(t *testing.T) (*Store, func()) {
	t.Helper()
	path := t.TempDir()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}

	const dims = 32
	prevEngine := vector.GlobalEngine
	vector.GlobalEngine = vector.NewTestEngine(&hashEmbedder{dims: dims}, dims)

	ctx := context.Background()
	for _, tool := range goldenSearchTools() {
		if err := store.SaveTool(tool); err != nil {
			t.Fatal(err)
		}
		text := BuildEmbeddingText(tool)
		hash := ComputeEmbeddingHash("hash-mock:test", text)
		if err := vector.GlobalEngine.UpsertDocument(ctx, tool.URN, text, hash); err != nil {
			t.Fatalf("upsert %s: %v", tool.URN, err)
		}
	}

	cleanup := func() {
		vector.GlobalEngine = prevEngine
		store.Close()
	}
	return store, cleanup
}

func TestSearchGoldenQueries_Hybrid(t *testing.T) {
	store, cleanup := setupHybridSearchHarness(t)
	defer cleanup()

	ctx := context.Background()
	cases := []struct {
		query    string
		wantURN  string
		category string
	}{
		{query: "recall metrics", wantURN: "recall:get_metrics"},
		{query: "telemetry", wantURN: "recall:get_metrics", category: "diagnostic"},
		{query: "get_metrics", wantURN: "recall:get_metrics"},
	}

	for _, tc := range cases {
		t.Run(tc.query, func(t *testing.T) {
			results, err := store.SearchTools(ctx, tc.query, tc.category, "", 0.0, 0.55, DomainSystem, false)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if len(results) == 0 {
				t.Fatalf("expected results for %q", tc.query)
			}
			if results[0].URN != tc.wantURN {
				t.Fatalf("query %q: got %s want %s", tc.query, results[0].URN, tc.wantURN)
			}
			trace := telemetry.SearchMetrics.LastQueryTrace.Load()
			if trace == nil {
				t.Fatalf("expected query trace for %q", tc.query)
			}
			if trace.FusionWinner == "" {
				t.Fatalf("expected fusion winner in trace for %q", tc.query)
			}
		})
	}
}
