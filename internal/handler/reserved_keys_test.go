package handler

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// TestReservedEnvelopeKeysContainsMeta guards the single source of truth: _meta must be
// recognized as reserved envelope metadata, never a forwardable tool argument.
func TestReservedEnvelopeKeysContainsMeta(t *testing.T) {
	if !reservedEnvelopeKeys["_meta"] {
		t.Fatalf("reservedEnvelopeKeys must contain \"_meta\"")
	}
}

// TestStripReservedKeys covers the dispatch-funnel authority: _meta is removed from the
// argument payload and any legacy embedded proxy_correlation_id is recovered.
func TestStripReservedKeys(t *testing.T) {
	t.Run("strips _meta and recovers correlation id", func(t *testing.T) {
		args := map[string]any{
			"path":  "/tmp",
			"_meta": map[string]any{"proxy_correlation_id": "corr-123"},
		}
		corrID := stripReservedKeys(args)
		if corrID != "corr-123" {
			t.Errorf("expected corrID %q, got %q", "corr-123", corrID)
		}
		if _, ok := args["_meta"]; ok {
			t.Errorf("_meta must be stripped from arguments, got: %v", args)
		}
		if args["path"] != "/tmp" {
			t.Errorf("non-reserved arguments must be preserved, got: %v", args)
		}
	})

	t.Run("strips _meta without a correlation id", func(t *testing.T) {
		args := map[string]any{"_meta": map[string]any{"unrelated": true}}
		if corrID := stripReservedKeys(args); corrID != "" {
			t.Errorf("expected empty corrID, got %q", corrID)
		}
		if _, ok := args["_meta"]; ok {
			t.Errorf("_meta must be stripped, got: %v", args)
		}
	})

	t.Run("no _meta present", func(t *testing.T) {
		args := map[string]any{"path": "/tmp"}
		if corrID := stripReservedKeys(args); corrID != "" {
			t.Errorf("expected empty corrID, got %q", corrID)
		}
		if len(args) != 1 || args["path"] != "/tmp" {
			t.Errorf("arguments must be untouched, got: %v", args)
		}
	})

	t.Run("nil map is safe", func(t *testing.T) {
		if corrID := stripReservedKeys(nil); corrID != "" {
			t.Errorf("expected empty corrID for nil map, got %q", corrID)
		}
	})
}

// TestStripExtraPropertiesStripsMeta covers the validation-path layer: _meta is removed
// quietly (as reserved metadata, not a hallucinated field) while valid schema-defined
// fields are preserved and genuine extras are still stripped.
func TestStripExtraPropertiesStripsMeta(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string"},
		},
	}
	args := map[string]any{
		"path":         "/tmp",
		"_meta":        map[string]any{"proxy_correlation_id": "corr-123"},
		"hallucinated": true,
	}

	stripExtraProperties(schema, args)

	if _, ok := args["_meta"]; ok {
		t.Errorf("_meta must be stripped by validation layer, got: %v", args)
	}
	if _, ok := args["hallucinated"]; ok {
		t.Errorf("genuine extra field must still be stripped, got: %v", args)
	}
	if args["path"] != "/tmp" {
		t.Errorf("schema-defined field must be preserved, got: %v", args)
	}
}

// TestBuildCallTemplateOmitsMeta guards the emit side (row 1): the align_tools call
// template must never pre-fill _meta into arguments, since strict sub-server schemas
// reject it and would force a retry.
func TestBuildCallTemplateOmitsMeta(t *testing.T) {
	record := &db.ToolRecord{
		URN: "test:tool",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"req"},
			"properties": map[string]any{
				"req": map[string]any{"type": "string"},
				"opt": map[string]any{"type": "string"},
			},
		},
		ZeroValues: map[string]any{"opt": ""},
	}

	tmpl := buildCallTemplate(record)
	if tmpl == nil {
		t.Fatal("buildCallTemplate returned nil")
	}
	argsRaw, ok := tmpl["arguments"].(map[string]any)
	if !ok {
		t.Fatalf("template arguments not a map: %T", tmpl["arguments"])
	}
	if _, ok := argsRaw["_meta"]; ok {
		t.Errorf("call_template arguments must not contain _meta, got: %v", argsRaw)
	}
}

// TestBuildCallTemplate_RequiredOnly is the ALIGN-4 regression: templates are
// required-only — arguments is always empty (the old optional pre-fill was dead
// code), and required fields are surfaced via required_missing.
func TestBuildCallTemplate_RequiredOnly(t *testing.T) {
	record := &db.ToolRecord{
		URN: "test:tool",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []any{"beta", "alpha"},
			"properties": map[string]any{
				"alpha": map[string]any{"type": "string"},
				"beta":  map[string]any{"type": "string"},
				"opt":   map[string]any{"type": "string"},
			},
		},
		// Even if ZeroValues contains optional keys, they must NOT be pre-filled.
		ZeroValues: map[string]any{"opt": "should-not-appear", "alpha": ""},
	}
	tmpl := buildCallTemplate(record)
	if tmpl == nil {
		t.Fatal("buildCallTemplate returned nil")
	}
	args := tmpl["arguments"].(map[string]any)
	if len(args) != 0 {
		t.Errorf("arguments must be empty (required-only), got %v", args)
	}
	if _, ok := tmpl["required_missing"]; !ok {
		t.Error("expected required_missing for required fields")
	}
}
