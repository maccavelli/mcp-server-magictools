package main

func mapFrom(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func sliceFrom(v any) []any {
	s, ok := v.([]any)
	if !ok {
		return nil
	}
	return s
}

func boolFrom(v any) bool {
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

func stringFrom(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func int64From(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}
