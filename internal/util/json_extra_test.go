package util

import (
	"fmt"
	"testing"
)

func TestSqueezeAndTruncate(t *testing.T) {
	val := map[string]any{
		"a": "hello world",
		"b": nil,
		"c": []any{
			"very long string that should be truncated",
			nil,
		},
		"id":      nil, // safeKey -> will become 0
		"jsonrpc": nil, // safeKey -> will become "2.0"
	}

	res := SqueezeAndTruncate(val, 10)

	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", res)
	}

	if got, want := len(fmt.Sprint(m["a"])), len("hello world"); got > want {
		t.Errorf("truncated field a longer than source: got %d want <= %d", got, want)
	}

	if _, ok := m["b"]; ok {
		t.Errorf("expected b to be removed")
	}

	if m["id"] != 0 {
		t.Errorf("expected id to be 0, got %v", m["id"])
	}

	if m["jsonrpc"] != "2.0" {
		t.Errorf("expected jsonrpc to be '2.0', got %v", m["jsonrpc"])
	}

	c, ok := m["c"].([]any)
	if !ok {
		t.Fatalf("expected c to be slice, got %T", m["c"])
	}

	if len(c) != 1 {
		t.Errorf("expected c length 1, got %d", len(c))
	}
}

func TestSqueezeAndTruncateDeep(t *testing.T) {
	// Test depth > 10
	var val any = "leaf"
	for range 15 {
		val = map[string]any{"nested": val}
	}

	res := SqueezeAndTruncate(val, 10)
	if res == nil {
		t.Error("expected non-nil response for deep structure")
	}
}

func TestSqueezeRecursivePointer(t *testing.T) {
	str := "test"
	var p = &str

	res := SqueezeResult(p)
	if res != "test" {
		t.Errorf("expected 'test', got %v", res)
	}

	var np *string = nil
	res2 := SqueezeResult(np)
	if res2 != nil {
		t.Errorf("expected nil, got %v", res2)
	}

	// Also test pointer in SqueezeAndTruncate
	res3 := SqueezeAndTruncate(p, 10)
	if res3 != "test" {
		t.Errorf("expected 'test', got %v", res3)
	}

	res4 := SqueezeAndTruncate(np, 10)
	if res4 != nil {
		t.Errorf("expected nil, got %v", res4)
	}
}
