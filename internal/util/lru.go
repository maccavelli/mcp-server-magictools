package util

import (
	"container/list"
	"sync"
)

// LRUCache is a generic, thread-safe Least Recently Used cache.
type LRUCache[K comparable, V any] struct {
	capacity int
	mu       sync.RWMutex
	ll       *list.List
	cache    map[K]*list.Element
}

// entry represents a key-value pair stored in the list.
type entry[K comparable, V any] struct {
	key   K
	value V
}

// NewLRUCache creates a new LRUCache with the given capacity.
func NewLRUCache[K comparable, V any](capacity int) *LRUCache[K, V] {
	return &LRUCache[K, V]{
		capacity: capacity,
		ll:       list.New(),
		cache:    make(map[K]*list.Element),
	}
}

func lruEntry[K comparable, V any](ele *list.Element) (*entry[K, V], bool) {
	ent, ok := ele.Value.(*entry[K, V])
	return ent, ok
}

// Add adds a value to the cache, returning true if an eviction occurred.
// If the key already exists, its value is updated and it is promoted to MRU.
func (c *LRUCache[K, V]) Add(key K, value V) bool { //nolint:revive // distinct cache type; method name matches S3FIFO API intentionally
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		c.cache = make(map[K]*list.Element)
		c.ll = list.New()
	}

	if ele, hit := c.cache[key]; hit {
		c.ll.MoveToFront(ele)
		if ent, ok := lruEntry[K, V](ele); ok {
			ent.value = value
		}
		return false
	}

	ele := c.ll.PushFront(&entry[K, V]{key, value})
	c.cache[key] = ele
	if c.capacity != 0 && c.ll.Len() > c.capacity {
		c.removeOldest()
		return true
	}
	return false
}

// Get looks up a key's value from the cache, promoting it to MRU if found.
func (c *LRUCache[K, V]) Get(key K) (value V, ok bool) { //nolint:revive // distinct cache type; method name matches S3FIFO API intentionally
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cache == nil {
		return
	}
	if ele, hit := c.cache[key]; hit {
		c.ll.MoveToFront(ele)
		if ent, ok := lruEntry[K, V](ele); ok {
			return ent.value, true
		}
	}
	return
}

// Values returns a slice of all values currently in the cache, from MRU to LRU.
func (c *LRUCache[K, V]) Values() []V { //nolint:revive // distinct cache type; method name matches S3FIFO API intentionally
	c.mu.RLock()
	defer c.mu.RUnlock()

	if c.cache == nil {
		return nil
	}

	values := make([]V, 0, c.ll.Len())
	for ele := c.ll.Front(); ele != nil; ele = ele.Next() {
		if ent, ok := lruEntry[K, V](ele); ok {
			values = append(values, ent.value)
		}
	}
	return values
}

// removeOldest removes the oldest item from the cache.
// Caller must hold the write lock.
func (c *LRUCache[K, V]) removeOldest() {
	if c.cache == nil {
		return
	}
	ele := c.ll.Back()
	if ele != nil {
		c.ll.Remove(ele)
		if kv, ok := lruEntry[K, V](ele); ok {
			delete(c.cache, kv.key)
		}
	}
}
