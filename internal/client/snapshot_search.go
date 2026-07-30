package client

import (
	"math"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

// searchSnapshotFromMetrics builds the dashboard search telemetry block from live counters.
func searchSnapshotFromMetrics(modeLabel, fusionMode string, bTop, hTop []string) map[string]any {
	totalSearches := telemetry.SearchMetrics.TotalSearches.Load()
	cacheHits := telemetry.SearchMetrics.AlignCacheHits.Load()
	cacheMisses := telemetry.SearchMetrics.AlignCacheMisses.Load()
	avgLat := telemetry.AvgSearchLatencyMs(telemetry.SearchMetrics.TotalLatencyMs.Load(), totalSearches)

	block := map[string]any{
		"mode":                     modeLabel,
		"total_searches":           totalSearches,
		"vector_searches":          telemetry.SearchMetrics.VectorSearches.Load(),
		"align_search_invocations": telemetry.SearchMetrics.AlignSearchInvocations.Load(),
		"lexical_searches":         telemetry.SearchMetrics.LexicalSearches.Load(), // deprecated alias
		"total_latency_ms":         telemetry.SearchMetrics.TotalLatencyMs.Load(),
		"avg_latency_ms":           avgLat,
		"total_confidence_score":   math.Float64frombits(telemetry.SearchMetrics.TotalConfidenceScore.Load()),
		"l1_cache_hits":            cacheHits,
		"l1_cache_misses":          cacheMisses,
		"cache_hits":               telemetry.SearchMetrics.CacheHits.Load(),
		"cache_misses":             telemetry.SearchMetrics.CacheMisses.Load(),
		"vector_stale_skips":       telemetry.SearchMetrics.VectorStaleSkips.Load(),
		"embed_latency_ms":         telemetry.SearchMetrics.EmbedLatencyMs.Load(),
		"query_embed_latency_ms":   telemetry.SearchMetrics.QueryEmbedLatencyMs.Load(),
		"graph_completeness":       math.Float64frombits(telemetry.SearchMetrics.GraphCompletenessRatio.Load()),
		"vector_wins":              telemetry.SearchMetrics.VectorWins.Load(),
		"lexical_wins":             telemetry.SearchMetrics.LexicalWins.Load(),
		"hnsw_graph_size":          getHNSWGraphSize(),
		"fusion_mode":              fusionMode,
		"bleve_top_5":              bTop,
		"hnsw_top_5":               hTop,
		"learning_weight":          math.Float64frombits(telemetry.SearchMetrics.LearningWeight.Load()),
		"gate_rejections":          telemetry.SearchMetrics.GateRejections.Load(),
		"vector_attempts":          telemetry.SearchMetrics.VectorSearchAttempts.Load(),
		"vector_errors":            telemetry.SearchMetrics.VectorSearchErrors.Load(),
	}

	if trace := telemetry.SearchMetrics.LastQueryTrace.Load(); trace != nil {
		block["last_query"] = map[string]any{
			"query":               trace.Query,
			"fusion_winner":       trace.FusionWinner,
			"gates_rejected":      trace.GatesRejected,
			"fast_path":           trace.FastPath,
			"top_bm25":            trace.TopBM25,
			"top_cosine":          trace.TopCosine,
			"bm25_squash_delta":   trace.BM25SquashDelta,
			"vector_attempted":    trace.VectorAttempted,
			"vector_search_error": trace.VectorSearchError,
		}
	}

	return block
}

func intentRoutingSnapshot(totalSearches int64) map[string]any {
	cacheHits := telemetry.SearchMetrics.AlignCacheHits.Load()
	cacheMisses := telemetry.SearchMetrics.AlignCacheMisses.Load()
	return map[string]any{
		"total_queries":   totalSearches,
		"cache_hit_ratio": telemetry.AlignCacheHitRatio(cacheHits, cacheMisses),
		"avg_latency_ms":  float64(telemetry.AvgSearchLatencyMs(telemetry.SearchMetrics.TotalLatencyMs.Load(), totalSearches)),
	}
}

func semanticRecallSnapshot(dbPath string, dbSizeMB float64, bleveDocs int64, hnswNodes int) map[string]any {
	if dbPath == "" {
		dbPath = "unknown"
	}
	return map[string]any{
		"index_path":       dbPath,
		"index_size_mb":    dbSizeMB,
		"bleve_doc_count":  bleveDocs,
		"hnsw_node_count":  hnswNodes,
		"doc_count":        bleveDocs, // deprecated alias
		"schema_mutations": telemetry.SearchMetrics.SchemaMutations.Load(),
	}
}

func schemaHealthSnapshot(managedSubservers int64) map[string]any {
	return map[string]any{
		"managed_subservers": managedSubservers,
		"valid_routes":       managedSubservers, // deprecated alias
		"invalid_routes":     telemetry.GlobalRouteTracker.InvalidRoutes.Load(),
	}
}

func orchestratorSearchSnapshot() map[string]any {
	wins := telemetry.SearchMetrics.VectorWins.Load()
	return map[string]any{
		"vector_routing_wins":  wins,
		"dynamic_intercepts":   wins, // deprecated alias
		"fallback_invocations": telemetry.SearchMetrics.FallbackInvocations.Load(),
	}
}
