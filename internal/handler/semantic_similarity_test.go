package handler

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSemanticSimilarityAudit(t *testing.T) {
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
			Description: "searches for something in the code",
			Category:    "search",
		},
		{
			URN:         "test:tool2",
			Name:        "tool2",
			Server:      "test",
			Description: "searches for something in the code", // Duplicate description
			Category:    "search",
		},
	}
	for _, tool := range tools {
		if err := store.SaveTool(tool); err != nil {
			t.Fatalf("failed to save tool: %v", err)
		}
	}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "semantic_similarity",
			Arguments: json.RawMessage(`{}`),
		},
	}

	res, err := h.SemanticSimilarityAudit(ctx, req)
	if err != nil {
		t.Fatalf("SemanticSimilarityAudit failed: %v", err)
	}

	if len(res.Content) == 0 {
		t.Fatalf("expected results, got none")
	}
}
