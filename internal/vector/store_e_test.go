package vector

import (
	"context"
	"strings"
	"testing"
)

// buildMockEngine builds an in-memory engine with a mock embedder, mirroring the
// pattern in TestStore_EngineOperations.
func buildMockEngine() *Engine {
	e := &Engine{
		graph:        createHNSWGraph(),
		embedder:     &mockEmbedder{vector: []float32{1, 0, 0}},
		expectedDims: 3,
	}
	e.initialized = true
	return e
}

// TestPruneOrphanedNodes_PreservesFailureAnchors is the INT-1 regression: a
// graph-reconciliation sweep against the tool set must NOT delete fail:<...>
// anchors, or the contrastive-penalty feature dies on the first GC tick.
func TestPruneOrphanedNodes_PreservesFailureAnchors(t *testing.T) {
	e := buildMockEngine()
	ctx := context.Background()

	if err := e.AddVector(ctx, "srv:tool", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	anchorID := FailureAnchorPrefix + "srv:tool:abc123"
	if err := e.AddVector(ctx, anchorID, []float32{0, 1, 0}); err != nil {
		t.Fatal(err)
	}

	// Reconcile against the tool set ONLY — anchors are never tool URNs, exactly
	// as the GC/purge call sites build validKeys.
	if pruned := e.PruneOrphanedNodes(map[string]bool{"srv:tool": true}); pruned != 0 {
		t.Errorf("expected 0 pruned (anchor must survive), got %d", pruned)
	}
	if !e.HasDocument(anchorID) {
		t.Error("failure anchor was pruned; contrastive-penalty feature would be dead")
	}
	if !e.HasDocument("srv:tool") {
		t.Error("valid tool was pruned")
	}

	// A genuinely orphaned tool (not in validKeys, no reserved prefix) must still prune.
	if err := e.AddVector(ctx, "srv:gone", []float32{0, 0, 1}); err != nil {
		t.Fatal(err)
	}
	if pruned := e.PruneOrphanedNodes(map[string]bool{"srv:tool": true}); pruned != 1 {
		t.Errorf("expected 1 pruned (orphan tool), got %d", pruned)
	}
	if e.HasDocument("srv:gone") {
		t.Error("orphaned tool should have been pruned")
	}
	if !e.HasDocument(anchorID) {
		t.Error("failure anchor must still survive a sweep that prunes a real orphan")
	}
}

// TestSearchWithScores_ExcludesFailureAnchors is the INT-5 regression: tool
// search must not return failure anchors (they would consume top-k slots), while
// the dedicated SearchFailureAnchors path must return exactly the anchors.
func TestSearchWithScores_ExcludesFailureAnchors(t *testing.T) {
	e := buildMockEngine() // mock embedder returns {1,0,0} for any text
	ctx := context.Background()

	anchorID := FailureAnchorPrefix + "srv:tool:hash"
	if err := e.AddVector(ctx, "srv:tool", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := e.AddVector(ctx, anchorID, []float32{1, 0, 0}); err != nil { // equally close
		t.Fatal(err)
	}

	tools, err := e.SearchWithScores(ctx, "q", 10)
	if err != nil {
		t.Fatal(err)
	}
	sawTool := false
	for _, r := range tools {
		if strings.HasPrefix(r.Key, FailureAnchorPrefix) {
			t.Errorf("failure anchor %q leaked into tool search", r.Key)
		}
		if r.Key == "srv:tool" {
			sawTool = true
		}
	}
	if !sawTool {
		t.Error("real tool missing from SearchWithScores")
	}

	anchors, err := e.SearchFailureAnchors(ctx, "q", 10)
	if err != nil {
		t.Fatal(err)
	}
	sawAnchor := false
	for _, r := range anchors {
		if !strings.HasPrefix(r.Key, FailureAnchorPrefix) {
			t.Errorf("non-anchor %q returned by SearchFailureAnchors", r.Key)
		}
		if r.Key == anchorID {
			sawAnchor = true
		}
	}
	if !sawAnchor {
		t.Error("anchor missing from SearchFailureAnchors")
	}
}

// TestRebuildGraph_ReapsRedundantTombstones is the HNW-7 regression: after a
// rebuild, tombstones for keys absent from the rebuilt graph (deleted nodes and
// delete-of-absent phantoms) must be reclaimed, not leaked forever.
func TestRebuildGraph_ReapsRedundantTombstones(t *testing.T) {
	e := buildMockEngine()
	ctx := context.Background()

	if err := e.AddVector(ctx, "a", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if err := e.AddVector(ctx, "b", []float32{0, 1, 0}); err != nil {
		t.Fatal(err)
	}

	e.DeleteDocuments("b")              // tombstone a real node
	e.tombstones.Store("phantom", true) // tombstone for an id never in the graph

	e.corrupt.Store(true)
	e.healIfCorrupt()

	if _, ok := e.tombstones.Load("b"); ok {
		t.Error("tombstone for deleted node not reaped")
	}
	if _, ok := e.tombstones.Load("phantom"); ok {
		t.Error("phantom tombstone not reaped")
	}
	if e.HasDocument("b") {
		t.Error("b should be gone after rebuild")
	}
	if !e.HasDocument("a") {
		t.Error("a should remain after rebuild")
	}
}

// TestAddVector_ClearsTombstoneOnReadd is the INT-4 regression: re-adding a
// tombstoned id must resurrect it (clear the tombstone), not leave it shadowed
// from search and then dropped by the next rebuild.
func TestAddVector_ClearsTombstoneOnReadd(t *testing.T) {
	e := buildMockEngine()
	ctx := context.Background()

	if err := e.AddVector(ctx, "x", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	// Delete tombstones the node (deferred-corrupt path; node stays in graph).
	if !e.DeleteDocument("x") {
		t.Fatal("delete failed")
	}
	if _, dead := e.tombstones.Load("x"); !dead {
		t.Fatal("expected tombstone after delete")
	}

	// Re-add the same id BEFORE any heal — hits the early-return (node still present).
	if err := e.AddVector(ctx, "x", []float32{1, 0, 0}); err != nil {
		t.Fatal(err)
	}
	if _, dead := e.tombstones.Load("x"); dead {
		t.Error("INT-4: tombstone not cleared on re-add; node is shadowed from search")
	}

	// It must survive a rebuild — a leaked tombstone would physically drop it.
	e.corrupt.Store(true)
	e.healIfCorrupt()
	if !e.HasDocument("x") {
		t.Error("re-added node was dropped by rebuild (tombstone leaked)")
	}
}

// TestSearchByNode_SortedByScore is the HNW-1 regression: SearchByNode must
// return results sorted by descending cosine, not raw heap order, and must yield
// the true top-k rather than k arbitrary near nodes.
func TestSearchByNode_SortedByScore(t *testing.T) {
	e := buildMockEngine()
	ctx := context.Background()

	// target identical to "a"; decreasing similarity through b, d, c.
	vecs := map[string][]float32{
		"t": {1, 0, 0},     // self (skipped)
		"a": {1, 0, 0},     // cosine 1.0
		"b": {0.8, 0.6, 0}, // cosine 0.8
		"d": {0.6, 0.8, 0}, // cosine 0.6
		"c": {0, 1, 0},     // cosine 0.0
	}
	for id, v := range vecs {
		if err := e.AddVector(ctx, id, v); err != nil {
			t.Fatalf("AddVector %s: %v", id, err)
		}
	}

	res, err := e.SearchByNode(ctx, "t", 10)
	if err != nil {
		t.Fatalf("SearchByNode: %v", err)
	}
	if len(res) != 4 {
		t.Fatalf("expected 4 results (all but self), got %d", len(res))
	}
	// Self must be excluded.
	for _, r := range res {
		if r.Key == "t" {
			t.Error("self node leaked into results")
		}
	}
	// Monotonic non-increasing scores.
	for i := 1; i < len(res); i++ {
		if res[i-1].Score < res[i].Score {
			t.Errorf("results not sorted descending: [%d]=%.4f < [%d]=%.4f",
				i-1, res[i-1].Score, i, res[i].Score)
		}
	}
	// Nearest neighbor (identical vector) must rank first.
	if res[0].Key != "a" {
		t.Errorf("expected nearest neighbor 'a' first, got %q", res[0].Key)
	}

	// True top-k: with k=2 we must get the two highest-scoring, not arbitrary nodes.
	top2, err := e.SearchByNode(ctx, "t", 2)
	if err != nil {
		t.Fatalf("SearchByNode k=2: %v", err)
	}
	if len(top2) != 2 {
		t.Fatalf("expected 2 results, got %d", len(top2))
	}
	if top2[0].Key != "a" || top2[1].Key != "b" {
		t.Errorf("expected top-2 [a b], got [%s %s]", top2[0].Key, top2[1].Key)
	}
}
