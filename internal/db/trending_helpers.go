package db

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type trendSnapshot struct {
	ts     int64
	calls  int64
	faults int64
}

type trendWindowData struct {
	earliest30m *trendSnapshot
	latest30m   *trendSnapshot
	earliest4h  *trendSnapshot
	latest4h    *trendSnapshot
	earliestAll *trendSnapshot
	latestAll   *trendSnapshot
}

func updateTrendWindows(w *trendWindowData, snap *trendSnapshot, ts, win30m, win4h int64) {
	if w.earliestAll == nil || ts < w.earliestAll.ts {
		w.earliestAll = snap
	}
	if w.latestAll == nil || ts > w.latestAll.ts {
		w.latestAll = snap
	}
	if ts >= win30m {
		if w.earliest30m == nil || ts < w.earliest30m.ts {
			w.earliest30m = snap
		}
		if w.latest30m == nil || ts > w.latest30m.ts {
			w.latest30m = snap
		}
	}
	if ts >= win4h {
		if w.earliest4h == nil || ts < w.earliest4h.ts {
			w.earliest4h = snap
		}
		if w.latest4h == nil || ts > w.latest4h.ts {
			w.latest4h = snap
		}
	}
}

func (s *Store) scanToolHistoryWindows(win30m, win4h int64) map[string]*trendWindowData {
	byURN := make(map[string]*trendWindowData)
	historyPrefix := []byte("telemetry:tool:")
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.Prefix = historyPrefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(historyPrefix); it.ValidForPrefix(historyPrefix); it.Next() {
			item := it.Item()
			key := string(item.Key())
			rest := strings.TrimPrefix(key, "telemetry:tool:")
			lastColon := strings.LastIndex(rest, ":")
			if lastColon <= 0 {
				continue
			}
			urn := rest[:lastColon]
			var ts int64
			if _, scanErr := fmt.Sscanf(rest[lastColon+1:], "%d", &ts); scanErr != nil {
				slog.Warn("db: failed to parse telemetry timestamp", "rest", rest, "error", scanErr)
			}
			itemValueOrWarn(item, func(val []byte) error {
				var entry map[string]any
				if err := json.Unmarshal(val, &entry); err != nil {
					return err
				}
				var calls, faults int64
				if v, ok := entry["calls"].(float64); ok {
					calls = int64(v)
				}
				if v, ok := entry["faults"].(float64); ok {
					faults = int64(v)
				}
				w, exists := byURN[urn]
				if !exists {
					w = &trendWindowData{}
					byURN[urn] = w
				}
				updateTrendWindows(w, &trendSnapshot{ts: ts, calls: calls, faults: faults}, ts, win30m, win4h)
				return nil
			})
		}
		return nil
	})
	return byURN
}

func trendReliability(snap *trendSnapshot) float64 {
	if snap == nil || snap.calls == 0 {
		return 1.0
	}
	return float64(snap.calls-snap.faults) / float64(snap.calls)
}

func buildTrendingDeltas(byURN map[string]*trendWindowData) map[string]map[string]float64 {
	trending := make(map[string]map[string]float64)
	for urn, w := range byURN {
		t := map[string]float64{deltaKey30m: 0.0, deltaKey4h: 0.0, deltaKeyAll: 0.0}
		if w.earliest30m != nil && w.latest30m != nil {
			t[deltaKey30m] = trendReliability(w.latest30m) - trendReliability(w.earliest30m)
		}
		if w.earliest4h != nil && w.latest4h != nil {
			t[deltaKey4h] = trendReliability(w.latest4h) - trendReliability(w.earliest4h)
		}
		if w.earliestAll != nil && w.latestAll != nil {
			t[deltaKeyAll] = trendReliability(w.latestAll) - trendReliability(w.earliestAll)
		}
		trending[urn] = t
	}
	return trending
}

func (s *Store) computeTrendingFromHistory() map[string]map[string]float64 {
	now := time.Now().Unix()
	win30m := now - (30 * 60)
	win4h := now - (4 * 3600)
	byURN := s.scanToolHistoryWindows(win30m, win4h)
	return buildTrendingDeltas(byURN)
}
