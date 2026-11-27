package util

import (
	"encoding/json"
	"reflect"
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

	// Decided 6 / D3.1: an untyped property stays untyped. The old pin
	// required type: object here. That was the retyping this record removes.
	props := schema["properties"].(map[string]any)
	foo := props["foo"].(map[string]any)
	if _, ok := foo["type"]; ok {
		t.Errorf("untyped property gained type %v; want it left unchanged", foo["type"])
	}
	if _, ok := foo["properties"]; ok {
		t.Error("untyped property gained properties; want it left unchanged")
	}
	if foo["description"] != "bar" {
		t.Errorf("description = %v, want bar", foo["description"])
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
	props, _ := m["properties"].(map[string]any)
	a, _ := props["a"].(map[string]any)
	if len(a) != 0 {
		t.Errorf("null property = %#v, want {}", props["a"])
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

// TestSanitizeToolSchema_D31Table holds 0005's D3.1 table. D3.2's probe was not
// run, so a nested object with no properties keeps properties: {}.
func TestSanitizeToolSchema_D31Table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   map[string]any
		want map[string]any
	}{
		{
			name: "root without type gains type object",
			in:   map[string]any{"properties": map[string]any{}},
			want: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			name: "root without properties gains empty properties",
			in:   map[string]any{"type": "object"},
			want: map[string]any{"type": "object", "properties": map[string]any{}},
		},
		{
			name: "untyped property stays untyped",
			in: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"foo": map[string]any{"description": "bar"},
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"foo": map[string]any{"description": "bar"},
				},
			},
		},
		{
			name: "null property becomes empty object",
			in: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": nil,
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"a": map[string]any{},
				},
			},
		},
		{
			name: "object property without properties keeps properties empty",
			in: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bag": map[string]any{"type": "object"},
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bag": map[string]any{"type": "object", "properties": map[string]any{}},
				},
			},
		},
		{
			name: "object property with properties is recursed",
			in: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bag": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"inner": map[string]any{"description": "only"},
						},
					},
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"bag": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"inner": map[string]any{"description": "only"},
						},
					},
				},
			},
		},
		{
			name: "items empty object is unchanged",
			in: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"list": map[string]any{"type": "array", "items": map[string]any{}},
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"list": map[string]any{"type": "array", "items": map[string]any{}},
				},
			},
		},
		{
			name: "downstream empty object is unchanged",
			in: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"empty": map[string]any{},
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"empty": map[string]any{},
				},
			},
		},
		{
			name: "root union is unchanged",
			in: map[string]any{
				"anyOf": []any{map[string]any{"type": "object"}, map[string]any{"type": "string"}},
			},
			want: map[string]any{
				"anyOf": []any{map[string]any{"type": "object"}, map[string]any{"type": "string"}},
			},
		},
		{
			name: "nested union is unchanged",
			in: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"choice": map[string]any{
						"oneOf": []any{map[string]any{"type": "string"}},
					},
				},
			},
			want: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"choice": map[string]any{
						"oneOf": []any{map[string]any{"type": "string"}},
					},
				},
			},
		},
		{
			name: "unknown keywords are unchanged",
			in: map[string]any{
				"type":                 "object",
				"$ref":                 "#/$defs/Root",
				"additionalProperties": false,
				"properties": map[string]any{
					"mode": map[string]any{
						"type":    "string",
						"enum":    []any{"a", "b"},
						"default": "a",
					},
				},
				"patternProperties": map[string]any{
					"^x": map[string]any{},
				},
				"$defs": map[string]any{
					"Root": map[string]any{"type": "object"},
				},
			},
			want: map[string]any{
				"type":                 "object",
				"$ref":                 "#/$defs/Root",
				"additionalProperties": false,
				"properties": map[string]any{
					"mode": map[string]any{
						"type":    "string",
						"enum":    []any{"a", "b"},
						"default": "a",
					},
				},
				"patternProperties": map[string]any{
					"^x": map[string]any{},
				},
				"$defs": map[string]any{
					"Root": map[string]any{"type": "object"},
				},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tool := &mcp.Tool{Name: "t", InputSchema: tc.in}
			SanitizeToolSchema(tool)
			got, _ := tool.InputSchema.(map[string]any)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %#v\nwant %#v", got, tc.want)
			}
		})
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
