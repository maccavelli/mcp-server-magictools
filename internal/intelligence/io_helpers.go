package intelligence

import (
	"log/slog"

	"github.com/dgraph-io/badger/v4"
	"golang.org/x/sync/errgroup"
)

func viewOrWarn(db *badger.DB, fn func(txn *badger.Txn) error) {
	if db == nil {
		return
	}
	if err := db.View(fn); err != nil {
		slog.Warn("database scan failed", "component", hydratorComponent, "error", err)
	}
}

func itemValueOrWarn(item *badger.Item, fn func(val []byte) error) {
	if item == nil {
		return
	}
	if err := item.Value(fn); err != nil {
		slog.Warn("failed to read item value", "component", hydratorComponent, "error", err)
	}
}

func waitGroupOrWarn(eg *errgroup.Group) {
	if eg == nil {
		return
	}
	if err := eg.Wait(); err != nil {
		slog.Warn("hydration sweep group wait failed", "component", hydratorComponent, "error", err)
	}
}

func stringFrom(v any) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

func mapFrom(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func anySliceFrom(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func safeUint64FromInt(n int) uint64 {
	if n < 0 {
		return 0
	}
	const maxUint64 = ^uint64(0)
	if uint64(n) > maxUint64 { //nolint:gosec // guarded upper bound
		return maxUint64
	}
	return uint64(n) //nolint:gosec // non-negative int fits uint64 on this platform
}
