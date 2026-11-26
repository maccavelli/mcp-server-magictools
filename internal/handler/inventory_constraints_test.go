package handler

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
)

// constraintToolSchema returns one tool's inputSchema as a generic map.
func constraintToolSchema(t *testing.T, name string) map[string]any {
	t.Helper()
	var tools []struct {
		Name        string         `json:"name"`
		InputSchema map[string]any `json:"inputSchema"`
	}
	if err := json.Unmarshal(InternalToolsInventoryJSON, &tools); err != nil {
		t.Fatal(err)
	}
	for _, tl := range tools {
		if tl.Name == name {
			return tl.InputSchema
		}
	}
	t.Fatalf("tool %s not in inventory", name)
	return nil
}

func constraintProp(t *testing.T, schema map[string]any, prop string) map[string]any {
	t.Helper()
	props, _ := schema["properties"].(map[string]any)
	p, _ := props[prop].(map[string]any)
	if p == nil {
		t.Fatalf("property %s missing", prop)
	}
	return p
}

func constraintNum(t *testing.T, p map[string]any, key string) float64 {
	t.Helper()
	v, ok := p[key].(float64)
	if !ok {
		t.Fatalf("keyword %s missing or not a number (got %v)", key, p[key])
	}
	return v
}

// TestInventoryConstraints pins the D2 keywords to the measured handler values
// (0015-MADR D2; the plan's honesty table).
func TestInventoryConstraints(t *testing.T) {
	t.Run("analyze_system_logs.lines", func(t *testing.T) {
		p := constraintProp(t, constraintToolSchema(t, "analyze_system_logs"), "lines")
		if got := constraintNum(t, p, "minimum"); got != 1 {
			t.Errorf("minimum = %v, want 1", got)
		}
		if got := constraintNum(t, p, "maximum"); got != 1000 {
			t.Errorf("maximum = %v, want 1000", got)
		}
		if got := constraintNum(t, p, "default"); got != 50 {
			t.Errorf("default = %v, want 50", got)
		}
	})

	t.Run("list_tools.options.max_tools", func(t *testing.T) {
		opts := constraintProp(t, constraintToolSchema(t, "list_tools"), "options")
		if d, _ := opts["description"].(string); strings.TrimSpace(d) == "" {
			t.Error("options has no description")
		}
		mt := constraintProp(t, opts, "max_tools")
		if got := constraintNum(t, mt, "minimum"); got != 1 {
			t.Errorf("minimum = %v, want 1", got)
		}
		if got := constraintNum(t, mt, "default"); got != 1000 {
			t.Errorf("default = %v, want 1000", got)
		}
		if _, has := mt["maximum"]; has {
			t.Error("max_tools must carry no maximum: the handler has none")
		}
	})

	t.Run("get_internal_logs.max_lines", func(t *testing.T) {
		p := constraintProp(t, constraintToolSchema(t, "get_internal_logs"), "max_lines")
		if got := constraintNum(t, p, "minimum"); got != 0 {
			t.Errorf("minimum = %v, want 0", got)
		}
		if got := constraintNum(t, p, "maximum"); got != 1000 {
			t.Errorf("maximum = %v, want 1000", got)
		}
		if got := constraintNum(t, p, "default"); got != 25 {
			t.Errorf("default = %v, want 25 (mcplib's wire copy says 0; the inventory copy must say 25)", got)
		}
	})

	t.Run("update_config.key enum", func(t *testing.T) {
		p := constraintProp(t, constraintToolSchema(t, "update_config"), "key")
		rawEnum, _ := p["enum"].([]any)
		var got []string
		for _, e := range rawEnum {
			s, _ := e.(string)
			got = append(got, s)
		}
		want := []string{
			"logLevel", "mcpLogLevel", "squeezeLevel", "logFormat", "scoreThreshold",
			"confidenceGap", "vector_enabled", "validateProxyCalls", "pinnedServers",
			"trustServers", "squeezeBypass", "ringBufferTargets", "tokenSpendThresh", "lruLimit",
		}
		sort.Strings(got)
		sort.Strings(want)
		if len(got) != len(want) {
			t.Fatalf("enum has %d keys, want %d: %v", len(got), len(want), got)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("enum mismatch at %d: got %s, want %s (full: %v)", i, got[i], want[i], got)
			}
		}
	})

	t.Run("update_config.value per-key rules", func(t *testing.T) {
		p := constraintProp(t, constraintToolSchema(t, "update_config"), "value")
		d, _ := p["description"].(string)
		for _, want := range []string{"squeezeLevel", "0-5", "scoreThreshold", "confidenceGap", "0.0-1.0", "logFormat", "json", "text", "tokenSpendThresh", "lruLimit"} {
			if !strings.Contains(d, want) {
				t.Errorf("value description missing per-key rule %q; got: %s", want, d)
			}
		}
	})
}
