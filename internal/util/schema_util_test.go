package util

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestValidateZeroNull_AllowsNullable(t *testing.T) {
	tool := &mcp.Tool{
		Name: "test_tool",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"opt": map[string]any{"type": []any{"string", "null"}},
				"def": map[string]any{"type": "string", "default": nil},
			},
		},
	}
	err := ValidateZeroNull(tool)
	if err != nil {
		t.Errorf("ValidateZeroNull failed on valid nullable JSON schema: %v", err)
	}
}

func TestSanitizeToolSchema_SetsDefaultType(t *testing.T) {
	tool := &mcp.Tool{
		Name: "test_tool",
		InputSchema: map[string]any{
			"properties": map[string]any{
				"foo": map[string]any{"description": "bar"},
			},
		},
	}
	sanitized := SanitizeToolSchema(tool)

	// Check root type
	schema := sanitized.InputSchema.(map[string]any)
	if schema["type"] != "object" {
		t.Errorf("Expected root type 'object', got %v", schema["type"])
	}

	// Check property type
	props := schema["properties"].(map[string]any)
	foo := props["foo"].(map[string]any)
	if foo["type"] != "object" {
		t.Errorf("Expected property 'foo' to default to type 'object', got %v", foo["type"])
	}
}

func TestMinifyDescription(t *testing.T) {
	longDesc := "This is a very long description that goes on and on " + string(make([]byte, 500))
	minified := MinifyDescription(longDesc)
	if len(minified) > 300 {
		t.Error("description not minified")
	}
}
func TestSanitizeToolSchema_Raw(t *testing.T) {
	tool := &mcp.Tool{
		Name:        "test",
		InputSchema: json.RawMessage(`{"properties": {"a": null}}`),
	}
	SanitizeToolSchema(tool)

	m, ok := tool.InputSchema.(map[string]any)
	if !ok {
		t.Fatal("expected InputSchema to be converted to map")
	}
	if m["type"] != "object" {
		t.Error("expected default type object")
	}
}

func TestSanitizeToolSchema_Nil(t *testing.T) {
	if SanitizeToolSchema(nil) != nil {
		t.Error("expected nil for nil tool")
	}

	tool := &mcp.Tool{Name: "test", InputSchema: nil}
	SanitizeToolSchema(tool)
	if tool.InputSchema == nil {
		t.Error("expected InputSchema to be initialized")
	}
}

func TestValidateZeroNull_Errors(t *testing.T) {
	if err := ValidateZeroNull(nil); err == nil {
		t.Error("expected error for nil tool")
	}
	if err := ValidateZeroNull(&mcp.Tool{Name: ""}); err == nil {
		t.Error("expected error for empty name")
	}
	if err := ValidateZeroNull(&mcp.Tool{Name: "t", InputSchema: nil}); err == nil {
		t.Error("expected error for nil schema")
	}

	tool := &mcp.Tool{
		Name:        "t",
		InputSchema: json.RawMessage(`invalid`),
	}
	if err := ValidateZeroNull(tool); err == nil {
		t.Error("expected error for invalid json")
	}
}
