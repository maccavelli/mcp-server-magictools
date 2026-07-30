package db

import (
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/dgraph-io/badger/v4"
)

type loadedMetric struct {
	succ    int64
	tot     int64
	latSum  int64
	lastErr string
}

func (s *Store) collectPendingMetrics() map[string]*loadedMetric {
	loaded := make(map[string]*loadedMetric)
	s.metricsBuf.Range(func(key, value any) bool {
		urn, ok := stringFromMapKey(key)
		if !ok {
			return true
		}
		delta, ok := metricDeltaFromMapVal(value)
		if !ok {
			return true
		}
		delta.mu.Lock()
		if delta.Total == 0 && delta.LatencySum == 0 && delta.LastError == "" {
			delta.mu.Unlock()
			return true
		}
		loaded[urn] = &loadedMetric{
			succ:    int64(delta.Successes),
			tot:     int64(delta.Total),
			latSum:  delta.LatencySum,
			lastErr: delta.LastError,
		}
		delta.Successes = 0
		delta.Total = 0
		delta.LatencySum = 0
		delta.LastError = ""
		delta.mu.Unlock()
		return true
	})
	return loaded
}

func applyLoadedMetricToIntel(intel *ToolIntelligence, delta *loadedMetric) {
	if intel.Metrics.ProxyReliability == 0 {
		intel.Metrics.ProxyReliability = 1.0
	}
	dRate := 0.005
	if delta.tot > 0 && delta.latSum > 0 {
		prevCalls := int64(intel.Metrics.TotalCalls)
		if prevCalls > 0 {
			newTotal := float64(prevCalls + delta.tot)
			blended := (float64(intel.Metrics.AvgLatencyMs)*float64(prevCalls) + float64(delta.latSum)) / newTotal
			intel.Metrics.AvgLatencyMs = int64(blended + 0.5)
		} else {
			intel.Metrics.AvgLatencyMs = int64(float64(delta.latSum)/float64(delta.tot) + 0.5)
		}
	}
	intel.Metrics.TotalCalls += int(delta.tot)
	intel.Metrics.TotalSuccessfulCalls += int(delta.succ)
	fails := delta.tot - delta.succ
	intel.Metrics.ProxyReliability += dRate * float64(delta.succ)
	if intel.Metrics.TotalSuccessfulCalls >= 50 {
		intel.Metrics.ProxyReliability -= (dRate / 2.0) * float64(fails)
	} else {
		intel.Metrics.ProxyReliability -= dRate * float64(fails)
	}
	if intel.Metrics.TotalCalls > 0 {
		intel.Metrics.FailureRate = 1.0 - float64(intel.Metrics.TotalSuccessfulCalls)/float64(intel.Metrics.TotalCalls)
	}
	if intel.Metrics.ProxyReliability < 0.5 {
		intel.Metrics.ProxyReliability = 0.5
	}
	if intel.Metrics.ProxyReliability > 2.0 {
		intel.Metrics.ProxyReliability = 2.0
	}
	if delta.lastErr != "" {
		intel.Metrics.LastErrorClass = delta.lastErr
	}
}

func (s *Store) flushLoadedMetrics(txn *badger.Txn, loaded map[string]*loadedMetric) error {
	for urn, delta := range loaded {
		var intel ToolIntelligence
		item, err := txn.Get([]byte("intel:" + urn))
		if errors.Is(err, badger.ErrKeyNotFound) {
			intel = ToolIntelligence{}
		} else if err != nil {
			continue
		} else {
			itemValueOrWarn(item, func(val []byte) error { return json.Unmarshal(val, &intel) })
		}
		applyLoadedMetricToIntel(&intel, delta)
		if data, err := json.Marshal(intel); err == nil {
			setKeyOrWarn(txn, []byte("intel:"+urn), data)
		}
	}
	return nil
}

func (s *Store) restoreLoadedMetrics(loaded map[string]*loadedMetric) {
	for urn, delta := range loaded {
		v, ok := s.metricsBuf.Load(urn)
		if !ok {
			continue
		}
		d, ok := metricDeltaFromMapVal(v)
		if !ok {
			continue
		}
		d.mu.Lock()
		d.Successes += int(delta.succ)
		d.Total += int(delta.tot)
		d.LatencySum += delta.latSum
		if delta.lastErr != "" {
			d.LastError = delta.lastErr
		}
		d.mu.Unlock()
	}
}

func (s *Store) schedulePostFlushToolRefresh(loaded map[string]*loadedMetric) {
	s.bgWg.Go(func() {
		now := time.Now().UnixMilli()
		if last := lastToolSync.Load(); now-last > 2000 {
			if lastToolSync.CompareAndSwap(last, now) {
				syncOrWarn(s.DB)
			}
		}
		for urn := range loaded {
			s.Cache.Delete("tool:" + urn)
			mergedRecord, err := s.GetTool(urn)
			if err == nil && mergedRecord != nil {
				if err := s.Index.IndexRecord(ToBleveDoc(mergedRecord)); err != nil {
					slog.Warn("Failed to update search index for tool metrics", "urn", urn, "error", err)
				}
			}
		}
	})
}
