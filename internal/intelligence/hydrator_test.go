package intelligence

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/llm"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestUpdateToolStatus(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-intel-test")
	defer os.RemoveAll(tmpDir)

	store, err := db.NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tool := &db.ToolRecord{
		URN:        "plugin:foo",
		SchemaHash: "hash123",
	}

	updateToolStatus(store, tool, "failed")

	intel, err := store.GetIntelligence("plugin:foo")
	if err != nil {
		t.Fatal(err)
	}
	if intel == nil {
		t.Fatal("expected intelligence to be stored")
	}
	if intel.AnalysisStatus != "failed" {
		t.Errorf("expected 'failed', got %s", intel.AnalysisStatus)
	}
}

func TestNormalizeProxyScores(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-intel-test2")
	defer os.RemoveAll(tmpDir)

	store, err := db.NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Setup native/non-native records in the store for the normalizer to read
	_ = store.SaveTool(&db.ToolRecord{URN: "p:t1", IsNative: false})
	_ = store.SaveTool(&db.ToolRecord{URN: "p:t2", IsNative: false})
	_ = store.SaveTool(&db.ToolRecord{URN: "p:t3", IsNative: false})
	_ = store.SaveTool(&db.ToolRecord{URN: "p:native", IsNative: true})

	store.SaveIntelligence("p:t1", &db.ToolIntelligence{Metrics: db.ToolMetrics{ProxyReliability: 0.1}})
	store.SaveIntelligence("p:t2", &db.ToolIntelligence{Metrics: db.ToolMetrics{ProxyReliability: 0.5}})
	store.SaveIntelligence("p:t3", &db.ToolIntelligence{Metrics: db.ToolMetrics{ProxyReliability: 0.9}})
	store.SaveIntelligence("p:native", &db.ToolIntelligence{Metrics: db.ToolMetrics{ProxyReliability: 1.5}})

	normalizeProxyScores(store)

	intel1, _ := store.GetIntelligence("p:t1")
	intelNative, _ := store.GetIntelligence("p:native")

	if intel1 != nil && intel1.Metrics.ProxyReliability == 0.1 {
		t.Errorf("expected score to be normalized, but remained 0.1")
	}
	if intelNative != nil && intelNative.Metrics.ProxyReliability != 1.5 {
		t.Errorf("expected native tool to retain 1.5, got %v", intelNative.Metrics.ProxyReliability)
	}
}

func TestProbeLLMUnknown(t *testing.T) {
	cfg := &config.Config{}
	cfg.Intelligence.Provider = "unknown_provider"
	err := probeLLMAvailability(context.Background(), cfg)
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestInitProvidersUnknown(t *testing.T) {
	cfg := &config.Config{}
	cfg.Intelligence.Provider = "unknown_provider"
	cfg.Intelligence.Model = "fake-model"
	m := initProviders(context.Background(), cfg, nil)
	if len(m) != 0 {
		t.Error("expected empty map for unknown provider")
	}
}

func TestProcessPendingQueue(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-intel-test3")
	defer os.RemoveAll(tmpDir)

	store, err := db.NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	cfg := &config.Config{}
	cfg.Intelligence.Provider = "unknown_provider"

	store.PendingHydrations.Store(1)

	// Fast exit test
	RunSweep(context.Background(), store, cfg, nil)
}

func TestRunSweepShortCircuit(t *testing.T) {
	cfg := &config.Config{}
	cfg.Intelligence.Provider = ""

	// Should return instantly
	RunSweep(context.Background(), nil, cfg, nil)

	cfg.Intelligence.Provider = "test"
	cfg.Intelligence.APIKey = "123"

	// With a done context, should exit early
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	RunSweep(ctx, nil, cfg, nil)
}

func TestProcessPendingQueueNative(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-intel-test4")
	defer os.RemoveAll(tmpDir)

	store, _ := db.NewStore(tmpDir)
	defer store.Close()

	cfg := &config.Config{}
	cfg.Intelligence.Provider = "unknown_provider"

	store.PendingHydrations.Store(1)

	tool := &db.ToolRecord{
		URN:      "plugin:native",
		IsNative: true,
	}
	// Manually inject a tool to bypass early return
	_ = store.SaveTool(tool)

	RunSweep(context.Background(), store, cfg, nil)
}

func TestMineRecallPatternsNoClient(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-intel-test-mine")
	defer os.RemoveAll(tmpDir)

	store, err := db.NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Should be a safe no-op with nil RecallMiner
	MineRecallPatterns(context.Background(), nil, store)

	// Should be a safe no-op with nil store
	MineRecallPatterns(context.Background(), nil, nil)
}

func TestCalibrateFromRecallNoClient(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-intel-test-calibrate")
	defer os.RemoveAll(tmpDir)

	store, err := db.NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Should be a safe no-op with nil RecallMiner — no panic, no score changes
	CalibrateFromRecall(context.Background(), nil, store)

	// Should be a safe no-op with nil store
	CalibrateFromRecall(context.Background(), nil, nil)
}

func TestHydrateVectorGraphNilEngine(t *testing.T) {
	hydrateVectorGraph(context.Background(), nil, nil, []*db.ToolRecord{
		{URN: "test:tool"},
	})
}

func TestApplySemanticAugmentationNilProvider(t *testing.T) {
	cfg := &config.Config{}
	cfg.Intelligence.Provider = "test"

	_, err := applySemanticAugmentation(context.Background(), &db.ToolRecord{}, cfg, map[string]llm.Provider{})
	if err == nil {
		t.Error("expected error for missing provider")
	}
}

func TestRunSweep(t *testing.T) {
	// Call RunSweep with nil dependencies to ensure it bails out cleanly.
	cfg := &config.Config{}
	cfg.Intelligence.Provider = "test"
	cfg.Intelligence.APIKey = "test"
	RunSweep(context.Background(), nil, cfg, nil)
}
