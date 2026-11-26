//go:build magictoolstestseam

package handler

import (
	"testing"
)

// TestSchemaDraftOnWire proves registered tools/list carries the explicit
// 2020-12 $schema on every own tool's inputSchema — the sanitiser passes it
// through on the real registration path (MADR 0015 D4, Confirmation 2).
// get_internal_logs is mcplib-registered: its wire copy carries no $schema
// (MADR S5); the inventory copy is pinned by TestInventorySchemaDraft.
func TestSchemaDraftOnWire(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	defer store.Close()
	tools := emittedToolsList(t, h)
	want := d3Titles() // the own-tool name set

	seen := 0
	for _, tl := range tools {
		if _, ok := want[tl.Name]; !ok {
			continue // proxied tools are out of scope
		}
		m := schemaMap(t, tl)
		got, has := m["$schema"]
		if tl.Name == "get_internal_logs" {
			if has {
				t.Errorf("get_internal_logs: mcplib wire copy carries $schema %v, must be absent (S5)", got)
			}
			continue
		}
		if !has {
			t.Errorf("%s: wire schema has no $schema (sanitiser stripped it?)", tl.Name)
			continue
		}
		if got != schemaDraft202012 {
			t.Errorf("%s: wire $schema = %v, want %q", tl.Name, got, schemaDraft202012)
		}
		seen++
	}
	if seen != len(want)-1 {
		t.Fatalf("saw %d own schemas with $schema on the wire, want %d (all but mcplib's get_internal_logs)", seen, len(want)-1)
	}
}

// TestSchemaDraftOnStaticFallback proves the fallback emission path carries
// the same $schema declaration (the schema map is shared with the inventory,
// not copied; MADR S7: the two layers must agree).
func TestSchemaDraftOnStaticFallback(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	defer store.Close()
	l := &listToolsLayers{h: h}
	tools := l.staticInventory()
	want := d3Titles()

	if len(tools) != len(want) {
		t.Fatalf("fallback returned %d tools, want %d", len(tools), len(want))
	}
	for _, tl := range tools {
		if _, ok := want[tl.Name]; !ok {
			t.Errorf("%s: not an own tool", tl.Name)
			continue
		}
		m := schemaMap(t, tl)
		if got := m["$schema"]; got != schemaDraft202012 {
			t.Errorf("%s: fallback $schema = %v, want %q", tl.Name, got, schemaDraft202012)
		}
	}
}
