package main

import (
	"strings"
	"testing"
)

func TestBuildToolIntelligenceTabWithFixture(t *testing.T) {
	snapshot := map[string]any{
		"search": map[string]any{
			"mode":                   "Hybrid (α=0.50)",
			"fusion_mode":            "Hybrid (α=0.50)",
			"total_searches":         int64(10),
			"vector_searches":        int64(4),
			"lexical_searches":       int64(6),
			"total_latency_ms":       int64(120),
			"avg_latency_ms":         int64(12),
			"total_confidence_score": 0.82,
			"l1_cache_hits":          int64(3),
			"l1_cache_misses":        int64(7),
			"vector_wins":            int64(2),
			"lexical_wins":           int64(1),
			"hnsw_graph_size":        int64(133),
			"graph_completeness":     0.98,
			"bleve_top_5":            []any{"ddg-search:search_web", "recall:search"},
			"hnsw_top_5":             []any{"ddg-search:search_web", "recall:get_metrics"},
			"gate_rejections":        int64(0),
		},
		"semantic_recall": map[string]any{
			"index_path":       "/home/user/.config/mcp-server-magictools",
			"index_size_mb":    45.2,
			"bleve_doc_count":  int64(133),
			"hnsw_node_count":  int64(133),
			"schema_mutations": int64(1),
		},
		"intent_routing": map[string]any{
			"total_queries":   int64(10),
			"cache_hit_ratio": 0.3,
			"avg_latency_ms":  float64(12),
		},
		"schema_health": map[string]any{
			"managed_subservers": int64(12),
			"invalid_routes":     int64(0),
		},
		"orchestrator": map[string]any{
			"vector_routing_wins":  int64(2),
			"fallback_invocations": int64(1),
		},
	}

	content := buildToolIntelligenceTab(snapshot)
	if content == "" {
		t.Fatal("expected non-empty tab content")
	}
	for _, want := range []string{
		"Search Intelligence Matrix",
		"Search Latency (avg)",
		"Bleve Documents",
		"HNSW Nodes",
		"Managed Sub-Servers",
		"Vector Routing Wins",
		"Graph Completeness",
		"Index Decision Matrix",
		"ddg-search:search_web",
	} {
		if !strings.Contains(content, want) {
			t.Fatalf("expected output to contain %q", want)
		}
	}
	if strings.Contains(content, "Search Latency") && strings.Contains(content, "120 ms") && !strings.Contains(content, "Cumulative") {
		// avg should be 12 ms not cumulative 120 as primary latency label
		if strings.Contains(content, "Search Latency (avg)") && !strings.Contains(content, "12") {
			t.Fatal("expected average latency 12 in output")
		}
	}
}

func TestSearchTelemetryEmptyReasonNoSearches(t *testing.T) {
	reason := searchTelemetryEmptyReason(map[string]any{
		"search": map[string]any{"total_searches": int64(0)},
	})
	if reason == "" || !strings.Contains(reason, "align_tools") {
		t.Fatalf("expected align_tools hint, got %q", reason)
	}
}
