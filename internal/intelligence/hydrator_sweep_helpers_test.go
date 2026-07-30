package intelligence

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/llm"

	"github.com/dgraph-io/badger/v4"
)

func TestHydratorSweepHelpers_ShouldRun(t *testing.T) {
	d, err := os.MkdirTemp("", "sweep_test_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, storeErr := db.NewStore(d)
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	defer store.Close()

	if shouldRunHydrationSweep(store) {
		t.Error("expected shouldRunHydrationSweep to be false initially with no vector engine")
	}

	store.PendingHydrations.Store(5)
	if !shouldRunHydrationSweep(store) {
		t.Error("expected true when pending hydrations exist")
	}
}

func TestHydratorSweepHelpers_CapBatch(t *testing.T) {
	d, err := os.MkdirTemp("", "sweep_test_cap_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, storeErr := db.NewStore(d)
	if storeErr != nil {
		t.Fatal(storeErr)
	}
	defer store.Close()

	var targets []*db.ToolRecord
	for i := 0; i < 10; i++ {
		targets = append(targets, &db.ToolRecord{})
	}

	capped, hasMore := capHydrationBatch(store, targets)
	if !hasMore {
		t.Error("expected hasMore to be true")
	}
	if len(capped) != 5 {
		t.Errorf("expected batch size 5, got %d", len(capped))
	}

	targets2 := []*db.ToolRecord{{}, {}}
	capped2, hasMore2 := capHydrationBatch(store, targets2)
	if hasMore2 {
		t.Error("expected hasMore to be false")
	}
	if len(capped2) != 2 {
		t.Errorf("expected batch size 2, got %d", len(capped2))
	}
}

func TestHydratorSweepHelpers_NativeSemantics(t *testing.T) {
	cases := []string{
		toolAlignTools,
		"call_proxy",
		"execute_pipeline",
		"sync_ecosystem",
		"list_tools",
		"get_health_report",
		"unknown_tool",
	}
	for _, c := range cases {
		i, tks := nativeToolSemantics(c)
		if len(i) == 0 || len(tks) == 0 {
			t.Errorf("expected semantics for %s", c)
		}
	}
}

func TestHydratorSweepHelpers_RecoverHNSW(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("recoverHNSWCall leaked panic: %v", r)
		}
	}()
	recoverHNSWCall(func() {
		panic("test panic")
	}, "test label")
}

func TestHydratorSweepHelpers_Pace(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	start := time.Now()
	paceHydrationLaunch(ctx)
	if time.Since(start) > 500*time.Millisecond {
		t.Error("expected pace to return immediately on cancelled context")
	}
}

func TestHydratorSweepHelpers_AcquireSlot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sem := make(chan struct{}, 1)

	if !acquireHydrationSlot(ctx, sem) {
		t.Error("expected to acquire slot")
	}

	cancel()
	if acquireHydrationSlot(ctx, sem) {
		t.Error("expected failure to acquire slot after cancel")
	}
}

func TestHydratorSweepHelpers_LogTrigger(t *testing.T) {
	// Simple coverage for log
	logHydrationTrigger(db.ToolRecord{URN: "test:a"}, nil, nil)
	logHydrationTrigger(db.ToolRecord{URN: "test:b"}, &db.ToolIntelligence{}, nil)
}

func TestHydratorSweepHelpers_RunHNSW(t *testing.T) {
	runHNSWBackfillIfNeeded(context.Background(), nil, &config.Config{}, nil) // empty slice
}

func TestHydratorSweepHelpers_HydrateNativeTool(t *testing.T) {
	d, err := os.MkdirTemp("", "sweep_test_native_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	tool := &db.ToolRecord{
		URN:        "test:" + toolAlignTools,
		Name:       toolAlignTools,
		IsNative:   true,
		SchemaHash: "hash123",
	}

	if !hydrateNativeTool(store, tool) {
		t.Error("expected true")
	}

	intel, err := store.GetIntelligence("test:" + toolAlignTools)
	if err != nil {
		t.Fatal(err)
	}
	if intel.AnalysisStatus != "hydrated" {
		t.Errorf("expected hydrated, got %v", intel.AnalysisStatus)
	}
	if intel.Metrics.ProxyReliability != 2.0 {
		t.Errorf("expected reliability 2.0, got %v", intel.Metrics.ProxyReliability)
	}
}

