package handler

import (
	"encoding/json"
	"testing"
)

// d1AnnotationRow is one tool's expected annotation set, from the MADR's D1
// table (0015-MADR-carry-the-mcp-standards-title-annotations-and-constraint-keywords-on-every-own-tool.md).
// A nil pointer means the hint must be absent (meaningless for a read-only tool).
type d1AnnotationRow struct {
	readOnly    bool
	destructive *bool
	idempotent  *bool
	openWorld   bool
}

func boolPtr(b bool) *bool { return &b }

// d1Annotations mirrors the MADR's D1 table exactly. The MADR is the source;
// a handler behaviour change that falsifies a row updates both in one commit.
func d1Annotations() map[string]d1AnnotationRow {
	return map[string]d1AnnotationRow{
		"sync_servers":              {false, boolPtr(false), boolPtr(true), false},
		"wake_servers":              {false, boolPtr(false), boolPtr(true), false},
		"reload_servers":            {false, boolPtr(true), boolPtr(true), false},
		"get_internal_logs":         {true, nil, nil, false},
		"get_session_stats":         {true, nil, nil, false},
		"get_health_report":         {false, boolPtr(false), boolPtr(true), false},
		"analyze_system_logs":       {true, nil, nil, false},
		"align_tools":               {false, boolPtr(true), boolPtr(false), true},
		"call_proxy":                {false, boolPtr(true), boolPtr(false), true},
		"update_config":             {false, boolPtr(false), boolPtr(true), false},
		"self_check":                {true, nil, nil, false},
		"list_tools":                {true, nil, nil, false},
		"semantic_similarity":       {false, boolPtr(false), boolPtr(true), false},
		"query_compliance":          {true, nil, nil, false},
		"execute_pipeline":          {false, boolPtr(true), boolPtr(false), true},
		"validate_pipeline_step":    {true, nil, nil, false},
		"cross_server_quality_gate": {true, nil, nil, false},
		"generate_audit_report":     {false, boolPtr(false), boolPtr(true), false},
	}
}

// TestInventoryAnnotations pins every own tool's annotations object in the
// inventory against the MADR's D1 table: the declared values exactly, and
// absence where the table says the hint is meaningless.
func TestInventoryAnnotations(t *testing.T) {
	var tools []InternalTool
	if err := json.Unmarshal(InternalToolsInventoryJSON, &tools); err != nil {
		t.Fatalf("unmarshal inventory: %v", err)
	}
	want := d1Annotations()
	if len(tools) != len(want) {
		t.Fatalf("inventory has %d tools, D1 table has %d", len(tools), len(want))
	}
	for _, tl := range tools {
		row, ok := want[tl.Name]
		if !ok {
			t.Errorf("%s: not in the D1 table", tl.Name)
			continue
		}
		if tl.Annotations == nil {
			t.Errorf("%s: no annotations object", tl.Name)
			continue
		}
		a := tl.Annotations
		if a.ReadOnlyHint != row.readOnly {
			t.Errorf("%s: readOnlyHint = %v, want %v", tl.Name, a.ReadOnlyHint, row.readOnly)
		}
		if a.OpenWorldHint == nil || *a.OpenWorldHint != row.openWorld {
			t.Errorf("%s: openWorldHint = %v, want %v", tl.Name, a.OpenWorldHint, row.openWorld)
		}
		if row.destructive == nil {
			if a.DestructiveHint != nil {
				t.Errorf("%s: destructiveHint present (%v), must be absent for a read-only tool", tl.Name, *a.DestructiveHint)
			}
		} else if a.DestructiveHint == nil || *a.DestructiveHint != *row.destructive {
			t.Errorf("%s: destructiveHint = %v, want %v", tl.Name, a.DestructiveHint, *row.destructive)
		}
		// IdempotentHint is a plain bool in the SDK type; the inventory JSON
		// carries it only where the table declares it. Absence unmarshals to
		// false, so for the false rows absence and false are indistinguishable
		// in the struct — the raw-JSON check below pins presence separately.
		if row.idempotent != nil && a.IdempotentHint != *row.idempotent {
			t.Errorf("%s: idempotentHint = %v, want %v", tl.Name, a.IdempotentHint, *row.idempotent)
		}
	}

	// Raw-JSON presence check: read-only tools must not carry destructiveHint
	// or idempotentHint keys at all; writers must carry both.
	var raw []struct {
		Name        string         `json:"name"`
		Annotations map[string]any `json:"annotations"`
	}
	if err := json.Unmarshal(InternalToolsInventoryJSON, &raw); err != nil {
		t.Fatal(err)
	}
	for _, tl := range raw {
		row := want[tl.Name]
		_, hasDestructive := tl.Annotations["destructiveHint"]
		_, hasIdempotent := tl.Annotations["idempotentHint"]
		if row.destructive == nil && hasDestructive {
			t.Errorf("%s: read-only tool carries destructiveHint key", tl.Name)
		}
		if row.destructive != nil && !hasDestructive {
			t.Errorf("%s: writer tool missing destructiveHint key", tl.Name)
		}
		if row.idempotent == nil && hasIdempotent {
			t.Errorf("%s: read-only tool carries idempotentHint key", tl.Name)
		}
		if row.idempotent != nil && !hasIdempotent {
			t.Errorf("%s: writer tool missing idempotentHint key", tl.Name)
		}
	}
}
