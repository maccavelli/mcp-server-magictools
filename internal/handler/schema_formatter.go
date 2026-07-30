package handler

import (
	"sort"
	"strings"
)

// FormatCompactSchema generates a compact, agent-readable parameter table
// from a tool's inputSchema. Output example:
//
//	Parameters:
//	  - query: string (Required) - Search query for tool name or description.
//	  - server_name: string (Optional) - Filter results to a specific server.
//	  - severity: string (Optional) - One of: ERROR, WARNING, CRITICAL
func FormatCompactSchema(schema map[string]any) string {
	props, ok := schema[keyProperties].(map[string]any)
	if !ok || len(props) == 0 {
		return ""
	}

	// Build required set
	requiredSet := make(map[string]bool)
	if req, ok := schema[keyRequired].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}

	// Sort keys alphabetically for determinism
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("Parameters:\n")

	for _, key := range keys {
		propDef, ok := props[key].(map[string]any)
		if !ok {
			continue
		}
		sb.WriteString(formatSchemaPropertyLine(key, propDef, requiredSet))
	}

	return sb.String()
}

// resolveTypeName extracts a human-readable type string from a JSON schema property.
func resolveTypeName(propDef map[string]any) string {
	if t, ok := propDef[keyType].(string); ok {
		if t == schemaTypeArray {
			if items, ok := propDef[keyItems].(map[string]any); ok {
				if itemType, ok := items[keyType].(string); ok {
					return "array[" + itemType + "]"
				}
			}
			return schemaTypeArray
		}
		return t
	}
	if t, ok := propDef[keyType].([]any); ok {
		var types []string
		for _, v := range t {
			if s, ok := v.(string); ok {
				types = append(types, s)
			}
		}
		return strings.Join(types, "|")
	}
	return "any"
}

// FormatRequiredMissingHints generates enriched metadata for each required
// field that the agent must fill, including type, description, and enum.
// Used by buildCallTemplate to enhance the required_missing array.
//
// Output: [{"name": "query", "type": "string", "description": "Search query...", "enum": null}]
func FormatRequiredMissingHints(schema map[string]any, missingFields []string) []map[string]any {
	props := mapFrom(schema[keyProperties])
	if props == nil {
		// Fallback: return bare field names as hints
		hints := make([]map[string]any, 0, len(missingFields))
		for _, name := range missingFields {
			hints = append(hints, map[string]any{"name": name})
		}
		return hints
	}

	hints := make([]map[string]any, 0, len(missingFields))
	for _, name := range missingFields {
		hint := map[string]any{"name": name}

		propDef, ok := props[name].(map[string]any)
		if ok {
			hint[keyType] = resolveTypeName(propDef)

			if desc, ok := propDef["description"].(string); ok {
				if len(desc) > 300 {
					desc = desc[:297] + "..."
				}
				hint["description_hint"] = desc
			}

			if enum, ok := propDef["enum"].([]any); ok && len(enum) > 0 {
				hint["enum"] = enum
			}
		}

		hints = append(hints, hint)
	}

	return hints
}
