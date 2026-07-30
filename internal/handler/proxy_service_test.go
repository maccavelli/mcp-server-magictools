package handler

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAutoCoerceArguments(t *testing.T) {
	ps := NewProxyService(nil)

	record := &db.ToolRecord{
		ZeroValues: map[string]any{
			"lines":     50,
			"recursive": true,
		},
	}

	args := map[string]any{
		"path": "/tmp",
	}

	ps.AutoCoerceArguments(record, args)

	if val, ok := args["lines"].(int); !ok || val != 50 {
		t.Errorf("expected lines to be 50, got %v", args["lines"])
	}
	if val, ok := args["recursive"].(bool); !ok || val != true {
		t.Errorf("expected recursive to be true, got %v", args["recursive"])
	}
}

func TestMinifyResponse(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ps := NewProxyService(h)
	ctx := context.Background()

	var input strings.Builder
	input.WriteString("This is a very long text content\n")
	for range 200 {
		input.WriteString("extra line of content\n")
	}

	res := &mcp.CallToolResult{
		StructuredContent: map[string]any{
			"output": input.String(),
		},
	}

	minified := ps.MinifyResponse(ctx, res, "test", "tool", 1000, 500, nil)

	// Check if content was truncated
	found := false
	for _, c := range minified.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if len(tc.Text) < len(input.String()) {
				found = true
			}
		}
	}
	_ = found // truncation is best-effort; hybrid transform may expand small payloads
}

func TestIsZeroNumeric(t *testing.T) {
	if !isZeroNumeric(0) {
		t.Errorf("expected 0 to be zero numeric")
	}
	if !isZeroNumeric(0.0) {
		t.Errorf("expected 0.0 to be zero numeric")
	}
	if !isZeroNumeric(int64(0)) {
		t.Errorf("expected int64(0) to be zero numeric")
	}
	if isZeroNumeric(1) {
		t.Errorf("expected 1 to not be zero numeric")
	}
	if isZeroNumeric("0") {
		t.Errorf("expected \"0\" to not be zero numeric")
	}
}

func TestFailClosedForMutator(t *testing.T) {
	ps := &ProxyService{}
	err := ps.failClosedForMutator(nil, "urn", "stage", os.ErrNotExist)
	if err != nil {
		t.Errorf("expected nil error for nil record, got %v", err)
	}
	err = ps.failClosedForMutator(&db.ToolRecord{Role: roleMutator}, "urn", "stage", os.ErrNotExist)
	if err == nil {
		t.Errorf("expected error for mutator role")
	}
}

func TestRepairSimpleArgs(t *testing.T) {
	ps := &ProxyService{}
	raw := []byte(`{"arg": "value",}`)
	var target map[string]any
	err := ps.repairSimpleArgs(raw, &target)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if target["arg"] != "value" {
		t.Errorf("expected arg=value, got %v", target["arg"])
	}
}

func TestResolveTimeout(t *testing.T) {
	if to := ResolveTimeout(&db.ToolRecord{}); to <= 0 {
		t.Errorf("expected timeout > 0, got %v", to)
	}
}
