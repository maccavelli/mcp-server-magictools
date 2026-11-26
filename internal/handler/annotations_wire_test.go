//go:build magictoolstestseam

package handler

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestAnnotationsOnWire proves registered tools/list carries each tool's
// annotations as the SDK emits them (including omitempty of the plain-bool
// fields: a writer's idempotentHint:false is absent on the wire and the client
// applies the spec default false — the same value).
func TestAnnotationsOnWire(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	defer store.Close()
	tools := emittedToolsList(t, h)
	want := d1Annotations()

	seen := 0
	for _, tl := range tools {
		row, ok := want[tl.Name]
		if !ok {
			continue // proxied tools are out of scope
		}
		if tl.Name == "get_internal_logs" {
			// mcplib-registered: the wire copy carries no annotations (MADR S5);
			// the inventory copy is pinned by TestInventoryAnnotations.
			if tl.Annotations != nil {
				t.Errorf("get_internal_logs: unexpected annotations on the mcplib wire copy")
			}
			continue
		}
		seen++
		if tl.Annotations == nil {
			t.Errorf("%s: no annotations on the wire", tl.Name)
			continue
		}
		a := tl.Annotations
		if a.ReadOnlyHint != row.readOnly {
			t.Errorf("%s: wire readOnlyHint = %v, want %v", tl.Name, a.ReadOnlyHint, row.readOnly)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint != row.openWorld {
			t.Errorf("%s: wire openWorldHint = %v, want %v", tl.Name, a.OpenWorldHint, row.openWorld)
		}
		if row.destructive == nil {
			if a.DestructiveHint != nil {
				t.Errorf("%s: wire destructiveHint present for read-only tool", tl.Name)
			}
		} else if a.DestructiveHint == nil || *a.DestructiveHint != *row.destructive {
			t.Errorf("%s: wire destructiveHint = %v, want %v", tl.Name, a.DestructiveHint, *row.destructive)
		}
		if row.idempotent != nil && a.IdempotentHint != *row.idempotent {
			t.Errorf("%s: wire idempotentHint = %v, want %v", tl.Name, a.IdempotentHint, *row.idempotent)
		}
	}
	// get_internal_logs is mcplib-registered (no annotations); the other 17
	// must all be present.
	if seen != len(want)-1 {
		t.Fatalf("saw %d annotated own tools on the wire, want %d (all but mcplib's get_internal_logs)", seen, len(want)-1)
	}
}

// TestAnnotationsOnStaticFallback proves the fallback emission path carries
// the same annotations as registration (MADR S7: the two layers must agree).
func TestAnnotationsOnStaticFallback(t *testing.T) {
	h, store, _, _ := newTestHandler(t)
	defer store.Close()
	l := &listToolsLayers{h: h}
	tools := l.staticInventory()
	want := d1Annotations()

	if len(tools) != len(want) {
		t.Fatalf("fallback returned %d tools, want %d", len(tools), len(want))
	}
	for _, tl := range tools {
		row, ok := want[tl.Name]
		if !ok {
			t.Errorf("%s: not in the D1 table", tl.Name)
			continue
		}
		if tl.Annotations == nil {
			t.Errorf("%s: fallback carries no annotations", tl.Name)
			continue
		}
		a := tl.Annotations
		if a.ReadOnlyHint != row.readOnly {
			t.Errorf("%s: fallback readOnlyHint = %v, want %v", tl.Name, a.ReadOnlyHint, row.readOnly)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint != row.openWorld {
			t.Errorf("%s: fallback openWorldHint = %v, want %v", tl.Name, a.OpenWorldHint, row.openWorld)
		}
		if row.destructive != nil && (a.DestructiveHint == nil || *a.DestructiveHint != *row.destructive) {
			t.Errorf("%s: fallback destructiveHint = %v, want %v", tl.Name, a.DestructiveHint, *row.destructive)
		}
		if row.idempotent != nil && a.IdempotentHint != *row.idempotent {
			t.Errorf("%s: fallback idempotentHint = %v, want %v", tl.Name, a.IdempotentHint, *row.idempotent)
		}
	}
}

// TestSanitiserPreservesAnnotations proves SanitizeToolSchema leaves the
// annotations object untouched (it reads/writes only type and properties).
func TestSanitiserPreservesAnnotations(t *testing.T) {
	tool := &mcp.Tool{
		Name:        "x",
		InputSchema: map[string]any{"type": "object"},
		Annotations: &mcp.ToolAnnotations{
			ReadOnlyHint:  true,
			OpenWorldHint: boolPtr(false),
		},
	}
	before := *tool.Annotations
	out := util.SanitizeToolSchema(tool)
	if out.Annotations == nil {
		t.Fatal("sanitiser stripped annotations")
	}
	if out.Annotations.ReadOnlyHint != before.ReadOnlyHint ||
		out.Annotations.OpenWorldHint == nil || *out.Annotations.OpenWorldHint != *before.OpenWorldHint {
		t.Fatalf("sanitiser altered annotations: %+v", out.Annotations)
	}
}
