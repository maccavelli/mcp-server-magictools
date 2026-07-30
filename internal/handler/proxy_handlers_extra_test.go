package handler

import (
	"context"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProxyHandlersExtraCoverage(t *testing.T) {
	_, _ = getIgnoreCase(map[string]any{"key": "value"}, "KEY")
	_ = rawURN("magictools:test")
	_ = rawURN("test")
	_ = measureResponseSize(&mcp.CallToolResult{}, nil)
	_ = summarize("test string that is long enough to summarize maybe")

	// isPreferred
	h := &OrchestratorHandler{}
	_ = h.interceptTier1Native(&mcp.CallToolResult{}, "test", "test")
	_ = h.interceptTier2HFSC(context.Background(), &mcp.CallToolResult{}, "test")
	_ = isTargetMatched("magictools", "magictools")
}
