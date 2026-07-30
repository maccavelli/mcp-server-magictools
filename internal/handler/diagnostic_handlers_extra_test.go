package handler

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDiagnosticHandlersCoverage(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	// Initialize Telemetry tracker for this test scope
	telemetry.GlobalTracker = telemetry.NewTracker()

	// Test GetHealthReport
	_, _ = h.GetHealthReport(context.Background(), &mcp.CallToolRequest{})

	// Test SelfCheck
	_, _ = h.SelfCheck(context.Background(), &mcp.CallToolRequest{})

	// Test GetSessionStats
	_, _ = h.GetSessionStats(context.Background(), &mcp.CallToolRequest{})

	// Test GetSystemLogs
	reqArgs := map[string]any{"lines": 10, "severity": "ERROR"}
	b, _ := json.Marshal(reqArgs)
	_, _ = h.AnalyzeSystemLogs(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "analyze_system_logs",
			Arguments: b,
		},
	})

	reqArgs2 := map[string]any{"server_id": "test", "lines": 5}
	b2, _ := json.Marshal(reqArgs2)
	_, _ = h.AnalyzeSystemLogs(context.Background(), &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "analyze_system_logs",
			Arguments: b2,
		},
	})
}
