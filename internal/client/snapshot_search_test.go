package client

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

func TestIntentRoutingSnapshotCacheRatio(t *testing.T) {
	telemetry.SearchMetrics.AlignCacheHits.Store(8)
	telemetry.SearchMetrics.AlignCacheMisses.Store(2)
	telemetry.SearchMetrics.TotalSearches.Store(100)
	telemetry.SearchMetrics.TotalLatencyMs.Store(500)

	snap := intentRoutingSnapshot(100)
	ratio, ok := snap["cache_hit_ratio"].(float64)
	if !ok {
		t.Fatalf("cache_hit_ratio missing or wrong type")
	}
	if ratio != 0.8 {
		t.Fatalf("expected 0.8 cache ratio, got %v", ratio)
	}
	avg, ok := snap["avg_latency_ms"].(float64)
	if !ok || avg != 5 {
		t.Fatalf("expected avg latency 5, got %v", avg)
	}
}

func TestSemanticRecallSnapshotFields(t *testing.T) {
	snap := semanticRecallSnapshot("/data/db", 12.5, 133, 120)
	if snap["index_path"] != "/data/db" {
		t.Fatalf("unexpected index_path: %v", snap["index_path"])
	}
	if snap["bleve_doc_count"] != int64(133) {
		t.Fatalf("unexpected bleve_doc_count: %v", snap["bleve_doc_count"])
	}
	if snap["hnsw_node_count"] != 120 {
		t.Fatalf("unexpected hnsw_node_count: %v", snap["hnsw_node_count"])
	}
}

func TestSearchSnapshotFromMetricsIncludesLastQuery(t *testing.T) {
	telemetry.SearchMetrics.LastQueryTrace.Store(&telemetry.SearchQueryTrace{
		Query:        "recall metrics",
		FusionWinner: "vector",
		FastPath:     "urn",
	})
	block := searchSnapshotFromMetrics("Hybrid (α=0.50)", "Hybrid (α=0.50)", []string{"a:1"}, []string{"a:2"})
	last, ok := block["last_query"].(map[string]any)
	if !ok {
		t.Fatal("expected last_query block")
	}
	if last["query"] != "recall metrics" {
		t.Fatalf("unexpected query: %v", last["query"])
	}
}
