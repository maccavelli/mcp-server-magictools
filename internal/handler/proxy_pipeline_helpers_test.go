package handler

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestResponseContainsMutationRequired(t *testing.T) {
	tests := []struct {
		name     string
		res      *mcp.CallToolResult
		expected bool
	}{
		{
			name:     "nil response",
			res:      &mcp.CallToolResult{},
			expected: false,
		},
		{
			name: "has in struct content",
			res: &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"mutation_required": true,
				},
			},
			expected: true,
		},
		{
			name: "has in text content",
			res: &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{Text: `some text "mutation_required":true more text`},
				},
			},
			expected: true,
		},
		{
			name: "does not have",
			res: &mcp.CallToolResult{
				StructuredContent: map[string]any{
					"mutation_required": false,
				},
				Content: []mcp.Content{
					&mcp.TextContent{Text: `some text`},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responseContainsMutationRequired(tt.res); got != tt.expected {
				t.Errorf("responseContainsMutationRequired() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestProxyEvaluateSqueezeBypass(t *testing.T) {
	h := &OrchestratorHandler{
		Config: &config.Config{
			SqueezeBypass: []string{"test"},
		},
	}

	if !h.proxyEvaluateSqueezeBypass("test:tool", true) {
		t.Error("expected true when bypassMinification is true")
	}

	if !h.proxyEvaluateSqueezeBypass("test:tool", false) {
		t.Error("expected true when matched bypass list")
	}

	if h.proxyEvaluateSqueezeBypass("other:tool", false) {
		t.Error("expected false when not matched")
	}
}

func TestProxyIsRingBufferTarget(t *testing.T) {
	h := &OrchestratorHandler{
		Config: &config.Config{
			RingBufferTargets: []string{"test"},
		},
	}

	if !h.proxyIsRingBufferTarget("test:tool") {
		t.Error("expected true for matched target")
	}

	if h.proxyIsRingBufferTarget("other:tool") {
		t.Error("expected false for unmatched target")
	}
}

func TestProxyExtractDiagnosticSizes(t *testing.T) {
	res := &mcp.CallToolResult{
		Meta: map[string]interface{}{
			"_diagnostics": map[string]interface{}{
				"raw_bytes":  int64(100),
				"post_bytes": int64(50),
			},
		},
	}

	raw, post := proxyExtractDiagnosticSizes(res)
	if raw != 100 || post != 50 {
		t.Errorf("expected 100, 50, got %d, %d", raw, post)
	}

	raw, post = proxyExtractDiagnosticSizes(&mcp.CallToolResult{})
	if raw != 0 || post != 0 {
		t.Errorf("expected 0, 0 for empty meta, got %d, %d", raw, post)
	}
}

func TestProxyCheckTokenBudget(t *testing.T) {
	h := &OrchestratorHandler{
		Config: &config.Config{
			TokenSpendThresh: 100,
		},
	}

	// Assuming 0 tokens spent
	res := h.proxyCheckTokenBudget()
	if res != nil {
		t.Error("expected nil when budget not exceeded")
	}
}

func TestProxyDispatchTrace(t *testing.T) {
	h := &OrchestratorHandler{}
	ctx := context.Background()
	_, _ = h.proxyBeginDispatchTrace(ctx, "server", "name", "corr-id")
	proxyEndDispatchTrace("corr-id", 100)
}

func TestProxyRecordDispatchFailure(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Arguments: []byte(`{}`),
		},
	}
	h.proxyRecordDispatchFailure("server", "name", "urn", "corr-id", req, 10, errors.New("test error"), "src")
}

func TestProxyFinalizeSuccess(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	res := &mcp.CallToolResult{}
	h.proxyFinalizeSuccess("server", "name", "urn", "corr-id", "cache-key", true, res, time.Now())
}

func TestProxyExecuteLoopback(t *testing.T) {
	h := &OrchestratorHandler{}
	ctx := context.Background()
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{},
	}
	res, err := h.proxyExecuteLoopback(ctx, req, "name", map[string]any{}, "urn", func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return &mcp.CallToolResult{}, nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if res == nil {
		t.Error("expected non-nil result")
	}
}

func TestProxyHandleMutationMandate(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	ps := &ProxyService{Handler: h}
	res := &mcp.CallToolResult{
		StructuredContent: map[string]any{
			"mutation_required": true,
		},
	}
	h.proxyHandleMutationMandate(ps, "test:urn", res, false, 100)
}
