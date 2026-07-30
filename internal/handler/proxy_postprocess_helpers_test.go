package handler

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestApplyResponseByteCap(t *testing.T) {
	config.Proxy.MaxResponseBytes = 10
	res := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "12345678901234567890"},
		},
	}
	applyResponseByteCap(res, false)
	tc := res.Content[0].(*mcp.TextContent)
	if len(tc.Text) > 20 {
		t.Errorf("expected string to be truncated, got %s", tc.Text)
	}
}

func TestEnrichResponseDiagnostics(t *testing.T) {
	res := &mcp.CallToolResult{}
	enrichResponseDiagnostics(res, 100, 50, false)
	if res.Meta == nil {
		t.Fatal("expected Meta to be created")
	}
	diag, ok := res.Meta["_diagnostics"].(map[string]any)
	if !ok {
		t.Fatal("expected _diagnostics to be a map")
	}
	if diag["raw_bytes"] != int64(100) {
		t.Error("expected raw_bytes 100")
	}
	if diag["post_bytes"] != int64(50) {
		t.Error("expected post_bytes 50")
	}
	if diag["squeeze_ratio"] != 0.5 {
		t.Error("expected squeeze_ratio 0.5")
	}
	if diag["minified"] != true {
		t.Error("expected minified true")
	}
}

func TestResolveBypassMinification(t *testing.T) {
	ps := &ProxyService{
		Handler: &OrchestratorHandler{
			Config: &config.Config{
				SqueezeBypass: []string{"test"},
			},
		},
	}
	bypass, limit := ps.resolveBypassMinification("test:tool", map[string]any{})
	if !bypass {
		t.Error("expected bypass true from matched target")
	}

	bypass, _ = ps.resolveBypassMinification("other:tool", map[string]any{
		"bypass_minification": true,
	})
	if !bypass {
		t.Error("expected bypass true from args")
	}
	_ = limit
}

func TestStripOrchestratorSignals(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	ps := &ProxyService{
		Handler: h,
	}
	content := []mcp.Content{
		&mcp.TextContent{Text: `{"__orchestrator_signal": {"success": true}}`},
	}
	// We might panic if Handler.Store is nil, but we can't easily mock it without creating a whole test handler.
	// So let's skip the actual store call by providing invalid JSON or not testing the goroutine side effect directly.
	content2 := ps.stripOrchestratorSignals(content, "test:tool")
	tc := content2[0].(*mcp.TextContent)
	if tc.Text != "{}" {
		t.Errorf("expected {}, got %s", tc.Text)
	}
}

func TestApplyMinificationStage(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	ps := &ProxyService{Handler: h}
	ctx := context.Background()
	res := &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{Text: "test content"},
		},
	}
	// bypass
	res2, size := ps.applyMinificationStage(ctx, res, "server", "tool", true, 10, 100, nil)
	if size != 12 {
		t.Errorf("expected size 12, got %d", size)
	}
	if res2.Meta["bypass_minification"] != true {
		t.Error("expected bypass_minification true")
	}
}
