package telemetry

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// ProxyMetricEvent defines the Tool-Centric JSON schema per ADR-0033
type ProxyMetricAlignment struct {
	ActionVerbBoostApplied bool    `json:"action_verb_boost_applied"`
	DynamicGapPassed       bool    `json:"dynamic_gap_passed"`
	BM25SquashDelta        float64 `json:"bm25_squash_delta,omitempty,omitzero"`
	FusedScore             float64 `json:"fused_score"`
	ConfidenceGap          float64 `json:"confidence_gap"`
}

type ProxyMetricExecution struct {
	SchemaValidationFailures int   `json:"schema_validation_failures"`
	Tier1CoercionsApplied    int   `json:"tier_1_coercions_applied"`
	Tier2StructuredErrors    int   `json:"tier_2_structured_errors"`
	ExecutionLatencyMs       int64 `json:"execution_latency_ms"`
}

type ProxyMetricEvent struct {
	Timestamp          time.Time `json:"timestamp,omitzero"`
	ProxyCorrelationID string    `json:"proxy_correlation_id"`
	ToolURN            string    `json:"tool_urn"`
	ServerName         string    `json:"server_name"`
	IntentClass        string    `json:"intent_class"`
	ResolutionType     string    `json:"resolution_type"`
	Tags               []string  `json:"tags"`
	Metrics            struct {
		Alignment ProxyMetricAlignment `json:"alignment"`
		Execution ProxyMetricExecution `json:"execution"`
	} `json:"metrics"`
}

var proxyMetricsQueue = make(chan ProxyMetricEvent, 4096)

// DispatchProxyMetric safely queues a metric event. Drops the event if the channel is full.
func DispatchProxyMetric(event ProxyMetricEvent) {
	// STD-MCP-TELEMETRY-ASYNC-001: select with default case
	select {
	case proxyMetricsQueue <- event:
	default:
		slog.Warn("telemetry: proxy metrics queue full, shedding payload")
	}
}

// SaveFunc is a function signature for persisting data to the KV store.
type SaveFunc func(key string, val []byte, ttl time.Duration) error

// StartMetricsFlusher starts the background goroutine that persists metrics to BadgerDB.
func StartMetricsFlusher(save SaveFunc) {
	// STD-MCP-CONTEXT-DETACHMENT-001
	ctx := context.WithoutCancel(context.Background())

	go func() {
		for event := range proxyMetricsQueue {
			// Write to proxy_metrics namespace
			key := "proxy_metrics:" + event.ProxyCorrelationID + ":" + event.ToolURN + ":" + event.Timestamp.Format(time.RFC3339Nano)

			val, err := json.Marshal(event)
			if err != nil {
				slog.ErrorContext(ctx, "telemetry: failed to marshal proxy metric", "error", err)
				continue
			}

			// Save to BadgerDB with a TTL of 30 days
			err = save(key, val, 30*24*time.Hour)
			if err != nil {
				slog.ErrorContext(ctx, "telemetry: failed to persist proxy metric", "error", err)
			}
		}
	}()
}
