package db

import (
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRegistryCacheMetricsAndCleaner(t *testing.T) {
	c := NewRegistryCache()
	// Set with negative TTL so it expires immediately (Unix time precision is 1 sec)
	c.Set("key1", "val", -1*time.Second)

	// Hits and misses
	_, _ = c.Get("key1")
	_, _ = c.Get("unknown")

	hits, misses, cnt := c.GetMetrics()
	// Get("key1") will miss and lazily EVICT because it's expired
	if hits != 0 || misses != 2 || cnt != 0 {
		t.Errorf("metrics mismatch: %d %d %d", hits, misses, cnt)
	}

	ctx := t.Context()

	go c.StartCleaner(ctx, 10*time.Millisecond)

	// Wait for expiration and cleaner sweep
	time.Sleep(50 * time.Millisecond)

	_, _, cnt = c.GetMetrics()
	if cnt != 0 {
		t.Errorf("expected 0 items after cleaner ran, got %d", cnt)
	}
}

func TestRegistryCacheEvictionGuard(t *testing.T) {
	c := NewRegistryCache()
	// Fill up to 2048 to trigger LRU/Size limit
	for i := range 2049 {
		c.Set(string(rune(i)), "val", 1*time.Minute)
	}

	_, _, cnt := c.GetMetrics()
	if cnt > 2048 || cnt == 0 {
		t.Errorf("expected eviction to occur, got count %d", cnt)
	}
}

func TestResponseCache(t *testing.T) {
	c := NewResponseCache()

	res := &mcp.CallToolResult{}

	c.Set("tool1", res, -1*time.Second)

	val, ok := c.Get("tool1")
	// Will miss because it's expired
	if ok || val != nil {
		t.Errorf("expected miss due to expiration")
	}

	_, ok = c.Get("unknown")
	if ok {
		t.Errorf("expected unknown to be false")
	}

	hits, misses, cnt := c.GetMetrics()
	// Get("tool1") lazily evicted the item
	if hits != 0 || misses != 2 || cnt != 0 {
		t.Errorf("metrics mismatch: %d %d %d", hits, misses, cnt)
	}

	ctx := t.Context()

	go c.StartCleaner(ctx, 10*time.Millisecond)

	time.Sleep(50 * time.Millisecond)

	_, _, cnt = c.GetMetrics()
	if cnt != 0 {
		t.Errorf("expected 0 items after cleaner, got %d", cnt)
	}
}

func TestResponseCacheEvictionGuard(t *testing.T) {
	c := NewResponseCache()
	// Fill up to 2048 to trigger LRU/Size limit
	for i := range 2049 {
		c.Set(string(rune(i)), &mcp.CallToolResult{}, 1*time.Minute)
	}

	_, _, cnt := c.GetMetrics()
	if cnt > 2048 || cnt == 0 {
		t.Errorf("expected eviction to occur, got count %d", cnt)
	}
}
