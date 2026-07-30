package handler

import (
	"testing"
)

func TestExtractKeysExtra(t *testing.T) {
	// nil schema
	keys := extractKeys("", nil)
	if len(keys) != 0 {
		t.Error("expected 0 keys for nil schema")
	}

	// flat schema
	schema := map[string]any{
		"a": map[string]any{"type": "string"},
		"b": map[string]any{"type": "integer"},
	}
	keys = extractKeys("", schema)
	if len(keys) != 2 || keys["a"] != "string" || keys["b"] != "integer" {
		t.Error("failed flat schema extraction")
	}

	// nested schema
	schema2 := map[string]any{
		"a": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"c": map[string]any{"type": "string"},
			},
		},
	}
	keys = extractKeys("pre.", schema2)
	if len(keys) != 2 || keys["pre.a"] != "object" || keys["pre.a.c"] != "string" {
		t.Error("failed nested schema extraction")
	}
}

func TestComputeJaccardExtra(t *testing.T) {
	if computeJaccard(map[string]string{}, map[string]string{}) != 1.0 {
		t.Error("expected 1.0 for empty maps")
	}

	if computeJaccard(map[string]string{"a": "1"}, map[string]string{}) != 0 {
		t.Error("expected 0 for disjoint maps")
	}

	mapA := map[string]string{"a": "string", "b": "integer"}
	mapB := map[string]string{"a": "string", "b": "integer", "c": "boolean"}

	score := computeJaccard(mapA, mapB)
	// intersection = 2 (a, b)
	// union = 2 + 3 - 2 = 3
	// jaccard = 2/3 = 0.666...
	if score < 0.66 || score > 0.67 {
		t.Error("incorrect jaccard score")
	}
}
