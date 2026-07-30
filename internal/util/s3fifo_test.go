package util

import (
	"testing"
)

func TestS3FIFO(t *testing.T) {
	cache := NewS3FIFOCache[string, any](5)

	// Test Add
	cache.Add("k1", "v1")
	cache.Get("k1") // freq = 1

	cache.Add("k2", "v2")
	cache.Get("k2") // freq = 1

	cache.Add("k3", "v3")
	cache.Get("k3") // freq = 1

	// Test Get
	if val, ok := cache.Get("k1"); !ok || val != "v1" {
		t.Errorf("expected v1, got %v", val)
	}

	// Test Values
	vals := cache.Values()
	if len(vals) != 3 {
		t.Errorf("expected 3 values, got %d", len(vals))
	}

	// Test Eviction
	for i := 4; i <= 20; i++ {
		cache.Add(string(rune(i)), i)
	}

	if len(cache.Values()) > 5 {
		t.Errorf("expected at most 5 values, got %d", len(cache.Values()))
	}

	// Test Delete
	cache.Delete("k1")
	if _, ok := cache.Get("k1"); ok {
		t.Errorf("expected k1 to be deleted")
	}

	// Test Clear
	cache.Clear()
	if len(cache.Values()) != 0 {
		t.Errorf("expected cache to be empty")
	}
}
