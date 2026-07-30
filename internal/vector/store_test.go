package vector

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

type mockEmbedder struct {
	vector []float32
	err    error
}

func (m *mockEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return m.vector, m.err
}

func (m *mockEmbedder) Provider() string {
	return "mock"
}

func TestStore_Init(t *testing.T) {
	initOnce = sync.Once{}
	GlobalEngine = nil

	cfg := &config.Config{}
	cfg.Intelligence.VectorEnabled = false

	err := InitGlobalEngine(t.TempDir(), cfg)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if GlobalEngine == nil {
		t.Fatalf("expected global engine")
	}

	if GlobalEngine.VectorEnabled() {
		t.Fatalf("expected vector to be disabled")
	}
}

func TestStore_EngineOperations(t *testing.T) {
	e := &Engine{
		graph: createHNSWGraph(),
		embedder: &mockEmbedder{
			vector: []float32{1.0, 0.0, 0.0},
		},
		expectedDims: 3,
	}
	e.initialized = true

	// Test AddVector
	err := e.AddVector(context.Background(), "test1", []float32{1.0, 0.0, 0.0})
	if err != nil {
		t.Fatalf("AddVector error: %v", err)
	}

	// Test AddDocument (uses mock embedder)
	err = e.AddDocument(context.Background(), "test2", "hello")
	if err != nil {
		t.Fatalf("AddDocument error: %v", err)
	}

	if !e.HasDocument("test1") || !e.HasDocument("test2") {
		t.Errorf("documents should exist")
	}

	if e.Len() != 2 {
		t.Errorf("expected 2 docs, got %d", e.Len())
	}

	vec, ok := e.GetVector("test1")
	if !ok || len(vec) == 0 {
		t.Errorf("GetVector failed")
	}

	if e.RequiresHydration() {
		t.Errorf("Should not require hydration")
	}

	// Test Search
	res, err := e.Search(context.Background(), "query", 10)
	if err != nil {
		t.Fatalf("Search error: %v", err)
	}
	if len(res) == 0 {
		t.Errorf("Search should return results")
	}

	// Test SearchWithScores
	resScores, err := e.SearchWithScores(context.Background(), "query", 10)
	if err != nil {
		t.Fatalf("SearchWithScores error: %v", err)
	}
	if len(resScores) == 0 {
		t.Errorf("SearchWithScores should return results")
	}

	// Test SearchByNode
	resNode, err := e.SearchByNode(context.Background(), "test1", 10)
	if err != nil {
		t.Fatalf("SearchByNode error: %v", err)
	}
	if len(resNode) == 0 {
		// Only test2 is left since it skips itself
		t.Errorf("SearchByNode should return results")
	}

	// Test DeleteDocument
	if !e.DeleteDocument("test1") {
		t.Errorf("DeleteDocument failed")
	}

	// It's just marked as tombstone, still in graph until heal or prune
	if _, deleted := e.tombstones.Load("test1"); !deleted {
		t.Errorf("tombstone missing")
	}

	// Force heal
	e.healIfCorrupt()

	// Prune
	e.PruneOrphanedNodes(map[string]bool{"test2": true})

	if e.Len() != 1 {
		t.Errorf("expected 1 doc after prune, got %d", e.Len())
	}

	// Save
	e.dbPath = t.TempDir() + "/db.bin"
	err = e.Save()
	if err != nil {
		t.Fatalf("Save error: %v", err)
	}
	_, err = os.Stat(e.dbPath)
	if err != nil {
		t.Errorf("expected db file to exist: %v", err)
	}
}

func TestUpsertDocumentStaleSkipTelemetry(t *testing.T) {
	telemetry.SearchMetrics.CacheHits.Store(0)
	telemetry.SearchMetrics.CacheMisses.Store(0)
	telemetry.SearchMetrics.VectorStaleSkips.Store(0)

	e := &Engine{
		graph: createHNSWGraph(),
		embedder: &mockEmbedder{
			vector: []float32{1.0, 0.0, 0.0},
		},
		expectedDims: 3,
	}
	e.initialized = true

	ctx := context.Background()
	hash := "abc123"
	if err := e.UpsertDocument(ctx, "tool1", "hello world", hash); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	if telemetry.SearchMetrics.CacheMisses.Load() != 1 {
		t.Fatalf("expected 1 cache miss, got %d", telemetry.SearchMetrics.CacheMisses.Load())
	}

	if err := e.UpsertDocument(ctx, "tool1", "hello world", hash); err != nil {
		t.Fatalf("second upsert: %v", err)
	}
	if telemetry.SearchMetrics.CacheHits.Load() != 1 {
		t.Fatalf("expected 1 cache hit, got %d", telemetry.SearchMetrics.CacheHits.Load())
	}
	if telemetry.SearchMetrics.VectorStaleSkips.Load() != 1 {
		t.Fatalf("expected 1 stale skip, got %d", telemetry.SearchMetrics.VectorStaleSkips.Load())
	}
}

func TestUpsertDocumentTombstoneRecovery(t *testing.T) {
	telemetry.SearchMetrics.CacheHits.Store(0)
	telemetry.SearchMetrics.CacheMisses.Store(0)

	e := &Engine{
		graph: createHNSWGraph(),
		embedder: &mockEmbedder{
			vector: []float32{1.0, 0.0, 0.0},
		},
		expectedDims: 3,
	}
	e.initialized = true

	ctx := context.Background()
	hash := "abc123"
	if err := e.UpsertDocument(ctx, "tool1", "hello world", hash); err != nil {
		t.Fatalf("first upsert: %v", err)
	}
	e.DeleteDocument("tool1")
	if e.isActiveDocument("tool1") {
		t.Fatal("tombstoned document should not be active")
	}

	if err := e.UpsertDocument(ctx, "tool1", "hello world", hash); err != nil {
		t.Fatalf("upsert after tombstone: %v", err)
	}
	if telemetry.SearchMetrics.CacheMisses.Load() != 2 {
		t.Fatalf("expected 2 cache misses (re-insert after tombstone), got %d", telemetry.SearchMetrics.CacheMisses.Load())
	}
	if !e.isActiveDocument("tool1") {
		t.Fatal("document should be active after tombstone recovery upsert")
	}
}

func TestAddVectorDimensionMismatch(t *testing.T) {
	e := &Engine{
		graph:        createHNSWGraph(),
		expectedDims: 3,
	}
	e.initialized = true

	err := e.AddVector(context.Background(), "bad", []float32{1.0, 0.0})
	if err == nil {
		t.Fatal("expected dimension mismatch error")
	}
}
