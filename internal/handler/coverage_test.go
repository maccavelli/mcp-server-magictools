package handler

import (
	"log/slog"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandlerCoverageSafe(t *testing.T) {
	p := t.TempDir()
	store, _ := db.NewStore(p)
	defer store.Close()

	cfg, _ := config.New("1.0", "")
	registry := client.NewWarmRegistry(p, store, cfg)

	h := NewHandler(store, registry, cfg)
	if h == nil {
		t.Fatal("expected handler")
	}

	h.SetLogLevel(new(slog.LevelVar))
	h.OnConfigReloaded(cfg)

	// test toSchemaMap
	schema1 := map[string]any{"type": "string"}
	_ = h.toSchemaMap(schema1)
	_ = h.toSchemaMap("test")
	_ = h.toSchemaMap(nil)

	// Register all tools onto a dummy server to hit all register logic
	srv := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1"}, &mcp.ServerOptions{})
	h.Register(srv)
	h.RegisterPipelineTools(srv)
	h.RegisterPrompts(srv)
}
