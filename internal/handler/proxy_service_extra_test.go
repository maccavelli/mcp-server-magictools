package handler

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProxyServiceMethods(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ps := NewProxyService(h)

	record := &db.ToolRecord{
		URN: "test:test",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"req_str"},
			"properties": map[string]any{
				"num":     map[string]any{"type": "number"},
				"int":     map[string]any{"type": "integer"},
				"str":     map[string]any{"type": "string", "enum": []any{"A", "B"}},
				"arr":     map[string]any{"type": "array"},
				"req_str": map[string]any{"type": "string"},
			},
		},
		ZeroValues: map[string]any{"zv": "123"},
	}

	// AutoCoerceArguments
	args := map[string]any{
		"num":     "1.23",
		"int":     "5",
		"str":     "a", // Should snap to A
		"arr":     nil,
		"req_str": 5, // coercion
		"extra":   "strip_me",
	}
	ps.AutoCoerceArguments(record, args)

	// ValidateArguments
	ps.ValidateArguments(context.Background(), "test:test", record, args)
	ps.ValidateArguments(context.Background(), "test:test", nil, args)

	// formatStructuredCorrectionError
	ps.formatStructuredCorrectionError("test:test", record, args, nil)

	// ResolveURN
	ps.ResolveURN(context.Background(), "magictools:test")
	ps.ResolveURN(context.Background(), "notfound:tool")

	// EnsureServerReady
	ps.EnsureServerReady(context.Background(), "magictools")

	// MinifyResponse
	res := &mcp.CallToolResult{
		StructuredContent: map[string]any{"test": "test"},
	}
	ps.MinifyResponse(context.Background(), res, "server", "tool", 0, 100, nil)

	// InspectResponse
	res2 := &mcp.CallToolResult{
		StructuredContent: map[string]any{
			"results":     []any{},
			"total_count": 0,
		},
	}
	ps.InspectResponse(context.Background(), res2, "server", "tool", nil)

	// unmarshalCallProxyArgs
	ps.unmarshalCallProxyArgs([]byte(`{"urn": "test", "arguments": {"a": 1}}`))
	ps.unmarshalCallProxyArgs([]byte(`{"urn": "test", "a": 1}`))                    // flat
	ps.unmarshalCallProxyArgs([]byte(`{"urn": "test", "arguments": "{\"a\": 1}"}`)) // double-encoded
}
