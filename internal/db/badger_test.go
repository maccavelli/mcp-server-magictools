package db

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestStoreTools(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-test")
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// 1. Save
	tool := &ToolRecord{
		URN:         "test:tool1",
		Name:        "tool1",
		Server:      "test",
		Description: "A test tool",
		Category:    "test",
	}
	if err := store.SaveTool(tool); err != nil {
		t.Fatalf("SaveTool failed: %v", err)
	}

	// 2. Search
	results, err := store.SearchTools(context.Background(), "test", "", "", 0.0, 0.5, DomainSystem, false)
	if err != nil {
		t.Fatalf("SearchTools failed: %v", err)
	}
	if len(results) != 1 || results[0].Name != "tool1" {
		t.Errorf("expected 1 result (tool1), got %v", results)
	}

	// 3. Purge
	if err := store.PurgeServerTools("test"); err != nil {
		t.Fatalf("PurgeServerTools failed: %v", err)
	}
	results, _ = store.SearchTools(context.Background(), "test", "", "", 0.0, 0.5, DomainSystem, false)
	if len(results) != 0 {
		t.Errorf("expected 0 results after purge, got %d", len(results))
	}
}

func TestRawResources(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-resource-test")
	defer os.RemoveAll(tmpDir)

	store, _ := NewStore(tmpDir)
	defer store.Close()

	id := "test-call-1"
	data := []byte("massive output that needs compression")

	// 1. Save
	if err := store.SaveRawResource(id, data); err != nil {
		t.Fatalf("SaveRawResource failed: %v", err)
	}

	// 2. Get
	retrieved, err := store.GetRawResource(id)
	if err != nil {
		t.Fatalf("GetRawResource failed: %v", err)
	}

	if string(retrieved) != string(data) {
		t.Errorf("got %q, want %q", string(retrieved), string(data))
	}

	// 3. Get non-existent
	_, err = store.GetRawResource("missing")
	if err == nil {
		t.Error("expected error for missing resource")
	}
}

func TestToolUsage(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-usage-test")
	defer os.RemoveAll(tmpDir)

	store, _ := NewStore(tmpDir)
	defer store.Close()

	urn := "test:tool1"
	tool := &ToolRecord{
		URN:  urn,
		Name: "tool1",
	}
	_ = store.SaveTool(tool)

	// Update usage
	store.UpdateToolUsage(urn)

	// Get and verify
	t2, _ := store.GetTool(urn)
	if t2.UsageCount != 1 {
		t.Errorf("expected usage 1, got %d", t2.UsageCount)
	}
}

func TestGetTool(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-get-test")
	defer os.RemoveAll(tmpDir)

	store, _ := NewStore(tmpDir)
	defer store.Close()

	urn := "test:tool1"
	tool := &ToolRecord{
		URN:         urn,
		Name:        "tool1",
		Description: "get test",
	}
	_ = store.SaveTool(tool)

	// 1. Found
	t2, err := store.GetTool(urn)
	if err != nil {
		t.Fatalf("GetTool failed: %v", err)
	}
	if t2.Name != "tool1" {
		t.Errorf("got %q, want %q", t2.Name, "tool1")
	}

	// 2. Not found
	_, err = store.GetTool("missing:tool")
	if err == nil {
		t.Error("expected error for missing tool")
	}
}

func TestWipeAll(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-wipe-test")
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// 1. Seed with data
	urn := "test:tool1"
	tool := &ToolRecord{
		URN:         urn,
		Name:        "tool1",
		Description: "wipe test",
	}
	_ = store.SaveTool(tool)
	_ = store.SaveRawResource("res1", []byte("data"))

	// 2. Verify data exists
	_, err = store.GetTool(urn)
	if err != nil {
		t.Fatalf("setup failed, tool not found: %v", err)
	}

	// 3. Wipe
	if err := store.WipeAll(); err != nil {
		t.Fatalf("WipeAll failed: %v", err)
	}

	// 4. Verify data is gone
	_, err = store.GetTool(urn)
	if err == nil {
		t.Error("expected tool to be gone after wipe")
	}

	_, err = store.GetRawResource("res1")
	if err == nil {
		t.Error("expected resource to be gone after wipe")
	}

	// 5. Verify index is empty (re-initialized)
	results, _ := store.SearchTools(context.Background(), "tool1", "", "", 0.0, 0.5, DomainSystem, false)
	if len(results) != 0 {
		t.Errorf("expected 0 search results after wipe, got %d", len(results))
	}
}

