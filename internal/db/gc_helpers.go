package db

import (
	"context"
	"errors"
	"log/slog"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"

	"github.com/dgraph-io/badger/v4"
)

func (s *Store) runBackgroundGCTick() {
	s.FlushMetrics()

	validKeys := make(map[string]bool)
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			urn := string(it.Item().Key()[len(prefix):])
			validKeys[urn] = true
		}
		return nil
	})
	if engine := vector.GetEngine(); engine != nil && engine.VectorEnabled() {
		engine.PruneOrphanedNodes(validKeys)
	}

	s.reconcileBleveDrift(validKeys)
	s.pruneSynergyOnGCTick()

	var gcCount int
	for {
		err := s.DB.RunValueLogGC(0.5)
		if err != nil {
			if !errors.Is(err, badger.ErrNoRewrite) {
				slog.Error("database: value log garbage collection failed", "error", err)
			}
			break
		}
		gcCount++
	}
	if gcCount > 0 {
		slog.Info("database: value log garbage collection successful", "reclaimed_files", gcCount)
	}
}

func (s *Store) reconcileBleveDrift(validKeys map[string]bool) {
	if s.Index == nil {
		return
	}
	bleveCount, bErr := s.Index.ToolCount()
	if bErr != nil {
		// Can't compute drift this tick, but still cap stale synthetic docs best-effort.
		slog.Warn("database: bleve ToolCount failed; attempting synthetic-doc prune anyway", "error", bErr)
		if pruned := pruneSyntheticDocsOrWarn(s.Index); pruned > 0 {
			slog.Info("database: pruned old synthetic search documents", "count", pruned)
		}
		return
	}
	badgerCount := uint64(len(validKeys))
	if bleveCount >= badgerCount {
		if pruned := pruneSyntheticDocsOrWarn(s.Index); pruned > 0 {
			slog.Info("database: pruned old synthetic search documents", "count", pruned)
		}
		return
	}
	telemetry.SearchMetrics.IndexRepairs.Add(safeInt64Diff(badgerCount, bleveCount)) //nolint:gosec // counts bounded by tool registry size
	slog.Warn("database: Badger→Bleve drift detected, reindexing",
		"badger_tools", badgerCount, "bleve_tools", bleveCount)
	if rErr := s.ReindexAllTools(); rErr != nil {
		slog.Warn("database: drift reindex failed", "error", rErr)
	}
	if pruned := pruneSyntheticDocsOrWarn(s.Index); pruned > 0 {
		slog.Info("database: pruned old synthetic search documents", "count", pruned)
	}
}

func (s *Store) pruneSynergyOnGCTick() {
	if pruned := s.PruneSynergyWeights(); pruned > 0 {
		slog.Info("database: pruned low-value synergy weights", "count", pruned)
	}
}

func (s *Store) startBackgroundReindexIfNeeded(ctx context.Context, index *SearchIndex, drifting bool) {
	s.bgWg.Go(func() {
		select {
		case <-s.closing:
			return
		case <-ctx.Done():
			return
		default:
		}
		if drifting {
			slog.Info("Search index has drifted, performing full re-index in background...")
		} else {
			slog.Info("Search index is empty, performing initial re-index in background...")
		}
		if err := s.ReindexAllTools(); err != nil {
			slog.Warn("background reindex failed", "error", err)
		} else if index != nil {
			if docs, dErr := index.DocCount(); dErr == nil {
				slog.Info("background reindex complete", "docs", docs)
			}
		}
	})
}
