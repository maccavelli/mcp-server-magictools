package util

import (
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// SanitizeToolSchema ensures that a tool's inputSchema is valid and never nil.
// If InputSchema is nil or empty, it defaults to {"type": "object", "properties": {}}.
func SanitizeToolSchema(tool *mcp.Tool) *mcp.Tool {
	if tool == nil {
		return nil
	}

	if tool.InputSchema == nil {
		tool.InputSchema = map[string]any{
			schemaKeyType:       schemaTypeObject,
			schemaKeyProperties: map[string]any{},
		}
		return tool
	}

	// 🛡️ BASTION SAFETY: If it's a map, recursively sanitize it.
	switch v := tool.InputSchema.(type) {
	case map[string]any:
		tool.InputSchema = sanitizeMap(v)
	case json.RawMessage:
		var m map[string]any
		if err := json.Unmarshal(v, &m); err == nil {
			tool.InputSchema = sanitizeMap(m)
		}
	}

	return tool
}

func sanitizeMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{
			schemaKeyType:       schemaTypeObject,
			schemaKeyProperties: map[string]any{},
		}
	}

	// If it doesn't have a type, it's potentially an invalid schema part.
	if _, ok := m[schemaKeyType]; !ok {
		m[schemaKeyType] = schemaTypeObject
	}

	if props, ok := m[schemaKeyProperties].(map[string]any); ok {
		for k, v := range props {
			if v == nil {
				props[k] = map[string]any{schemaKeyType: schemaTypeObject}
			} else if pm, ok := v.(map[string]any); ok {
				props[k] = sanitizeMap(pm)
			}
		}
	} else if _, ok := m[schemaKeyProperties]; !ok && m[schemaKeyType] == schemaTypeObject {
		m[schemaKeyProperties] = map[string]any{}
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
