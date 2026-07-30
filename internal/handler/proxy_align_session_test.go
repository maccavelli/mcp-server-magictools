package handler

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAlignTools_SessionlessWithSchema is the ALIGN-7 regression: a request with
// no session must not panic when rendering a tool that has a schema (the schema
// render path touches req.GetSession().ID()).
func TestAlignTools_SessionlessWithSchema(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	tool := &db.ToolRecord{
		URN:         "test:schematool",
		Name:        "schematool",
		Server:      "test",
		Description: "does a searchable thing",
		Category:    "search",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"x": map[string]any{"type": "string"}},
			"required":   []any{"x"},
		},
	}
	if err := store.SaveTool(tool); err != nil {
		t.Fatalf("save tool: %v", err)
	}

	// No session on the request — must not panic.
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "align_tools",
			Arguments: json.RawMessage(`{"query": "searchable thing"}`),
		},
	}
	res, err := h.AlignTools(context.Background(), req)
	if err != nil {
		t.Fatalf("AlignTools (sessionless) failed: %v", err)
	}
	if res == nil || len(res.Content) == 0 {
		t.Fatal("expected a non-empty result")
	}
}
