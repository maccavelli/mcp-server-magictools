package util

import (
	"container/list"
	"sync"
)

// S3FIFOCache is a generic, thread-safe S3-FIFO cache.
// It uses three queues (Small, Main, Ghost) to provide high hit ratios and avoid "One-Hit Wonder" thrashing.
type S3FIFOCache[K comparable, V any] struct {
	capacity int
	sLimit   int
	mLimit   int
	gLimit   int

	mu sync.RWMutex

	sList *list.List
	mList *list.List
	gList *list.List

	cache map[K]*list.Element // holds items in S or M
	ghost map[K]*list.Element // holds items in G
}

type s3entry[K comparable, V any] struct {
	key   K
	value V
	freq  int
	isM   bool // true if in M, false if in S
}

func s3Entry[K comparable, V any](ele *list.Element) (*s3entry[K, V], bool) {
	ent, ok := ele.Value.(*s3entry[K, V])
	return ent, ok
}

// NewS3FIFOCache creates a new S3FIFOCache with the given capacity.
func NewS3FIFOCache[K comparable, V any](capacity int) *S3FIFOCache[K, V] {
	sLimit := capacity / 10
	if sLimit < 1 && capacity > 0 {
		sLimit = 1
	}
	mLimit := max(capacity-sLimit, 0)
	gLimit := capacity

	return &S3FIFOCache[K, V]{
		capacity: capacity,
		sLimit:   sLimit,
		mLimit:   mLimit,
		gLimit:   gLimit,
		sList:    list.New(),
		mList:    list.New(),
		gList:    list.New(),
		cache:    make(map[K]*list.Element),
		ghost:    make(map[K]*list.Element),
	}
}

func (c *S3FIFOCache[K, V]) Add(key K, value V) bool { //nolint:revive // distinct cache type; method name matches LRU API intentionally
	c.mu.Lock()
	defer c.mu.Unlock()

	// If already in S or M, update value and increment freq
	if ele, ok := c.cache[key]; ok {
		ent, ok := s3Entry[K, V](ele)
		if !ok {
			return false
		}
		ent.value = value
		if ent.freq < 3 {
			ent.freq++
		}
		return false
	}

	evicted := false

	// If in Ghost queue, remove from Ghost and add to M
	if ele, ok := c.ghost[key]; ok {
		c.gList.Remove(ele)
		delete(c.ghost, key)
		c.insertM(key, value)
		evicted = true // We are adding a new active item, technically a cache miss/eviction happened previously
		return evicted
	}

	// Otherwise, insert into S
	c.insertS(key, value)
	return true
}

func (c *S3FIFOCache[K, V]) Get(key K) (value V, ok bool) { //nolint:revive // distinct cache type; method name matches LRU API intentionally
	c.mu.Lock()
	defer c.mu.Unlock()

	if ele, hit := c.cache[key]; hit {
		if ent, valid := s3Entry[K, V](ele); valid {
			if ent.freq < 3 {
				ent.freq++
			}
			return ent.value, true
		}
	}
	return
}

func (c *S3FIFOCache[K, V]) Values() []V { //nolint:revive // distinct cache type; method name matches LRU API intentionally
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Returns values from M first (MRU/frequent), then S
	values := make([]V, 0, len(c.cache))
	for ele := c.mList.Front(); ele != nil; ele = ele.Next() {
		if ent, ok := s3Entry[K, V](ele); ok {
			values = append(values, ent.value)
		}
	}
	for ele := c.sList.Front(); ele != nil; ele = ele.Next() {
		if ent, ok := s3Entry[K, V](ele); ok {
			values = append(values, ent.value)
		}
	}
	return values
}

// insertS inserts into Small queue, evicting if necessary
func (c *S3FIFOCache[K, V]) insertS(key K, value V) {
	if c.capacity == 0 {
		return
	}

	for c.sList.Len() >= c.sLimit && c.sList.Len() > 0 {
		c.evictS()
	}

	ent := &s3entry[K, V]{key: key, value: value, freq: 0, isM: false}
	ele := c.sList.PushFront(ent)
	c.cache[key] = ele
}

// insertM inserts into Main queue, evicting if necessary
func (c *S3FIFOCache[K, V]) insertM(key K, value V) {
	if c.capacity == 0 {
		return
	}

	for c.mList.Len() >= c.mLimit && c.mList.Len() > 0 {
		c.evictM()
	}

	ent := &s3entry[K, V]{key: key, value: value, freq: 0, isM: true}
	ele := c.mList.PushFront(ent)
	c.cache[key] = ele
}

// evictS pops the oldest element from S
func (c *S3FIFOCache[K, V]) evictS() {
	ele := c.sList.Back()
	if ele == nil {
		return
	}
	c.sList.Remove(ele)
	ent, ok := s3Entry[K, V](ele)
	if !ok {
		return
	}
	delete(c.cache, ent.key)

	if ent.freq > 0 {
		// Promoted to M
		c.insertM(ent.key, ent.value)
	} else {
		// Evicted completely, insert metadata to Ghost
		c.insertG(ent.key)
	}
}

// evictM pops the oldest element from M
func (c *S3FIFOCache[K, V]) evictM() {
	ele := c.mList.Back()
	if ele == nil {
		return
	}
	c.mList.Remove(ele)
	ent, ok := s3Entry[K, V](ele)
	if !ok {
		return
	}

	if ent.freq > 0 {
		// Decrement and re-insert into M (S3-FIFO algorithm standard)
		ent.freq--
		newEle := c.mList.PushFront(ent)
		c.cache[ent.key] = newEle
	} else {
		// Evicted completely
		delete(c.cache, ent.key)
	}
}

// insertG inserts key into Ghost queue
func (c *S3FIFOCache[K, V]) insertG(key K) {
	if c.capacity == 0 {
		return
	}

	for c.gList.Len() >= c.gLimit && c.gList.Len() > 0 {
		oldest := c.gList.Back()
		if oldest != nil {
			c.gList.Remove(oldest)
			if k, ok := oldest.Value.(K); ok {
				delete(c.ghost, k)
			}
		}
	}

	ele := c.gList.PushFront(key)
	c.ghost[key] = ele
}

// Delete removes a key from the cache completely.
func (c *S3FIFOCache[K, V]) Delete(key K) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if ele, ok := c.cache[key]; ok {
		ent, ok := s3Entry[K, V](ele)
		if !ok {
			return
		}
		if ent.isM {
			c.mList.Remove(ele)
		} else {
			c.sList.Remove(ele)
		}
		delete(c.cache, key)
	}

	if ele, ok := c.ghost[key]; ok {
		c.gList.Remove(ele)
		delete(c.ghost, key)
	}
}

// Clear removes all items from the cache.
func (c *S3FIFOCache[K, V]) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.sList.Init()
	c.mList.Init()
	c.gList.Init()
	clear(c.cache)
	clear(c.ghost)
}

// Peek returns the value for the key without updating the frequency counter or list order.
func (c *S3FIFOCache[K, V]) Peek(key K) (value V, ok bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	if ele, hit := c.cache[key]; hit {
		if ent, valid := s3Entry[K, V](ele); valid {
			return ent.value, true
		}
	}
	return
}
