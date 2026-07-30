package handler

import (
	"context"
	"log/slog"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandlerExhaustive(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "handler-exhaustive-test")
	defer os.RemoveAll(tmpDir)

	store, _ := db.NewStore(tmpDir)
	cfg := &config.Config{ConfigPath: "fake"}
	reg := client.NewWarmRegistry(tmpDir, store, cfg)

	h := NewHandler(store, reg, cfg)
	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, &mcp.ServerOptions{})
	h.Register(s)

	// isSafeToCache("fake")
	// getCacheKey("fake", nil)

	h.OnConfigReloaded(cfg)
	lv := new(slog.LevelVar)
	h.SetLogLevel(lv)

	ctx := context.Background()

	var r2 mcp.Request = &mcp.ServerRequest[*mcp.CallToolParams]{
		Params: &mcp.CallToolParams{
			Name:      "test",
			Arguments: map[string]any{},
		},
	}

	f := h.ListToolsMiddleware(func(ctx context.Context, m string, rq mcp.Request) (mcp.Result, error) {
		return &mcp.ListToolsResult{}, nil
	})
	_, _ = f(ctx, "tools/list", nil)

	c := h.CallToolMiddleware(func(ctx context.Context, m string, rq mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{}, nil
	})

	_, _ = c(ctx, "tools/call", r2)

	r2.(*mcp.ServerRequest[*mcp.CallToolParams]).Params.Name = "magictools:get_internal_logs"
	_, _ = c(ctx, "tools/call", r2)

	r2.(*mcp.ServerRequest[*mcp.CallToolParams]).Params.Name = "magictools:analyze_system_logs"
	_, _ = c(ctx, "tools/call", r2)

	r2.(*mcp.ServerRequest[*mcp.CallToolParams]).Params.Name = "subserver:toolName"
	_, _ = c(ctx, "tools/call", r2)
}