func TestHydratorSweepHelpers_ScanTargets(t *testing.T) {
	d, err := os.MkdirTemp("", "sweep_test_scan_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	// Seed tool
	tool := &db.ToolRecord{
		URN:        "test:a",
		Name:       "a",
		SchemaHash: "hash1",
	}
	store.SaveTool(tool)

	cfg := &config.Config{}
	res := scanHydrationTargets(store, cfg)
	if len(res.targets) != 1 {
		t.Errorf("expected 1 target, got %d", len(res.targets))
	}
}

func TestHydratorSweepHelpers_ExecuteSweep(t *testing.T) {
	d, err := os.MkdirTemp("", "sweep_test_exec_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	cfg := &config.Config{}

	tool1 := &db.ToolRecord{
		URN:      "test:a",
		Name:     toolAlignTools,
		IsNative: true,
	}
	tool2 := &db.ToolRecord{
		URN:      "test:b",
		Name:     "b",
		IsNative: true,
	}

	succ, fail := executeHydrationSweep(context.Background(), store, cfg, nil, []*db.ToolRecord{tool1, tool2})
	if succ != 2 {
		t.Errorf("expected 2 success, got %d", succ)
	}
	if fail != 0 {
		t.Errorf("expected 0 failure, got %d", fail)
	}
}

type mockProvider struct{}

func (m *mockProvider) Name() string { return "mock" }
func (m *mockProvider) Generate(ctx context.Context, prompt string) (string, error) {
	return `{"synthetic_intents":["i1"], "lexical_tokens":["t1"], "negative_triggers":["n1"]}`, nil
}

func TestHydratorSweepHelpers_HydrateSemanticTool(t *testing.T) {
	d, err := os.MkdirTemp("", "sweep_test_sem_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	tool := &db.ToolRecord{
		URN:        "test:foo",
		SchemaHash: "hash",
	}

	cfg := &config.Config{
		Intelligence: config.IntelligenceEngine{
			TimeoutSeconds: 5,
			Model:          "mock",
		},
	}
	providers := map[string]llm.Provider{
		"mock": &mockProvider{},
	}

	if !hydrateSemanticTool(context.Background(), store, cfg, providers, tool) {
		t.Error("expected true")
	}

	intel, _ := store.GetIntelligence("test:foo")
	if intel.AnalysisStatus != "hydrated" {
		t.Errorf("expected hydrated, got %v", intel.AnalysisStatus)
	}
	if len(intel.SyntheticIntents) == 0 || intel.SyntheticIntents[0] != "i1" {
		t.Error("expected synthetic intent i1")
	}
}

func TestHydratorSweepHelpers_SaveSemantic(t *testing.T) {
	d, err := os.MkdirTemp("", "sweep_test_save_semantic_*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	tool := &db.ToolRecord{
		URN:        "test:foo",
		SchemaHash: "hash123",
	}

	// Add existing intel with reliability to test preservation
	store.SaveIntelligence("test:foo", &db.ToolIntelligence{
		Metrics: db.ToolMetrics{ProxyReliability: 1.5},
	})

	res := &LLMResponse{
		SyntheticIntents: []string{"test"},
		LexicalTokens:    []string{"test"},
		NegativeTriggers: []string{"bad"},
	}

	if !saveSemanticHydration(store, tool, res) {
		t.Error("expected save to succeed")
	}

	intel, _ := store.GetIntelligence("test:foo")
	if intel.AnalysisStatus != "hydrated" {
		t.Error("expected hydrated status")
	}
	if intel.Metrics.ProxyReliability != 1.5 {
		t.Error("expected reliability 1.5 to be preserved")
	}
}

func TestHydratorSweepHelpers_LoadToolIntel(t *testing.T) {
	d, _ := os.MkdirTemp("", "sweep_intel_*")
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	// Missing
	store.DB.View(func(txn *badger.Txn) error {
		_, err := loadToolIntel(txn, "missing:urn")
		if err == nil {
			t.Error("expected error")
		}
		return nil
	})

	intel := &db.ToolIntelligence{AnalysisStatus: "hydrated"}
	store.SaveIntelligence("test:urn", intel)

	store.DB.View(func(txn *badger.Txn) error {
		loaded, err := loadToolIntel(txn, "test:urn")
		if err != nil {
			t.Error(err)
		}
		if loaded.AnalysisStatus != "hydrated" {
			t.Error("failed to load")
		}
		return nil
	})
}

func TestHydratorSweepHelpers_RunHNSWBackfillIfNeeded(t *testing.T) {
	d, _ := os.MkdirTemp("", "sweep_backfill_*")
	defer os.RemoveAll(d)
	store, _ := db.NewStore(d)
	defer store.Close()

	cfg := &config.Config{}

	// empty list
	runHNSWBackfillIfNeeded(context.Background(), store, cfg, nil)

	// panic recover
	targets := []*db.ToolRecord{{URN: "test:foo"}}
	runHNSWBackfillIfNeeded(context.Background(), store, cfg, targets)
}
