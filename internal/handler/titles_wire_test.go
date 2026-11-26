//go:build magictoolstestseam

package handler

import (
	"testing"
)

// TestTitlesOnWire proves registered tools/list carries each own tool's
// title as the inventory declares it (MADR D3). get_internal_logs is
// mcplib-registered: its wire copy carries no title (MADR S5); the inventory
// copy is pinned by TestInventoryTitles.
func TestTitlesOnWire(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	defer store.Close()
	tools := emittedToolsList(t, h)
	want := d3Titles()

	seen := 0
	for _, tl := range tools {
		title, ok := want[tl.Name]
		if !ok {
			continue // proxied tools are out of scope
		}
		if tl.Name == "get_internal_logs" {
			if tl.Title != "" {
				t.Errorf("get_internal_logs: mcplib wire copy carries title %q, must be absent (S5)", tl.Title)
			}
			continue
		}
		if tl.Title != title {
			t.Errorf("%s: wire title = %q, want %q", tl.Name, tl.Title, title)
		}
		seen++
	}
	if seen != len(want)-1 {
		t.Fatalf("saw %d titled own tools on the wire, want %d (all but mcplib's get_internal_logs)", seen, len(want)-1)
	}
}

// TestTitlesOnStaticFallback proves the fallback emission path carries the
// same titles as registration (MADR S7: the two layers must agree).
func TestTitlesOnStaticFallback(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	defer store.Close()
	l := &listToolsLayers{h: h}
	tools := l.staticInventory()
	want := d3Titles()

	if len(tools) != len(want) {
		t.Fatalf("fallback returned %d tools, want %d", len(tools), len(want))
	}
	for _, tl := range tools {
		title, ok := want[tl.Name]
		if !ok {
			t.Errorf("%s: not in the D3 table", tl.Name)
			continue
		}
		if tl.Title != title {
			t.Errorf("%s: fallback title = %q, want %q", tl.Name, tl.Title, title)
		}
	}
}
