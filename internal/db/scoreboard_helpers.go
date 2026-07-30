package db

import (
	"encoding/json"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"

	"github.com/dgraph-io/badger/v4"
)

func buildLiveToolScoreCards(trending map[string]map[string]float64) map[string]any {
	scores := make(map[string]any)
	liveTools := telemetry.GlobalToolTracker.GetAll()
	for urn, m := range liveTools {
		if m.Calls == 0 {
			continue
		}
		reliability := float64(m.Calls-m.Faults) / float64(m.Calls)
		avgMs := m.TotalMs / m.Calls
		card := map[string]any{
			"URN":         urn,
			"Calls":       m.Calls,
			"Faults":      m.Faults,
			"Reliability": reliability,
			"AvgMs":       avgMs,
			"Baseline":    1.0,
			"Deviation":   reliability - 1.0,
			deltaKey30m:   0.0,
			deltaKey4h:    0.0,
			deltaKeyAll:   0.0,
		}
		if t, ok := trending[urn]; ok {
			card[deltaKey30m] = t[deltaKey30m]
			card[deltaKey4h] = t[deltaKey4h]
			card[deltaKeyAll] = t[deltaKeyAll]
		}
		scores[urn] = card
	}
	return scores
}

func (s *Store) overlayIntelBaselines(scores map[string]any) {
	if s == nil || s.DB == nil {
		return
	}
	intelPrefix := []byte("intel:")
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.PrefetchSize = 50
		opts.Prefix = intelPrefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Seek(intelPrefix); it.ValidForPrefix(intelPrefix); it.Next() {
			item := it.Item()
			urn := strings.TrimPrefix(string(item.Key()), "intel:")
			cardRaw, exists := scores[urn]
			if !exists {
				continue
			}
			card, ok := cardRaw.(map[string]any)
			if !ok {
				continue
			}
			itemValueOrWarn(item, func(val []byte) error {
				var intel map[string]any
				if err := json.Unmarshal(val, &intel); err != nil {
					return err
				}
				metrics, ok := intel["metrics"].(map[string]any)
				if !ok {
					return nil
				}
				rel, ok := metrics["proxy_reliability"].(float64)
				if !ok || rel <= 0 {
					return nil
				}
				card["Baseline"] = rel
				if r, ok := card["Reliability"].(float64); ok {
					card["Deviation"] = r - rel
				}
				return nil
			})
		}
		return nil
	})
}

func capScoreBoardEntries(scores map[string]any, limit int) {
	if len(scores) <= limit {
		return
	}
	type urnCalls struct {
		urn   string
		calls int64
	}
	var sorted []urnCalls
	for urn, raw := range scores {
		card, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		c, ok := card["Calls"].(int64)
		if !ok {
			continue
		}
		sorted = append(sorted, urnCalls{urn, c})
	}
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[i].calls < sorted[j].calls {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	keep := make(map[string]bool)
	for i := 0; i < limit && i < len(sorted); i++ {
		keep[sorted[i].urn] = true
	}
	for urn := range scores {
		if !keep[urn] {
			delete(scores, urn)
		}
	}
}
