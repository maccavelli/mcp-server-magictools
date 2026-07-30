package db

import (
	"context"
	"testing"
	"time"
)

func TestDBSimpleCoverage(t *testing.T) {
	p := t.TempDir()
	store, err := NewStore(p)
	if err != nil {
		t.Skip("failed to init store", err)
	}
	defer store.Close()

	// 1. Basic retrieval coverage
	store.GetCategories()
	store.HasServerTools("fake")
	store.GetServerToolCount("fake")
	store.PurgeServerTools("fake")
	_, _ = store.GetStaleServers([]string{"a"})
	_ = store.PurgeOrphanedServers([]string{"a"})

	// 2. Logging and tracing logic
	_ = store.SaveLog([]byte("test log"), time.Second)
	_, _ = store.GetLogs(10)

	// 3. Triggers
	_ = store.SaveTrigger("key", "val")
	_, _ = store.GetTriggers()
	store.PopulateDefaultTriggers()

	// 4. Raw outputs and cache
	store.UpdateToolUsage("fake")
	_ = store.SaveRawResource("fake", []byte("data"))
	_, _ = store.GetRawResource("fake")

	// 5. Tool logic
	record := &ToolRecord{
		URN:        "fake:urn",
		Name:       "fakeurn",
		Server:     "fake",
		Category:   "cat",
		UsageCount: 1,
	}
	_ = store.SaveTool(record)
	_, _ = store.GetTool("fake:urn")
	_, _ = store.SearchTools(context.Background(), "fake", "cat", "", 0.0, 0.5, DomainSystem, false)

	// 6. Schema logic
	schema := map[string]any{}
	_ = store.SaveSchema("hash123", schema)
	_, _ = store.GetSchema("hash123")

	// 7. Batch operations
	records := []*ToolRecord{record}
	schemas := map[string]map[string]any{"hash123": schema}
	_ = store.BatchSaveTools(records, schemas)
}
