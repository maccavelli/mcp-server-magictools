package db

import (
	"log/slog"
	"os"
	"regexp"
	"sync"
	"sync/atomic"

	"github.com/dgraph-io/badger/v4"
)

func viewOrWarn(db *badger.DB, fn func(txn *badger.Txn) error) {
	if db == nil {
		return
	}
	if err := db.View(fn); err != nil {
		slog.Warn("db: view failed", "error", err)
	}
}

func updateOrWarn(db *badger.DB, fn func(txn *badger.Txn) error) {
	if db == nil {
		return
	}
	if err := db.Update(fn); err != nil {
		slog.Warn("db: update failed", "error", err)
	}
}

func (s *Store) updateWithRetryOrWarn(fn func(txn *badger.Txn) error) {
	if s == nil {
		return
	}
	if err := s.UpdateWithRetry(fn); err != nil {
		slog.Warn("db: update with retry failed", "error", err)
	}
}

func deleteKeyOrWarn(txn *badger.Txn, key []byte) {
	if txn == nil {
		return
	}
	if err := txn.Delete(key); err != nil {
		slog.Warn("db: failed to delete key", "key", string(key), "error", err)
	}
}

func setKeyOrWarn(txn *badger.Txn, key, val []byte) {
	if txn == nil {
		return
	}
	if err := txn.Set(key, val); err != nil {
		slog.Warn("db: failed to set key", "key", string(key), "error", err)
	}
}

func itemValueOrWarn(item *badger.Item, fn func(val []byte) error) {
	if item == nil {
		return
	}
	if err := item.Value(fn); err != nil {
		slog.Warn("db: failed to read item value", "error", err)
	}
}

func wbDeleteOrWarn(wb *badger.WriteBatch, key []byte) {
	if wb == nil {
		return
	}
	if err := wb.Delete(key); err != nil {
		slog.Warn("db: write batch delete failed", "key", string(key), "error", err)
	}
}

func syncOrWarn(db *badger.DB) {
	if db == nil {
		return
	}
	if err := db.Sync(); err != nil {
		slog.Warn("db: sync failed", "error", err)
	}
}

func removeAllOrWarn(path string) {
	if path == "" {
		return
	}
	if err := os.RemoveAll(path); err != nil {
		slog.Warn("db: failed to remove path", "path", path, "error", err)
	}
}

func closeIndexOrWarn(idx *SearchIndex) {
	if idx == nil {
		return
	}
	if err := idx.Close(); err != nil {
		slog.Warn("db: failed to close search index", "error", err)
	}
}

func synergyCounterOrNew(m *sync.Map, hash string) *atomic.Int64 {
	v, loaded := m.LoadOrStore(hash, &atomic.Int64{})
	if c, ok := v.(*atomic.Int64); ok {
		if !loaded {
			slog.Debug("db: initialized synergy counter", "hash", hash)
		}
		return c
	}
	c := &atomic.Int64{}
	m.Store(hash, c)
	return c
}

func atomicInt64FromMapVal(v any) (*atomic.Int64, bool) {
	c, ok := v.(*atomic.Int64)
	return c, ok
}

func stringFromMapKey(k any) (string, bool) {
	s, ok := k.(string)
	return s, ok
}

func mapFromAny(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func regexpFromCache(v any) (*regexp.Regexp, bool) {
	re, ok := v.(*regexp.Regexp)
	return re, ok
}

func countKeysOrZero(s *Store, prefix string) int {
	n, err := s.countKeys(prefix)
	if err != nil {
		slog.Warn("db: count keys failed", "prefix", prefix, "error", err)
		return 0
	}
	return n
}

func pruneSyntheticDocsOrWarn(idx *SearchIndex) int {
	if idx == nil {
		return 0
	}
	pruned, err := idx.PruneSyntheticDocs()
	if err != nil {
		slog.Warn("db: prune synthetic docs failed", "error", err)
		return 0
	}
	return pruned
}

func metricDeltaOrNew(m *sync.Map, urn string) *metricDelta {
	v, loaded := m.LoadOrStore(urn, &metricDelta{})
	if d, ok := v.(*metricDelta); ok {
		if !loaded {
			slog.Debug("db: initialized metric delta", "urn", urn)
		}
		return d
	}
	d := &metricDelta{}
	m.Store(urn, d)
	return d
}

func metricDeltaFromMapVal(v any) (*metricDelta, bool) {
	d, ok := v.(*metricDelta)
	return d, ok
}

func closeFileOrWarn(f *os.File, label string) {
	if f == nil {
		return
	}
	if err := f.Close(); err != nil {
		slog.Warn("db: failed to close file", "label", label, "error", err)
	}
}

func safeInt64Diff(a, b uint64) int64 {
	if a <= b {
		return 0
	}
	return int64(a - b) //nolint:gosec // guarded subtraction avoids overflow
}

func safeUint64FromInt(n int) uint64 {
	if n < 0 {
		return 0
	}
	return uint64(n) //nolint:gosec // negative values clamped to zero
}

func indexHFSCTraceOrWarn(idx *SearchIndex, doc HFSCTraceDocument) {
	if idx == nil {
		return
	}
	if err := idx.IndexHFSCTrace(doc); err != nil {
		slog.Warn("db: failed to index HFSC trace", "id", doc.ID, "error", err)
	}
}
