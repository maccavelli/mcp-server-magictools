package db

import (
	"bytes"
	"strings"
	"testing"
)

// TestUpdateToolUsage_SeparateKey is the BDG-3 regression: usage counters live
// under their own "usage:<urn>" key, so the hot per-call increment never rewrites
// the (large) tool: blob, and GetTool overlays the live count back.
func TestUpdateToolUsage_SeparateKey(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	rec := &ToolRecord{
		URN: "test:u1", Name: "u1", Server: "test", Category: "x",
		InputSchema: map[string]any{"big": strings.Repeat("x", 5000)}, // large blob
	}
	if err := store.SaveTool(rec); err != nil {
		t.Fatalf("SaveTool: %v", err)
	}

	blobBefore := rawGet(t, store, "tool:test:u1")
	if len(blobBefore) == 0 {
		t.Fatal("tool blob missing")
	}

	for range 3 {
		store.UpdateToolUsage("test:u1")
	}

	// The hot path must NOT rewrite the large tool: blob.
	if blobAfter := rawGet(t, store, "tool:test:u1"); !bytes.Equal(blobBefore, blobAfter) {
		t.Error("UpdateToolUsage rewrote the tool: blob (BDG-3 write-amplification not fixed)")
	}
	// The usage: key holds the counters.
	if len(rawGet(t, store, "usage:test:u1")) == 0 {
		t.Fatal("usage: key was not written")
	}

	// Cache-hit GetTool reflects the live count (cache kept fresh by the bumps).
	if got, _ := store.GetTool("test:u1"); got.UsageCount != 3 {
		t.Errorf("cache-hit GetTool UsageCount = %d, want 3", got.UsageCount)
	}
	// Cold GetTool overlays the count from the usage: key.
	store.Cache.Delete("tool:test:u1")
	if got, _ := store.GetTool("test:u1"); got.UsageCount != 3 {
		t.Errorf("cold GetTool UsageCount = %d, want 3 (overlay missed)", got.UsageCount)
	}
}

// TestUpdateToolUsage_MigratesLegacyCount verifies the first post-split increment
// seeds from the historical UsageCount embedded in a pre-existing tool blob, so
// usage history is not reset to zero on upgrade.
func TestUpdateToolUsage_MigratesLegacyCount(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// Legacy record with a non-zero historical count embedded in the blob.
	rec := &ToolRecord{URN: "test:u2", Name: "u2", Server: "test", Category: "x", UsageCount: 5}
	if err := store.SaveTool(rec); err != nil {
		t.Fatalf("SaveTool: %v", err)
	}

	store.UpdateToolUsage("test:u2") // first bump migrates 5 -> 6

	store.Cache.Delete("tool:test:u2")
	if got, _ := store.GetTool("test:u2"); got.UsageCount != 6 {
		t.Errorf("migrated UsageCount = %d, want 6", got.UsageCount)
	}
}
