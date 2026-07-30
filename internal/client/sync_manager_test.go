package client

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSyncEcosystem_Empty(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "magictools-sync-test-*")
	defer os.RemoveAll(tempDir)

	store, _ := db.NewStore(tempDir)
	defer store.Close()
	cfg := &config.Config{}
	m := NewWarmRegistry(tempDir, store, cfg)

	_, err := m.SyncEcosystem(context.Background())
	if err != nil {
		t.Errorf("SyncEcosystem failed: %v", err)
	}
}

func TestSyncServer_NotFound(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "magictools-sync-test-*")
	defer os.RemoveAll(tempDir)

	store, _ := db.NewStore(tempDir)
	defer store.Close()
	cfg := &config.Config{}
	m := NewWarmRegistry(tempDir, store, cfg)

	err := m.SyncServer(context.Background(), "unknown")
	if err == nil {
		t.Error("expected error for unknown server")
	}
}

func TestSyncNativeTools(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "magictools-sync-test-*")
	defer os.RemoveAll(tempDir)
	store, _ := db.NewStore(tempDir)
	defer store.Close()

	cfg := &config.Config{}
	wr := NewWarmRegistry(tempDir, store, cfg)

	_, _ = wr.SyncNativeTools(context.Background(), &mcp.ListToolsResult{})
}

func TestStripEmbeddingBoilerplate(t *testing.T) {
	raw := "[DIRECTIVE: Tool Discovery Catalog] Resolves natural language intent. Keywords: find-tool tool-catalog"
	got := db.BuildEmbeddingText(&db.ToolRecord{Description: raw})
	want := "Resolves natural language intent."
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
