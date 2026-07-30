package main

import "strings"

func searchTelemetryEmptyReason(snapshot map[string]any) string {
	searchRaw, ok := snapshot["search"].(map[string]any)
	if !ok || len(searchRaw) == 0 {
		return "No search telemetry in snapshot yet — ensure serve is running and wait for the next health monitor tick"
	}
	if numI64(searchRaw, "total_searches") == 0 {
		return "No align_tools searches this session — invoke align_tools to populate metrics"
	}
	mode := str(searchRaw, "mode")
	if strings.Contains(mode, "Lexical") {
		return "Vector leg disabled — HNSW metrics and RAG confidence require vector search enabled"
	}
	return ""
}

func indexMatrixEmptyReason(snapshot map[string]any) string {
	if reason := searchTelemetryEmptyReason(snapshot); reason != "" {
		return reason
	}
	searchRaw := mapFrom(snapshot["search"])
	bleveTop, bOk := searchRaw["bleve_top_5"].([]any)
	hnswTop, hOk := searchRaw["hnsw_top_5"].([]any)
	if (bOk && len(bleveTop) > 0) || (hOk && len(hnswTop) > 0) {
		return ""
	}
	l1Hits := numI64(searchRaw, "l1_cache_hits")
	if l1Hits > 0 && numI64(searchRaw, "l1_cache_misses") == 0 {
		return "Only L1 cache hits so far — run a new align_tools query to refresh fusion matrix"
	}
	return "No fusion top-5 captured yet — URN/internal fast paths skip SearchTools fusion"
}
