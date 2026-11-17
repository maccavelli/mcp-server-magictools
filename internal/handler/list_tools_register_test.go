package handler

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// What 0005-MADR-make-execute-pipeline-schema-and-handler-agree.md relies on (row E-2 of
// the 0008 behaviour table), through the registration the binary uses
// (Register, RegisterPipelineTools, RegisterPrompts) and a real in-memory
// session: base tools come out as registered (sanitised once, at registration),
// and a tool the session cache holds comes out after them exactly as the cache
// holds it. The middleware sanitises, retypes and copies nothing.
func TestListToolsThroughRegister(t *testing.T) {
	e := newLSEnv(t)
	h := e.h
	ctx := context.Background()

	s := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	h.Register(s)
	h.RegisterPipelineTools(s)
	h.RegisterPrompts(s)

	ct, st := mcp.NewInMemoryTransports()
	ss, err := s.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	e.reset()
	e.synced(true)
	before, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(before.Tools) != len(h.InternalTools) {
		t.Fatalf("registered tools = %d, inventory = %d", len(before.Tools), len(h.InternalTools))
	}

	// A proxied tool as align_tools stores it: sanitised once, then cached.
	cached := &mcp.Tool{
		Name:        "srv:tool",
		Description: "proxied",
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"untyped": map[string]any{"description": "no type"}},
		},
	}
	wantCached := mustJSON(t, cached) // taken before the call: a sanitiser that edits in place must not hide itself
	c := e.cache("", 20, cached)      // the in-memory session's ID() is ""; see the L10 row
	after, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Tools) != len(before.Tools)+1 {
		t.Fatalf("tools = %d, want %d", len(after.Tools), len(before.Tools)+1)
	}
	for i := range before.Tools {
		if a, b := mustJSON(t, before.Tools[i]), mustJSON(t, after.Tools[i]); a != b {
			t.Fatalf("base tool %d changed when a session tool was added:\n%s\n%s", i, a, b)
		}
	}
	if got := mustJSON(t, after.Tools[len(after.Tools)-1]); got != wantCached {
		t.Fatalf("session tool was altered on the way out:\n got %s\nwant %s", got, wantCached)
	}

	// The middleware keeps no copy: a value the cache replaces (align_tools adds
	// the same URN again after a re-sync) is what the next call emits.
	replaced := &mcp.Tool{Name: "srv:tool", Description: "proxied again", InputSchema: map[string]any{"type": "object"}}
	wantReplaced := mustJSON(t, replaced)
	c.Add("srv:tool#ka", replaced)
	again, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustJSON(t, again.Tools[len(again.Tools)-1]); got != wantReplaced {
		t.Fatalf("a replaced cache value was not emitted:\n got %s\nwant %s", got, wantReplaced)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
