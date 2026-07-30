package handler

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestResolveURNNoCrossServerRemap(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ps := NewProxyService(h)

	if err := store.SaveTool(&db.ToolRecord{URN: "recall:search", Name: "search", Server: "recall", Description: "recall search"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTool(&db.ToolRecord{URN: "ddg-search:search_web", Name: "search_web", Server: "ddg-search", Description: "web search"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTool(&db.ToolRecord{URN: "s1:toolA", Name: "toolA", Server: "s1"}); err != nil {
		t.Fatal(err)
	}

	_, _, urn, _, err := ps.ResolveURN(context.Background(), "ddg-search:search_web")
	if err != nil || urn != "ddg-search:search_web" {
		t.Fatalf("expected exact URN resolve, got urn=%q err=%v", urn, err)
	}

	_, _, _, _, err = ps.ResolveURN(context.Background(), "ddg-search:search")
	if err == nil {
		t.Fatal("expected error for wrong tool on ddg-search, not cross-server remap")
	}
	if !strings.Contains(err.Error(), "ddg-search") {
		t.Fatalf("expected server-scoped error, got: %v", err)
	}
	if strings.Contains(err.Error(), "recall:search") {
		t.Fatalf("must not suggest recall tool for ddg-search URN: %v", err)
	}

	_, _, _, _, err = ps.ResolveURN(context.Background(), "wrong:toolA")
	if err == nil {
		t.Fatal("wrong:toolA must not silently resolve to s1:toolA")
	}
	if strings.Contains(err.Error(), "s1:toolA") && !strings.Contains(err.Error(), "wrong") {
		t.Fatalf("unexpected remap to s1: %v", err)
	}
}
