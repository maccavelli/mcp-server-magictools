package util

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SanitizeToolSchema ensures that a tool's inputSchema is valid and never nil.
// A nil schema becomes {"type": "object", "properties": {}}.
//
// The walk is 0005's D3.1 table (docs/decisions/0005-MADR-make-execute-pipeline-schema-and-handler-agree.md).
// A missing root type becomes object, because MCP requires an object root. A missing root
// properties map becomes {}. An untyped property is left untyped: adding type: object was the
// retyping Decided 6 removed, and it is what made a description-only node look like an object.
// A null property becomes {}, the one empty this function may invent, and only from an invalid
// input. An object-typed property with no properties keeps properties: {}; D3.2's probe was not
// run, so the default is to keep them. A union (anyOf, oneOf, allOf) at the root or nested is
// not written, byte for byte (D3.4). items: {}, a downstream {}, and keywords this function does
// not know ($ref, $defs, enum, default, additionalProperties, patternProperties) are not written.
func SanitizeToolSchema(tool *mcp.Tool) *mcp.Tool {
	if tool == nil {
		return nil
	}

	if tool.InputSchema == nil {
		tool.InputSchema = defaultObjectSchema()
		return tool
	}

	switch v := tool.InputSchema.(type) {
	case map[string]any:
		tool.InputSchema = sanitizeRoot(v)
	case json.RawMessage:
		var m map[string]any
		if err := json.Unmarshal(v, &m); err == nil {
			tool.InputSchema = sanitizeRoot(m)
		}
	}

	return tool
}

func defaultObjectSchema() map[string]any {
	return map[string]any{
		schemaKeyType:       schemaTypeObject,
		schemaKeyProperties: map[string]any{},
	}
}

func isUnionSchema(m map[string]any) bool {
	_, anyOf := m["anyOf"]
	_, oneOf := m["oneOf"]
	_, allOf := m["allOf"]
	return anyOf || oneOf || allOf
}

// sanitizeRoot applies the root rows. A union root is returned as the same map, with no writes.
func sanitizeRoot(m map[string]any) map[string]any {
	if m == nil {
		return defaultObjectSchema()
	}
	if isUnionSchema(m) {
		return m
	}
	if _, ok := m[schemaKeyType]; !ok {
		m[schemaKeyType] = schemaTypeObject
	}
	props, hasProps := m[schemaKeyProperties]
	if !hasProps {
		m[schemaKeyProperties] = map[string]any{}
		return m
	}
	if pm, ok := props.(map[string]any); ok {
		sanitizeProperties(pm)
	}
	return m
}

func sanitizeProperties(props map[string]any) {
	for k, v := range props {
		props[k] = sanitizeProperty(v)
	}
}

// sanitizeProperty applies the property rows. It does not add type to an untyped node, and it
// does not walk items, additionalProperties, patternProperties, or $defs.
func sanitizeProperty(v any) any {
	if v == nil {
		return map[string]any{}
	}
	m, ok := v.(map[string]any)
	if !ok || len(m) == 0 || isUnionSchema(m) {
		return v
	}
	typ, hasType := m[schemaKeyType]
	if !hasType || typ != schemaTypeObject {
		return m
	}
	props, hasProps := m[schemaKeyProperties]
	if !hasProps {
		m[schemaKeyProperties] = map[string]any{}
		return m
	}
	if pm, ok := props.(map[string]any); ok {
		sanitizeProperties(pm)
	}
	return m
}

// ValidateZeroNull checks a tool for null values in its InputSchema or critical fields.
func ValidateZeroNull(tool *mcp.Tool) error {
	if tool == nil {
		return fmt.Errorf("tool is nil")
	}
	if tool.Name == "" {
		return fmt.Errorf("tool name is empty")
	}
	if tool.InputSchema == nil {
		return fmt.Errorf("tool '%s' has nil inputSchema", tool.Name)
	}

	// 🛡️ BASTION SAFETY: Recursive null check
	switch v := tool.InputSchema.(type) {
	case map[string]any:
		return checkMapForNulls(v)
	case json.RawMessage:
		var m map[string]any
		if err := json.Unmarshal(v, &m); err != nil {
			return fmt.Errorf("invalid json in tool '%s' inputSchema: %w", tool.Name, err)
		}
		return checkMapForNulls(m)
	}

	return nil
}

func checkMapForNulls(m map[string]any) error {
	if m == nil {
		return nil
	}
	for _, v := range m {
		// 🛡️ BASTION SAFETY: We've relaxed this to allow nulls in JSON schemas.
		// Standard JSON Schema 2020-12 (which MCP follows) allows literal nulls
		// for 'default' values or nullable types.
		if v == nil {
			continue
		}
		if sm, ok := v.(map[string]any); ok {
			if err := checkMapForNulls(sm); err != nil {
				return err
			}
		}
	}
	return nil
}

// MinifyDescription truncates a description to 300 characters with ellipsis.
func MinifyDescription(desc string) string {
	if len(desc) <= 300 {
		return desc
	}
	return desc[:297] + "..."
}
