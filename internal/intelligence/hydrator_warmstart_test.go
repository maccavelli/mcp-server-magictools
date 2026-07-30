package intelligence

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

// countingEmbedder records how many times Embed is called so a test can assert
// the (paid) API path was avoided.
type countingEmbedder struct {
	dims  int
	calls atomic.Int64
}

func (c *countingEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	c.calls.Add(1)
	v := make([]float32, c.dims)
	v[0] = 1 // valid finite, non-zero-norm vector
	return v, nil
}

func (c *countingEmbedder) Provider() string { return "count-mock" }

// TestVectorBackfill_WarmStartFromCache is the INT-2 regression: on a cold HNSW
// graph (Badger survived, vector blob wiped) the backfill sweep must repopulate
// tools whose cached embedding is still valid directly from the vec: cache,
// WITHOUT calling the embedder API. A tool whose content hash has drifted still
// goes through the API.
func TestVectorBackfill_WarmStartFromCache(t *testing.T) {
	// Isolate any best-effort engine.Save() side effect to a temp dir.
	t.Chdir(t.TempDir())

	store, err := db.NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	const dims = 8
	cfg := &config.Config{}
	cfg.Intelligence.EmbeddingProvider = "count-mock"
	cfg.Intelligence.EmbeddingModel = "test"
	cfg.Intelligence.VectorEnabled = true
	providerModel := cfg.Intelligence.EmbeddingProvider + ":" + cfg.Intelligence.EmbeddingModel

	emb := &countingEmbedder{dims: dims}
	prev := vector.GlobalEngine
	vector.GlobalEngine = vector.NewTestEngine(emb, dims)
	defer func() { vector.GlobalEngine = prev }()

	cachedVec := make([]float32, dims)
	cachedVec[1] = 1 // distinct from the embedder's output, finite, non-zero

	// Warm tool: persisted EmbeddingHash matches current content + a cached vector.
	warm := &db.ToolRecord{
		URN:         "srv:warm",
		Name:        "warm_tool",
		Server:      "srv",
		Description: "a cached tool whose embedding is still valid",
		Vector:      cachedVec,
	}
	warm.EmbeddingHash = db.ComputeEmbeddingHash(providerModel, db.BuildEmbeddingText(warm))
	if err := store.SaveTool(warm); err != nil {
		t.Fatal(err)
	}

	// Stale tool: cached vector present but the persisted hash no longer matches
	// the current content — must be re-embedded via the API.
	stale := &db.ToolRecord{
		URN:           "srv:stale",
		Name:          "stale_tool",
		Server:        "srv",
		Description:   "a tool whose content drifted",
		Vector:        cachedVec,
		EmbeddingHash: "stale-hash-does-not-match",
	}
	if err := store.SaveTool(stale); err != nil {
		t.Fatal(err)
	}

	e := vector.GlobalEngine
	if e.HasDocument("srv:warm") || e.HasDocument("srv:stale") {
		t.Fatal("graph should start cold")
	}

	runVectorBackfillSweep(context.Background(), store, cfg)

	if !e.HasDocument("srv:warm") {
		t.Error("warm tool was not hydrated into the graph")
	}
	if !e.HasDocument("srv:stale") {
		t.Error("stale tool was not hydrated into the graph")
	}
	// Exactly one embed call — the stale tool. The warm tool came from vec: cache.
	if got := emb.calls.Load(); got != 1 {
		t.Errorf("expected exactly 1 embedder call (stale only), got %d", got)
	}
}