func TestIntelligence(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-intel-test")
	defer os.RemoveAll(tmpDir)
	store, _ := NewStore(tmpDir)
	defer store.Close()

	urn := "test:tool1"
	intel := &ToolIntelligence{AnalysisStatus: "hydrated", SyntheticIntents: []string{"test"}}
	err := store.SaveIntelligence(urn, intel)
	if err != nil {
		t.Fatalf("SaveIntelligence failed: %v", err)
	}

	data, err := store.GetIntelligence(urn)
	if err != nil {
		t.Fatalf("GetIntelligence failed: %v", err)
	}
	if data.AnalysisStatus != "hydrated" {
		t.Errorf("expected hydrated, got %s", data.AnalysisStatus)
	}
}

func TestToolMetricsAndTop(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-metrics-test")
	defer os.RemoveAll(tmpDir)
	store, _ := NewStore(tmpDir)
	defer store.Close()

	urn := "server1:tool1"
	_ = store.SaveTool(&ToolRecord{URN: urn, Server: "server1", Name: "tool1"})

	// Update metrics (success bool)
	store.UpdateToolMetrics(urn, true)
	store.UpdateToolMetrics(urn, false)

	tools, err := store.GetTopToolsForServer("server1", 10)
	// Just check it doesn't crash; the function might not populate tools if usage count is 0
	if err != nil {
		t.Fatalf("GetTopToolsForServer failed: %v", err)
	}
	_ = tools
}

func TestDiagnostics(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-diag-test")
	defer os.RemoveAll(tmpDir)
	store, _ := NewStore(tmpDir)
	defer store.Close()

	diag, err := store.GetExtendedDiagnostics()
	if err != nil {
		t.Fatalf("GetExtendedDiagnostics failed: %v", err)
	}
	_ = diag.TotalKeys // may be non-zero when indexing internal keys are present
}

func TestStartBackgroundGC(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-gc-test")
	defer os.RemoveAll(tmpDir)
	store, _ := NewStore(tmpDir)
	defer store.Close()

	ctx, cancel := context.WithCancel(context.Background())
	// Should not panic/block
	go store.StartBackgroundGC(ctx, 10*time.Millisecond)

	time.Sleep(30 * time.Millisecond)
	cancel()
}

func TestGetSchema(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "badger-schema-test")
	defer os.RemoveAll(tmpDir)
	store, _ := NewStore(tmpDir)
	defer store.Close()

	schema := map[string]any{"type": "object"}
	err := store.SaveSchema("schema1", schema)
	if err != nil {
		t.Fatalf("SaveSchema failed: %v", err)
	}

	ret, err := store.GetSchema("schema1")
	if err != nil {
		t.Fatalf("GetSchema failed: %v", err)
	}
	if ret["type"] != "object" {
		t.Errorf("expected type object")
	}
}

func TestServerSyncHash(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-hash-test")
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Initial get should be empty string
	hash := store.GetServerSyncHash("server1")
	if hash != "" {
		t.Errorf("expected empty hash, got %s", hash)
	}

	// Save and retrieve
	store.SaveServerSyncHash("server1", "hash123")
	hash2 := store.GetServerSyncHash("server1")
	if hash2 != "hash123" {
		t.Errorf("expected hash123, got %s", hash2)
	}
}

func TestWipeDatabase(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "magictools-wipe-test")
	defer os.RemoveAll(tmpDir)

	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.SaveTool(&ToolRecord{URN: "test:1", Server: "test"})
	store.WipeDatabase()

	tools := store.GetServerToolCount("test")
	if tools != 0 {
		t.Errorf("expected 0 tools after wipe, got %d", tools)
	}
}
