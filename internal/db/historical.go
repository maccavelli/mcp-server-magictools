package db

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"

	"github.com/dgraph-io/badger/v4"
)

// FlushMetricBucket writes the 1-minute gauge snapshot and tool history to BadgerDB in a single transaction.
func FlushMetricBucket(store *Store, snapshot map[string]any) {
	if store == nil || store.DB == nil {
		return
	}

	ts := time.Now().Truncate(time.Minute).Unix()
	key := fmt.Sprintf("telemetry:bucket:%d", ts)

	b, err := json.Marshal(snapshot)
	if err != nil {
		return
	}

	err = store.DB.Update(func(txn *badger.Txn) error {
		// 1. Persist the current gauges bucket (30-day TTL)
		eBucket := badger.NewEntry([]byte(key), b).WithTTL(30 * 24 * time.Hour)
		if err := txn.SetEntry(eBucket); err != nil {
			return err
		}

		// 2. Persist per-tool history
		if toolsRaw, ok := snapshot["tools"]; ok {
			switch tools := toolsRaw.(type) {
			case map[string]*telemetry.ToolMetrics:
				for urn, metrics := range tools {
					if err := setToolHistoryEntry(txn, urn, metrics); err != nil {
						return err
					}
				}
			case map[string]telemetry.ToolMetrics:
				for urn, metrics := range tools {
					if err := setToolHistoryEntry(txn, urn, &metrics); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("database: failed to flush metrics bucket", "error", err)
	}
}

func setToolHistoryEntry(txn *badger.Txn, urn string, metrics *telemetry.ToolMetrics) error {
	if metrics == nil {
		return nil
	}

	ts := time.Now().Unix()
	key := fmt.Sprintf("telemetry:tool:%s:%d", urn, ts)

	avgMs := int64(0)
	if metrics.Calls > 0 {
		avgMs = metrics.TotalMs / metrics.Calls
	}

	val := map[string]any{
		"calls":  metrics.Calls,
		"avg_ms": avgMs,
		"faults": metrics.Faults,
	}
	b, err := json.Marshal(val)
	if err != nil {
		return err
	}
	e := badger.NewEntry([]byte(key), b).WithTTL(30 * 24 * time.Hour)
	return txn.SetEntry(e)
}

// FlushHFSCTrace persists a single HFSC trace to BadgerDB and the Bleve index.
func FlushHFSCTrace(store *Store, doc HFSCTraceDocument) {
	if store == nil || store.DB == nil {
		return
	}
	key := fmt.Sprintf("telemetry:trace:%s", doc.ID)
	b, err := json.Marshal(doc)
	if err == nil {
		updateOrWarn(store.DB, func(txn *badger.Txn) error {
			e := badger.NewEntry([]byte(key), b).WithTTL(30 * 24 * time.Hour)
			return txn.SetEntry(e)
		})
	}
	indexHFSCTraceOrWarn(store.Index, doc)
}

// ProxyCallRecord represents a captured JSON-RPC CallTool invocation natively
type ProxyCallRecord struct {
	URN       string         `json:"urn"`
	Arguments map[string]any `json:"arguments"`
	Response  any            `json:"response,omitempty,omitzero"`
	Error     string         `json:"error,omitempty,omitzero"`
	LatencyMs int64          `json:"latency_ms"`
	Timestamp int64          `json:"timestamp"`
}

// FlushProxyCall stores raw tool request/response payloads for Time Travel Debugging (Phase 2).
func FlushProxyCall(store *Store, record ProxyCallRecord) {
	if store == nil || store.DB == nil {
		return
	}
	key := fmt.Sprintf("proxy:call:%s:%d", record.URN, record.Timestamp)
	b, err := json.Marshal(record)
	if err == nil {
		updateOrWarn(store.DB, func(txn *badger.Txn) error {
			e := badger.NewEntry([]byte(key), b).WithTTL(7 * 24 * time.Hour)
			return txn.SetEntry(e)
		})
	}
}

// TokenQuotaRecord represents long-term usage tracking for the LLM backplane
type TokenQuotaRecord struct {
	TotalTokens int64            `json:"total_tokens"`
	PerServer   map[string]int64 `json:"per_server"`
}

// FlushTokenQuotas persists LLM token usage to BadgerDB.
func FlushTokenQuotas(store *Store, total int64, perServer map[string]int64) {
	if store == nil || store.DB == nil {
		return
	}
	record := TokenQuotaRecord{
		TotalTokens: total,
		PerServer:   perServer,
	}
	if b, err := json.Marshal(record); err == nil {
		updateOrWarn(store.DB, func(txn *badger.Txn) error {
			return txn.SetEntry(badger.NewEntry([]byte("llm:quota:global"), b))
		})
	}
}

// LoadTokenQuotas retrieves long-term LLM token usage from BadgerDB on boot.
func LoadTokenQuotas(store *Store) (int64, map[string]int64) {
	var total int64
	perServer := make(map[string]int64)
	if store == nil || store.DB == nil {
		return total, perServer
	}
	viewOrWarn(store.DB, func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("llm:quota:global"))
		if err == nil {
			itemValueOrWarn(item, func(val []byte) error {
				var rec TokenQuotaRecord
				if json.Unmarshal(val, &rec) == nil {
					total = rec.TotalTokens
					perServer = rec.PerServer
				}
				return nil
			})
		}
		return err
	})
	return total, perServer
}
