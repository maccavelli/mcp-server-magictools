package handler

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListToolsMiddleware(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()

	// Mock next handler
	next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return &mcp.ListToolsResult{
			Tools: []*mcp.Tool{
				{Name: "internal_tool", Description: "desc"},
			},
		}, nil
	}

	mw := h.ListToolsMiddleware(next)

	t.Run("ToolsList", func(t *testing.T) {
		lruCache := util.NewS3FIFOCache[string, *mcp.Tool](20)
		lruCache.Add("sub:tool", &mcp.Tool{Name: "sub:tool", Description: "desc"})
		h.ActiveSessions.Store("active-session", lruCache)

		h.Registry.IsSynced.Store(true)

		res, err := mw(ctx, "tools/list", nil)
		if err != nil {
			t.Errorf("Middleware failed: %v", err)
		}
		listRes := res.(*mcp.ListToolsResult)
		foundSub := false
		for _, tool := range listRes.Tools {
			if tool.Name == "sub:tool" {
				foundSub = true
			}
		}
		if !foundSub {
			t.Error("expected sub:tool in list")
		}
	})

	t.Run("OtherMethod", func(t *testing.T) {
		_, err := mw(ctx, "other", nil)
		if err != nil {
			t.Errorf("Middleware failed on other method: %v", err)
		}
	})

	t.Run("PanicRecovery", func(t *testing.T) {
		panicMW := h.ListToolsMiddleware(func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			panic("test panic")
		})
		res, err := panicMW(ctx, "tools/list", nil)
		if err != nil {
			t.Errorf("Expected nil error after recovery, got %v", err)
		}
		if res == nil {
			t.Fatal("Expected non-nil result after recovery")
		}
	})
}

func TestCallToolMiddleware(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()

	next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "base"}}}, nil
	}

	mw := h.CallToolMiddleware(next)

	t.Run("InternalTool", func(t *testing.T) {
		req := &mcp.ServerRequest[*mcp.CallToolParams]{Params: &mcp.CallToolParams{Name: "internal"}}
		res, err := mw(ctx, "tools/call", req)
		if err != nil {
			t.Errorf("Internal call failed: %v", err)
		}
		text := res.(*mcp.CallToolResult).Content[0].(*mcp.TextContent).Text
		if text != "base" {
			t.Errorf("expected base response, got %s", text)
		}
	})

	t.Run("NamespacedTool_NotFound", func(t *testing.T) {
		req := &mcp.ServerRequest[*mcp.CallToolParams]{Params: &mcp.CallToolParams{Name: "missing:tool"}}
		_, err := mw(ctx, "tools/call", req)
		if err == nil {
			t.Error("expected error for missing server")
		}
	})

	t.Run("OtherMethod", func(t *testing.T) {
		_, err := mw(ctx, "other", nil)
		if err != nil {
			t.Errorf("Middleware failed on other method: %v", err)
		}
	})
}
