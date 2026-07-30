package handler

func mapFrom(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func stringFrom(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
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
