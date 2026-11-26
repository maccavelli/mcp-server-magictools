package handler

import (
	"encoding/json"
	"testing"
)

// schemaDraft202012 is the URI every own inputSchema must declare (MADR 0015
// D4: 0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md).
const schemaDraft202012 = "https://json-schema.org/draft/2020-12/schema"

// TestInventorySchemaDraft pins the explicit $schema declaration on every own
// tool's inputSchema in the inventory: the exact 2020-12 URI, no trailing
// slash, no fragment.
func TestInventorySchemaDraft(t *testing.T) {
	var tools []struct {
		Name        string         `json:"name"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	if err := json.Unmarshal(InternalToolsInventoryJSON, &tools); err != nil {
		t.Fatalf("unmarshal inventory: %v", err)
	}
	if len(tools) != 18 {
		t.Fatalf("inventory has %d tools, want 18", len(tools))
	}
	for _, tl := range tools {
		got, ok := tl.InputSchema["$schema"]
		if !ok {
			t.Errorf("%s: inputSchema has no $schema", tl.Name)
			continue
		}
		if got != schemaDraft202012 {
			t.Errorf("%s: $schema = %v, want %q", tl.Name, got, schemaDraft202012)
		}
	}
}
