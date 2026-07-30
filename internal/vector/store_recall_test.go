package vector

import (
	"context"
	"testing"
)

// TestSearchWithScores_RankedByCosine is the IDX-1/IDX-2 regression: the index
// must return the true nearest-k in descending cosine order, not the first-k of
// the graph's heap-ordered slice. B is the 2nd-nearest and must appear at rank 2.
func TestSearchWithScores_RankedByCosine(t *testing.T) {
	queryVec := []float32{1, 0, 0, 0}
	e := &Engine{
		graph:        createHNSWGraph(),
		embedder:     &mockEmbedder{vector: queryVec},
		expectedDims: 4,
	}
	e.initialized = true

	docs := map[string][]float32{
		"A": {1, 0, 0, 0},     // == query (nearest)
		"B": {0.9, 0.1, 0, 0}, // 2nd nearest
		"C": {0, 1, 0, 0},
		"D": {0, 0, 1, 0},
		"E": {0, 0, 0, 1},
	}
	for id, v := range docs {
		if err := e.AddVector(context.Background(), id, v); err != nil {
			t.Fatalf("AddVector %s: %v", id, err)
		}
	}

	res, err := e.SearchWithScores(context.Background(), "query", 3)
	if err != nil {
		t.Fatalf("SearchWithScores: %v", err)
	}
	if len(res) == 0 || len(res) > 3 {
		t.Fatalf("expected 1..3 results, got %d: %+v", len(res), res)
	}
	if res[0].Key != "A" {
		t.Errorf("nearest should be A, got %s (%+v)", res[0].Key, res)
	}
	if len(res) >= 2 && res[1].Key != "B" {
		t.Errorf("2nd nearest should be B, got %s (%+v)", res[1].Key, res)
	}
	for i := 1; i < len(res); i++ {
		if res[i].Score > res[i-1].Score+1e-9 {
			t.Errorf("results must be sorted by descending score: %+v", res)
		}
	}
}
