package handler

import (
	"encoding/json"
	"testing"
)

// d3Titles mirrors the MADR's D3 title table exactly
// (0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md).
// The MADR is the source; a wording refinement updates both in one commit.
func d3Titles() map[string]string {
	return map[string]string{
		"sync_servers":              "Refresh server tool index",
		"wake_servers":              "Start sleeping servers",
		"reload_servers":            "Restart servers",
		"get_internal_logs":         "Read gateway logs",
		"get_session_stats":         "Show session metrics",
		"get_health_report":         "Show server health",
		"analyze_system_logs":       "Filter gateway logs by severity",
		"align_tools":               "Find the right tool",
		"call_proxy":                "Run a downstream tool",
		"update_config":             "Change a gateway setting",
		"self_check":                "Run gateway self-diagnostics",
		"list_tools":                "List a server's tools",
		"semantic_similarity":       "Find overlapping tools",
		"query_compliance":          "Search coding standards",
		"execute_pipeline":          "Run an analysis pipeline",
		"validate_pipeline_step":    "Check a pipeline step's output",
		"cross_server_quality_gate": "Check pipeline readiness",
		"generate_audit_report":     "Build a pipeline audit report",
	}
}

// TestInventoryTitles pins every own tool's title in the inventory against
// the MADR's D3 table: present, non-empty, distinct from the programmatic
// name, and exactly the table's wording.
func TestInventoryTitles(t *testing.T) {
	var tools []InternalTool
	if err := json.Unmarshal(InternalToolsInventoryJSON, &tools); err != nil {
		t.Fatalf("unmarshal inventory: %v", err)
	}
	want := d3Titles()
	if len(tools) != len(want) {
		t.Fatalf("inventory has %d tools, D3 table has %d", len(tools), len(want))
	}
	for _, tl := range tools {
		title, ok := want[tl.Name]
		if !ok {
			t.Errorf("%s: not in the D3 table", tl.Name)
			continue
		}
		if tl.Title == "" {
			t.Errorf("%s: no title", tl.Name)
			continue
		}
		if tl.Title == tl.Name {
			t.Errorf("%s: title equals the programmatic name", tl.Name)
		}
		if tl.Title != title {
			t.Errorf("%s: title = %q, want %q", tl.Name, tl.Title, title)
		}
	}
}
