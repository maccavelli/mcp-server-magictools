package prompt_registry

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type PromptEntry struct {
	ServerName string
	Prompt     mcp.Prompt
	CachedAt   time.Time
}

type PersistedPrompt struct {
	Entries   []PromptEntry `json:"entries"`
	WrittenAt int64         `json:"written_at"`
}

type Cache struct {
	mu        sync.RWMutex
	entries   map[string][]PromptEntry
	flattened []*mcp.Prompt
	dirty     atomic.Bool
	db        *badger.DB
}

func NewCache(db *badger.DB) *Cache {
	c := &Cache{
		entries: make(map[string][]PromptEntry),
		db:      db,
	}
	c.dirty.Store(true)
	return c
}

// HydrateFromDB runs during cold/warm starts to seed the Tier 1 cache from Tier 2 persistence,
// explicitly dropping any expired keys to prevent cache poisoning.
func (c *Cache) HydrateFromDB() {
	if c.db == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	err := c.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("prompt:")
		now := time.Now().Unix()

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := string(item.Key())
			serverName := key[len("prompt:"):]

			err := item.Value(func(val []byte) error {
				var pp PersistedPrompt
				if err := json.Unmarshal(val, &pp); err != nil {
					return err
				}

				// Proactive Garbage Collection: Drop entries older than 1 hour.
				// We don't delete them from Badger here because we are in a View transaction.
				// They will naturally be overwritten or dropped by background tasks.
				if now > pp.WrittenAt+3600 {
					return nil // Expired, skip
				}

				c.entries[serverName] = pp.Entries
				return nil
			})
			if err != nil {
				slog.Warn("prompt_registry: failed to unmarshal cached prompt", "server", serverName, "error", err)
			}
		}
		return nil
	})

	if err != nil {
		slog.Error("prompt_registry: badger hydration failed", "error", err)
	}
	c.dirty.Store(true)
}

func (c *Cache) Refresh(ctx context.Context, serverName string, session *mcp.ClientSession) {
	if session == nil {
		return
	}

	res, err := session.ListPrompts(ctx, &mcp.ListPromptsParams{})
	if err != nil {
		if strings.Contains(err.Error(), "Method not found") {
			return
		}
		slog.Error("prompt_registry: failed to fetch prompts", "server", serverName, "error", err)
		return
	}

	entries := make([]PromptEntry, 0, len(res.Prompts))
	now := time.Now()
	for _, p := range res.Prompts {
		entries = append(entries, PromptEntry{
			ServerName: serverName,
			Prompt:     *p,
			CachedAt:   now,
		})
	}

	c.mu.Lock()
	c.entries[serverName] = entries
	c.mu.Unlock()
	c.dirty.Store(true)

	// Tier 2 Persistence (Fire and Forget)
	if c.db != nil {
		go func() {
			pp := PersistedPrompt{
				Entries:   entries,
				WrittenAt: now.Unix(),
			}
			data, err := json.Marshal(pp)
			if err != nil {
				slog.Error("prompt_registry: failed to marshal persisted prompt", "server", serverName, "error", err)
				return
			}
			key := []byte("prompt:" + serverName)
			err = c.db.Update(func(txn *badger.Txn) error {
				e := badger.NewEntry(key, data).WithTTL(time.Hour)
				return txn.SetEntry(e)
			})
			if err != nil {
				slog.Error("prompt_registry: badger persistence failed", "server", serverName, "error", err)
			}
		}()
	}
}

func (c *Cache) Remove(serverName string) {
	c.mu.Lock()
	delete(c.entries, serverName)
	c.mu.Unlock()
	c.dirty.Store(true)

	if c.db != nil {
		go func() {
			err := c.db.Update(func(txn *badger.Txn) error {
				return txn.Delete([]byte("prompt:" + serverName))
			})
			if err != nil {
				slog.Warn("prompt_registry: failed to delete prompt from badger", "server", serverName, "error", err)
			}
		}()
	}
}

func (c *Cache) List() []*mcp.Prompt {
	if c.dirty.Load() {
		c.mu.Lock()
		if c.dirty.Load() { // double-checked locking
			var flattened []*mcp.Prompt
			for serverName, entries := range c.entries {
				for _, e := range entries {
					// 🛡️ MEMORY SAFETY: create a new struct instance for the pointer
					namespacedPrompt := e.Prompt
					namespacedPrompt.Name = serverName + ":" + e.Prompt.Name
					flattened = append(flattened, &namespacedPrompt)
				}
			}
			c.flattened = flattened
			c.dirty.Store(false)
		}
		c.mu.Unlock()
	}

	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a shallow copy so callers can't modify the slice
	result := make([]*mcp.Prompt, len(c.flattened))
	copy(result, c.flattened)
	return result
}
