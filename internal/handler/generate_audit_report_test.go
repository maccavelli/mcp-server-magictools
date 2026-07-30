package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestHandleGenerateAuditReport(t *testing.T) {
	h := &OrchestratorHandler{}

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{},
	}
	args := map[string]string{
		"session_id": "test-session",
		"target":     ".",
	}
	req.Params.Arguments, _ = json.Marshal(args)

	res, err := h.handleGenerateAuditReport(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatalf("unexpected error result")
	}
}
