package handler

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAlignTools(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()

	// Add some mock tools to the store
	tools := []*db.ToolRecord{
		{
			URN:         "test:tool1",
			Name:        "tool1",
			Server:      "test",
			Description: "searches for something",
			Category:    "search",
		},
		{
			URN:         "test:tool2",
			Name:        "tool2",
			Server:      "test",
			Description: "refactors code",
			Category:    "refactor",
		},
	}
	for _, tool := range tools {
		if err := store.SaveTool(tool); err != nil {
			t.Fatalf("failed to save tool: %v", err)
		}
	}

	// 1. Test basic search
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "align_tools",
			Arguments: json.RawMessage(`{"query": "search"}`),
		},
	}

	res, err := h.AlignTools(ctx, req)
	if err != nil {
		t.Fatalf("AlignTools failed: %v", err)
	}

	if len(res.Content) == 0 {
		t.Fatalf("expected results, got none")
	}

	// Verify content
	found := false
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if strings.Contains(tc.Text, "test:tool1") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected to find test:tool1 in results")
	}

	// 2. Test server filtering
	req = &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "align_tools",
			Arguments: json.RawMessage(`{"query": "refactor", "server_name": "test"}`),
		},
	}

	res, err = h.AlignTools(ctx, req)
	if err != nil {
		t.Fatalf("AlignTools failed: %v", err)
	}

	found = false
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if strings.Contains(tc.Text, "test:tool2") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected to find test:tool2 in filtered results")
	}
}

func TestIsAmbiguousURNFastPath(t *testing.T) {
	t.Parallel()
	if !isAmbiguousURNFastPath("recall", "recall", "recall:recall") {
		t.Fatal("recall+recall should be ambiguous")
	}
	if isAmbiguousURNFastPath("recall", "get_metrics", "recall:get_metrics") {
		t.Fatal("get_metrics should not be ambiguous")
	}
}

func TestAlignTools_withArguments_runsSearch(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()
	_ = store.SaveTool(&db.ToolRecord{
		URN:         "recall:get_metrics",
		Name:        "get_metrics",
		Server:      "recall",
		Description: "Returns recall health and telemetry metrics",
		Category:    "memory",
	})
	_ = store.SaveTool(&db.ToolRecord{
		URN:         "recall:recall",
		Name:        "recall",
		Server:      "recall",
		Description: "Fetch historical transcripts",
		Category:    "memory",
	})

	h.Config.ScoreThreshold = 0.0

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "align_tools",
			Arguments: json.RawMessage(`{"query": "metrics", "server_name": "recall", "arguments": {}}`),
		},
	}
	res, err := h.AlignTools(ctx, req)
	if err != nil {
		t.Fatalf("AlignTools failed: %v", err)
	}
	text := alignResultText(res)
	if strings.Contains(text, "NO_LOCAL_TOOL_MATCH") {
		t.Fatalf("expected search with arguments, got NO_LOCAL_TOOL_MATCH")
	}
	if !strings.Contains(text, "recall:get_metrics") {
		t.Fatalf("expected recall:get_metrics in results, got: %s", text)
	}
}

func TestAlignTools_serverNameNotToolName(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()
	_ = store.SaveTool(&db.ToolRecord{
		URN:         "recall:recall",
		Name:        "recall",
		Server:      "recall",
		Description: "Fetch historical transcripts",
		Category:    "memory",
	})
	_ = store.SaveTool(&db.ToolRecord{
		URN:         "recall:get_metrics",
		Name:        "get_metrics",
		Server:      "recall",
		Description: "Returns recall health metrics telemetry",
		Category:    "memory",
	})

	h.Config.ScoreThreshold = 0.0

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "align_tools",
			Arguments: json.RawMessage(`{"query": "recall", "server_name": "recall"}`),
		},
	}
	res, err := h.AlignTools(ctx, req)
	if err != nil {
		t.Fatalf("AlignTools failed: %v", err)
	}
	text := alignResultText(res)
	if strings.HasPrefix(text, `{"status":"NO_LOCAL_TOOL_MATCH"`) {
		t.Fatal("expected search results, not NO_LOCAL_TOOL_MATCH")
	}
	trace := telemetry.SearchMetrics.LastQueryTrace.Load()
	if trace != nil && trace.FastPath == "urn" && alignFirstToolURN(text) == "recall:recall" {
		t.Fatal("ambiguous URN fast path should not return recall:recall at confidence 1.0")
	}
}

func TestAlignTools_urnFastPath_get_metrics(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()
	_ = store.SaveTool(&db.ToolRecord{
		URN:         "recall:get_metrics",
		Name:        "get_metrics",
		Server:      "recall",
		Description: "Returns recall health metrics",
		Category:    "memory",
	})

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "align_tools",
			Arguments: json.RawMessage(`{"query": "get_metrics", "server_name": "recall"}`),
		},
	}
	res, err := h.AlignTools(ctx, req)
	if err != nil {
		t.Fatalf("AlignTools failed: %v", err)
	}
	if !strings.Contains(alignResultText(res), "recall:get_metrics") {
		t.Fatalf("expected recall:get_metrics via URN fast path")
	}
}

func alignResultText(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func alignFirstToolURN(text string) string {
	payload := alignExtractJSONPayload(text)
	var envelope struct {
		Tools []struct {
			URN string `json:"urn"`
		} `json:"tools"`
	}
	if json.Unmarshal([]byte(payload), &envelope) == nil && len(envelope.Tools) > 0 {
		return envelope.Tools[0].URN
	}
	return ""
}

func alignExtractJSONPayload(text string) string {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "```") {
		return text
	}
	start := strings.Index(text, "```json")
	if start < 0 {
		start = strings.Index(text, "```")
	}
	if start < 0 {
		return text
	}
	rest := text[start:]
	if nl := strings.Index(rest, "\n"); nl >= 0 {
		rest = rest[nl+1:]
	} else {
		return text
	}
	if before, _, ok := strings.Cut(rest, "\n```"); ok {
		return strings.TrimSpace(before)
	}
	return strings.TrimSpace(rest)
}

func TestAlignThenCallProxyE2E(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()
	_ = store.SaveTool(&db.ToolRecord{
		URN:         "test-server:echo",
		Name:        "echo",
		Server:      "test-server",
		Description: "Echo metrics and telemetry payloads",
		Category:    "test",
	})

	h.Config.ScoreThreshold = 0.0
	alignReq := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "align_tools",
			Arguments: json.RawMessage(`{"query": "metrics telemetry", "server_name": "test-server"}`),
		},
	}
	alignRes, err := h.AlignTools(ctx, alignReq)
	if err != nil {
		t.Fatalf("AlignTools: %v", err)
	}
	urn := alignFirstToolURN(alignResultText(alignRes))
	if urn == "" {
		t.Fatalf("expected URN from align envelope: %s", alignResultText(alignRes))
	}

	proxyArgs, _ := json.Marshal(map[string]any{"urn": urn, "arguments": map[string]any{}})
	proxyReq := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "call_proxy",
			Arguments: proxyArgs,
		},
	}
	_, err = h.CallProxy(ctx, proxyReq)
	if err == nil {
		t.Fatal("expected call_proxy error without live sub-server")
	}
}

func TestCallProxy(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()

	_ = store.SaveTool(&db.ToolRecord{
		URN:    "bad_server:tool",
		Name:   "tool",
		Server: "bad_server",
	})

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "call_proxy",
			Arguments: json.RawMessage(`{"urn": "bad_server:tool", "arguments": {}}`),
		},
	}

	_, err := h.CallProxy(ctx, req)
	if err == nil {
		t.Error("expected error for bad_server")
	}
}
