package handler

import (
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

// AlignCacheEntry stores align_tools discovery results and last fusion top-5 for dashboard telemetry.
type AlignCacheEntry struct {
	Results   []*db.ToolRecord
	BleveTop5 []string
	HnswTop5  []string
}

func snapshotTop5FromMetrics() (bleve, hnsw []string) {
	if bp := telemetry.SearchMetrics.LastBleveTop5.Load(); bp != nil {
		bleve = append([]string(nil), *bp...)
	}
	if hp := telemetry.SearchMetrics.LastHnswTop5.Load(); hp != nil {
		hnsw = append([]string(nil), *hp...)
	}
	return bleve, hnsw
}

func restoreSearchTop5FromCache(entry *AlignCacheEntry) {
	if entry == nil {
		return
	}
	if len(entry.BleveTop5) > 0 {
		bTop := append([]string(nil), entry.BleveTop5...)
		telemetry.SearchMetrics.LastBleveTop5.Store(&bTop)
	}
	if len(entry.HnswTop5) > 0 {
		hTop := append([]string(nil), entry.HnswTop5...)
		telemetry.SearchMetrics.LastHnswTop5.Store(&hTop)
	}
}

func storeAlignFastPathTrace(query, path string) {
	telemetry.SearchMetrics.LastQueryTrace.Store(&telemetry.SearchQueryTrace{
		Query:    query,
		FastPath: path,
	})
}
