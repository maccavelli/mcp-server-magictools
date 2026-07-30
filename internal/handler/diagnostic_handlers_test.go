package handler

import (
	"context"
	"encoding/json"
	"testing"

	"os"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDiagnosticHandlersExtra(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "handler-diag-*")
	defer os.RemoveAll(tempDir)

	store, _ := db.NewStore(tempDir)
	store.Cache = db.NewRegistryCache(10)
	defer store.Close()

	cfg := &config.Config{}
	wr := client.NewWarmRegistry(tempDir, store, cfg)

	alignCache, _ := lru.New[string, *AlignCacheEntry](10)
	h := &OrchestratorHandler{
		Config:     cfg,
		Registry:   wr,
		Store:      store,
		Responses:  db.NewResponseCache(10),
		AlignCache: alignCache,
		Telemetry:  telemetry.NewTracker(),
	}

	callReq := func(args map[string]any) *mcp.CallToolRequest {
		req := &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{},
		}
		req.Params.Arguments, _ = json.Marshal(args)
		return req
	}

	h.ListToolsInfo(context.Background(), callReq(map[string]any{"server_name": "recall"}))
	h.GetHealthReport(context.Background(), callReq(map[string]any{}))
	h.SelfCheck(context.Background(), callReq(map[string]any{}))
	h.UpdateConfig(context.Background(), callReq(map[string]any{"key": "logLevel", "value": "DEBUG"}))
	h.AnalyzeSystemLogs(context.Background(), callReq(map[string]any{"lines": 10}))
	h.GetSessionStats(context.Background(), callReq(map[string]any{}))
	h.QueryCompliance(context.Background(), callReq(map[string]any{"query": "test"}))
}

func TestSelfCheckVectorTelemetryFields(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "handler-diag-vector-*")
	defer os.RemoveAll(tempDir)

	store, _ := db.NewStore(tempDir)
	store.Cache = db.NewRegistryCache(10)
	defer store.Close()

	cfg := &config.Config{}
	if err := vector.InitGlobalEngine(tempDir, cfg); err != nil {
		t.Fatalf("InitGlobalEngine: %v", err)
	}
	wr := client.NewWarmRegistry(tempDir, store, cfg)
	alignCache, _ := lru.New[string, *AlignCacheEntry](10)
	h := &OrchestratorHandler{
		Config:     cfg,
		Registry:   wr,
		Store:      store,
		Responses:  db.NewResponseCache(10),
		AlignCache: alignCache,
		Telemetry:  telemetry.NewTracker(),
	}

	res, err := h.SelfCheck(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{Name: "self_check"},
	})
	if err != nil {
		t.Fatalf("SelfCheck: %v", err)
	}
	text := ""
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text = tc.Text
		}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		t.Fatalf("parse self_check: %v", err)
	}
	vectorBlock, ok := payload["vector"].(map[string]any)
	if !ok {
		t.Fatal("expected vector block in self_check")
	}
	for _, key := range []string{
		"vector_searches", "vector_search_attempts", "vector_search_errors",
		"gate_rejections", "fallback_invocations", "fallback_rescues", "last_fusion_winner",
	} {
		if _, exists := vectorBlock[key]; !exists {
			t.Fatalf("self_check vector missing %q", key)
		}
	}
}
