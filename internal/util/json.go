package util

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
)

// SqueezeResult recursively removes nulls and empty arrays from a JSON-RPC response map.
func SqueezeResult(val any) any {
	return squeezeRecursive(val, 0)
}

// 🛡️ BASTION SAFETY: 'Safe Keys' that must NEVER be stripped to maintain MCP 2.0 compliance.
// Maps key names to their protocol-safe default values should the source be null.
var safeKeys = map[string]any{
	"jsonrpc":     "2.0",
	"inputSchema": map[string]any{schemaKeyType: schemaTypeObject, schemaKeyProperties: map[string]any{}},
	"name":        "unknown-tool",
	schemaKeyType: schemaTypeObject,
	"method":      "unknown/method",
	"id":          0,
}

func squeezeRecursive(val any, depth int) any {
	if val == nil || depth > 10 {
		return val
	}

	switch v := val.(type) {
	case map[string]any:
		if len(v) == 0 {
			return v
		}
		m := make(map[string]any, len(v))
		for k, mv := range v {
			squeezed := squeezeRecursive(mv, depth+1)

			if squeezed != nil {
				m[k] = squeezed
			} else if defaultValue, ok := safeKeys[k]; ok {
				// 🛡️ RECOVERY: If a safe key is null, use the protocol-compliant default.
				slog.Error("orchestrator: detected null value for critical protocol key, injecting default", "key", k)
				m[k] = defaultValue
			}
		}
		return m
	case []any:
		if len(v) == 0 {
			return nil
		}
		res := make([]any, 0, len(v))
		for _, item := range v {
			s := squeezeRecursive(item, depth+1)
			if s != nil {
				res = append(res, s)
			}
		}
		return res
	default:

		// 🛡️ Reflection Fallback: Only for non-standard types to minimize CPU/GC overhead
		rv := reflect.ValueOf(val)
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			return squeezeRecursive(rv.Elem().Interface(), depth+1)
		}
		return val
	}
}

// MarshalMinified minifies a response using standard library behavior.
func MarshalMinified(val any) ([]byte, error) {
	data, err := json.Marshal(val)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal minced response: %w", err)
	}
	return data, nil
}

// SqueezeAndTruncate performs both null-removal (squeeze) and large-string
// truncation in a single recursive pass, eliminating two separate traversals.
// 🛡️ BASTION PERFORMANCE GUARD: Depth-limited (10 levels).
func SqueezeAndTruncate(val any, truncateLimit int) any {
	return squeezeAndTruncateRecursive(val, truncateLimit, 0)
}

func squeezeAndTruncateRecursive(val any, limit int, depth int) any {
	if val == nil || depth > 10 {
		return val
	}

	switch v := val.(type) {
	case string:
		if len(v) > limit {
			return CenterTruncate(v, limit)
		}
		return v
	case map[string]any:
		if len(v) == 0 {
			return v
		}
		m := make(map[string]any, len(v))
		for k, mv := range v {
			squeezed := squeezeAndTruncateRecursive(mv, limit, depth+1)
			if squeezed != nil {
				m[k] = squeezed
			} else if defaultValue, ok := safeKeys[k]; ok {
				slog.Error("orchestrator: detected null value for critical protocol key, injecting default", "key", k)
				m[k] = defaultValue
			}
		}
		return m
	case []any:
		if len(v) == 0 {
			return nil
		}
		res := make([]any, 0, len(v))
		for _, item := range v {
			s := squeezeAndTruncateRecursive(item, limit, depth+1)
			if s != nil {
				res = append(res, s)
			}
		}
		return res
	default:
		rv := reflect.ValueOf(val)
		if rv.Kind() == reflect.Pointer {
			if rv.IsNil() {
				return nil
			}
			return squeezeAndTruncateRecursive(rv.Elem().Interface(), limit, depth+1)
		}
		return val
	}
}
