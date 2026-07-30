package handler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestListToolsConcurrency(t *testing.T) {
	// Setup temporary store and registry
	tempDir := t.TempDir()
	store, err := db.NewStore(tempDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer store.Close()

	registry := client.NewWarmRegistry(tempDir, store, &config.Config{})
	h := NewHandler(store, registry, &config.Config{})

	// Create the middleware-wrapped handler
	methodHandler := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method == "tools/list" {
			return &mcp.ListToolsResult{Tools: []*mcp.Tool{}}, nil
		}
		return nil, nil
	}

	wrapped := h.ListToolsMiddleware(methodHandler)

	ctx := context.Background()
	req := &mcp.ServerRequest[*mcp.ListToolsParams]{}

	// 1. Run many concurrent ListTools calls
	var wg sync.WaitGroup
	numCalls := 100
	wg.Add(numCalls * 2)

	for i := range numCalls {
		go func() {
			defer wg.Done()
			_, err := wrapped(ctx, "tools/list", req)
			if err != nil {
				t.Errorf("ListTools failed: %v", err)
			}
		}()

		go func(idx int) {
			defer wg.Done()
			serverName := fmt.Sprintf("server-%d", idx)
			registry.RequestState(serverName, client.StatusHealthy)
			// Small churn
			time.Sleep(1 * time.Millisecond)
		}(i)
	}

	wg.Wait()
}
