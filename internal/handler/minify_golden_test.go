package handler

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The raw-cache callID embeds time.Now().UnixNano(); normalize it so the golden
// is stable across runs while still capturing the full rendered output.
var (
	minifyCallIDRe = regexp.MustCompile(`-full-\d+`)
	minifyRawURIRe = regexp.MustCompile(`raw/[A-Za-z0-9_.\-]+`)
)

func normalizeMinify(s string) string {
	s = minifyCallIDRe.ReplaceAllString(s, "-full-N")
	s = minifyRawURIRe.ReplaceAllString(s, "raw/ID")
	return s
}

func buildMinifyCase(name string) *mcp.CallToolResult {
	switch name {
	case "small":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"a": 1, "b": "hello", "nested": map[string]any{"x": []any{1, 2, 3}}}}
	case "large":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"output": strings.Repeat("a long line of content here\n", 400)}}
	case "markdown_payload":
		return &mcp.CallToolResult{StructuredContent: map[string]any{"markdown_payload": "# Title\n\nsome body text for the report"}}
	case "error":
		return &mcp.CallToolResult{IsError: true, StructuredContent: map[string]any{"x": 1}}
	default: // nil_struct
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "plain text"}}}
	}
}

func renderMinifyResult(res *mcp.CallToolResult) string {
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
			b.WriteString("\n---CONTENT-SEP---\n")
		}
	}
	if res.StructuredContent != nil {
		sc, _ := json.Marshal(res.StructuredContent)
		b.WriteString("STRUCT:" + string(sc))
	}
	return b.String()
}

// TestMinifyResponse_Golden is the PROXY-M5 parity harness: it snapshots the
// rendered MinifyResponse output across representative payloads. The decode-once
// refactor must keep these byte-identical (regenerate with UPDATE_GOLDEN=1).
func TestMinifyResponse_Golden(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	ps := NewProxyService(h)
	ctx := context.Background()

	names := []string{"small", "large", "markdown_payload", "error", "nil_struct"}

	var out strings.Builder
	for _, name := range names {
		res := ps.MinifyResponse(ctx, buildMinifyCase(name), "srv", "tool", 1000, 500, nil)
		out.WriteString("=== " + name + " ===\n")
		out.WriteString(normalizeMinify(renderMinifyResult(res)))
		out.WriteString("\n")
	}
	got := out.String()

	golden := filepath.Join("testdata", "minify_golden.txt")
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden regenerated")
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (generate first with UPDATE_GOLDEN=1): %v", err)
	}
	wantText := strings.ReplaceAll(string(want), "\r\n", "\n")
	if got != wantText {
		t.Errorf("MinifyResponse output changed (M5 parity broken):\n--- got ---\n%s\n--- want ---\n%s", got, wantText)
	}
}

// TestMinifyResponse_PreMarshalParity is the PROXY-M5 guarantee: passing the
// correct pre-marshal yields byte-identical output to marshaling internally.
func TestMinifyResponse_PreMarshalParity(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()
	ps := NewProxyService(h)
	ctx := context.Background()

	for _, name := range []string{"small", "large", "markdown_payload"} {
		// Independent copies — MinifyResponse mutates StructuredContent in place.
		r1 := buildMinifyCase(name)
		r2 := buildMinifyCase(name)
		pre, _ := json.Marshal(r2.StructuredContent)

		outNil := normalizeMinify(renderMinifyResult(ps.MinifyResponse(ctx, r1, "srv", "tool", 1000, 500, nil)))
		outPre := normalizeMinify(renderMinifyResult(ps.MinifyResponse(ctx, r2, "srv", "tool", 1000, 500, pre)))
		if outNil != outPre {
			t.Errorf("[%s] pre-marshal path diverged from internal marshal:\n--- nil ---\n%s\n--- pre ---\n%s", name, outNil, outPre)
		}
	}
}
