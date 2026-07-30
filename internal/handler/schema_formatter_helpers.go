package handler

import (
	"fmt"
	"strings"
)

func formatSchemaPropertyLine(key string, propDef map[string]any, requiredSet map[string]bool) string {
	typeName := resolveTypeName(propDef)

	reqLabel := "Optional"
	if requiredSet[key] {
		reqLabel = "Required"
	}

	desc := ""
	if d, ok := propDef["description"].(string); ok {
		desc = d
		if len(desc) > 300 {
			desc = desc[:297] + "..."
		}
	}

	enumStr := ""
	if enum, ok := propDef["enum"].([]any); ok && len(enum) > 0 {
		var vals []string
		for _, e := range enum {
			if s, ok := e.(string); ok {
				vals = append(vals, s)
			} else {
				vals = append(vals, fmt.Sprintf("%v", e))
			}
		}
		enumStr = fmt.Sprintf(" (One of: %s)", strings.Join(vals, ", "))
	}

	nestedReqStr := ""
	if typeName == schemaTypeObject {
		if nestedProps, ok := propDef[keyProperties].(map[string]any); ok {
			if nestedReq, ok := propDef[keyRequired].([]any); ok && len(nestedReq) > 0 {
				var nestedKeys []string
				for _, r := range nestedReq {
					if s, ok := r.(string); ok {
						nestedKeys = append(nestedKeys, s)
					}
				}
				if len(nestedKeys) > 0 {
					nestedReqStr = fmt.Sprintf(" (%d required keys: %s)", len(nestedKeys), strings.Join(nestedKeys, ", "))
				}
			} else {
				nestedReqStr = fmt.Sprintf(" (%d keys)", len(nestedProps))
			}
		}
	}

	line := fmt.Sprintf("  - %s: %s (%s)", key, typeName, reqLabel)
	if enumStr != "" {
		line += enumStr
	}
	if nestedReqStr != "" {
		line += nestedReqStr
	}
	if desc != "" {
		line += " - " + desc
	}
	return line + "\n"
}
