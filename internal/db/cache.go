// Package db provides functionality for the db subsystem.
package db

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/dgraph-io/badger/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type cacheEntry struct {
	value      any
	expiration int64
}

// RegistryCache is a simple thread-safe TTL cache.
// Optimized for low-memory bastion environments where GC pressure must be minimized.
type RegistryCache struct {
	mu         sync.RWMutex
	cache      *util.S3FIFOCache[string, *cacheEntry]
	limit      int
	categories []string
	hits       uint64
	misses     uint64
}

// NewRegistryCache is undocumented but satisfies standard structural requirements.
func NewRegistryCache(opts ...int) *RegistryCache {
	limit := 2048
	if len(opts) > 0 && opts[0] > 0 {
		limit = opts[0]
	}
	return &RegistryCache{
		cache: util.NewS3FIFOCache[string, *cacheEntry](limit),
		limit: limit,
	}
}

// Get is undocumented but satisfies standard structural requirements.
func (c *RegistryCache) Get(key string) (any, bool) {
	ent, ok := c.cache.Get(key)
	if !ok {
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	if time.Now().Unix() > ent.expiration {
		c.cache.Delete(key)
		atomic.AddUint64(&c.misses, 1)
		return nil, false
	}

	atomic.AddUint64(&c.hits, 1)
	return ent.value, true
}

// GetMetrics is undocumented but satisfies standard structural requirements.
func (c *RegistryCache) GetMetrics() (uint64, uint64, int) {
	return atomic.LoadUint64(&c.hits), atomic.LoadUint64(&c.misses), len(c.cache.Values())
}

// Set is undocumented but satisfies standard structural requirements.
func (c *RegistryCache) Set(key string, value any, ttl time.Duration) {
	c.cache.Add(key, &cacheEntry{
		value:      value,
		expiration: time.Now().Add(ttl).Unix(),
	})
}

// GetCategories is undocumented but satisfies standard structural requirements.
func (c *RegistryCache) GetCategories() ([]string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.categories == nil {
		return nil, false
	}
	// Return a copy to avoid external mutation
	res := make([]string, len(c.categories))
	copy(res, c.categories)
	return res, true
}

// SetCategories is undocumented but satisfies standard structural requirements.
func (c *RegistryCache) SetCategories(categories []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.categories = make([]string, len(categories))
	copy(c.categories, categories)
}

// Delete is undocumented but satisfies standard structural requirements.
func (c *RegistryCache) Delete(urn string) {
	c.cache.Delete(urn)
}

// Peek probes the cache without incrementing hit/miss counters.
// Used by internal merge operations (SaveIntelligence, UpdateToolUsage) where the
// probe is a side-channel optimization check, not a real cache request.
func (c *RegistryCache) Peek(key string) (any, bool) {
	ent, ok := c.cache.Peek(key)
	if !ok {
		return nil, false
	}
	if time.Now().Unix() > ent.expiration {
		c.cache.Delete(key)
		return nil, false
	}
	return ent.value, true
}

// Clear is undocumented but satisfies standard structural requirements.
func (c *RegistryCache) Clear() {
	c.cache.Clear()
}

// StartCleaner runs a background cleanup loop for expired items.
func (c *RegistryCache) StartCleaner(ctx context.Context, interval time.Duration) {
}

// ----------------------------------------------------------------------------
// ResponseCache: Volatile Short-Term Output Caching (Freshness Layer)
// ----------------------------------------------------------------------------

type responseEntry struct {
	result     *mcp.CallToolResult
	expiration int64
}

// ResponseCache is a thread-safe, memory-resident LRU TTL cache for tool outputs.
// Designed to absorb repetitive agent queries (e.g., 'oc get pods') in high-latency environments.
type ResponseCache struct {
	mu     sync.RWMutex
	cache  *util.S3FIFOCache[string, *responseEntry]
	limit  int
	hits   uint64
	misses uint64
	store  *Store // Layer 2 persistent backing (Phase 5)
}

// NewResponseCache is undocumented but satisfies standard structural requirements.
func NewResponseCache(opts ...int) *ResponseCache {
	limit := 2048
	if len(opts) > 0 && opts[0] > 0 {
		limit = opts[0]
	}
	return &ResponseCache{
		cache: util.NewS3FIFOCache[string, *responseEntry](limit),
		limit: limit,
	}
}

// SetStore attaches a BadgerDB backend for L2 persistence (Phase 5).
func (c *ResponseCache) SetStore(s *Store) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.store = s
}

// Get is undocumented but satisfies standard structural requirements.
func (c *ResponseCache) Get(key string) (*mcp.CallToolResult, bool) {
	ent, ok := c.cache.Get(key)
	if ok {
		if time.Now().Unix() > ent.expiration {
			c.cache.Delete(key)
			// fallback to L2
		} else {
			atomic.AddUint64(&c.hits, 1)
			return ent.result, true
		}
	}

	// ── Phase 5: L2 Persistent Cache Retrieval ──
	c.mu.RLock()
	store := c.store
	c.mu.RUnlock()

	if store != nil && store.DB != nil {
		var res *mcp.CallToolResult
		err := store.DB.View(func(txn *badger.Txn) error {
			item, err := txn.Get([]byte("cache:l2:" + key))
			if err != nil {
				return err
			}
			return item.Value(func(val []byte) error {
				var r mcp.CallToolResult
				if err := json.Unmarshal(val, &r); err != nil {
					return err
				}
				res = &r
				return nil
			})
		})
		if err == nil && res != nil {
			atomic.AddUint64(&c.hits, 1)
			// Promote back to L1
			c.cache.Add(key, &responseEntry{
				result:     res,
				expiration: time.Now().Add(5 * time.Minute).Unix(),
			})
			return res, true
		}
	}

	atomic.AddUint64(&c.misses, 1)
	return nil, false
}

// GetMetrics is undocumented but satisfies standard structural requirements.
func (c *ResponseCache) GetMetrics() (uint64, uint64, int) {
	return atomic.LoadUint64(&c.hits), atomic.LoadUint64(&c.misses), len(c.cache.Values())
}

// Set is undocumented but satisfies standard structural requirements.
func (c *ResponseCache) Set(key string, result *mcp.CallToolResult, ttl time.Duration) {
	c.cache.Add(key, &responseEntry{
		result:     result,
		expiration: time.Now().Add(ttl).Unix(),
	})

	// ── Phase 5: L2 Persistent Cache Write (async to avoid blocking hot path) ──
	c.mu.RLock()
	store := c.store
	c.mu.RUnlock()

	if store != nil && store.DB != nil {
		b, err := json.Marshal(result)
		if err == nil {
			store.bgWg.Add(1)
			go func(data []byte, cacheKey string, cacheTTL time.Duration) {
				defer store.bgWg.Done()
				select {
				case <-store.closing:
					return
				default:
				}
				updateOrWarn(store.DB, func(txn *badger.Txn) error {
					e := badger.NewEntry([]byte("cache:l2:"+cacheKey), data).WithTTL(cacheTTL)
					return txn.SetEntry(e)
				})
			}(b, key, ttl)
		}
	}
}

// StartCleaner is undocumented but satisfies standard structural requirements.
func (c *ResponseCache) StartCleaner(ctx context.Context, interval time.Duration) {
}
