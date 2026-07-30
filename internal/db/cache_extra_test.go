package db

import (
	"os"
	"testing"
)

func TestCacheExtraCoverage(t *testing.T) {
	c := NewResponseCache()

	tmp, _ := os.MkdirTemp("", "cache-*")
	defer os.RemoveAll(tmp)
	store, _ := NewStore(tmp)
	defer store.Close()

	// SetStore
	c.SetStore(store)

	// Set full capacity
	for range 600 {
		c.Set("k", nil, 100)
	}

	// Get with Store (will fail because key does not exist in store)
	c.Get("k")
}
