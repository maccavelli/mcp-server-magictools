package util

import (
	"testing"
)

func TestLRU(t *testing.T) {
	lru := NewLRUCache[string, string](2)
	lru.Add("k1", "v1")
	lru.Add("k2", "v2")

	if val, ok := lru.Get("k1"); !ok || val != "v1" {
		t.Errorf("expected v1, got %v", val)
	}

	lru.Add("k3", "v3") // evicts k2

	if _, ok := lru.Get("k2"); ok {
		t.Errorf("expected k2 to be evicted")
	}

	vals := lru.Values()
	if len(vals) != 2 {
		t.Errorf("expected 2 values, got %d", len(vals))
	}
}
