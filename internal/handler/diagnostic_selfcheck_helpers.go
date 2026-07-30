package handler

import (
	"context"
	"encoding/json"
	"math"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

func selfCheckVectorPayload() map[string]any {
	e := vector.GetEngine()
	if e == nil {
		return nil
	}
	lastTrace := telemetry.SearchMetrics.LastQueryTrace.Load()
	lastFusionWinner := ""
	if lastTrace != nil {
		lastFusionWinner = lastTrace.FusionWinner
	}
	return map[string]any{
		"enabled":                e.VectorEnabled(),
		"graph_nodes":            e.Len(),
		"needs_hydration":        e.RequiresHydration(),
		"vector_wins":            telemetry.SearchMetrics.VectorWins.Load(),
		"lexical_wins":           telemetry.SearchMetrics.LexicalWins.Load(),
		"total_searches":         telemetry.SearchMetrics.TotalSearches.Load(),
		"vector_searches":        telemetry.SearchMetrics.VectorSearches.Load(),
		"vector_search_attempts": telemetry.SearchMetrics.VectorSearchAttempts.Load(),
		"vector_search_errors":   telemetry.SearchMetrics.VectorSearchErrors.Load(),
		"gate_rejections":        telemetry.SearchMetrics.GateRejections.Load(),
		"fallback_invocations":   telemetry.SearchMetrics.FallbackInvocations.Load(),
		"fallback_rescues":       telemetry.SearchMetrics.FallbackRescues.Load(),
		"last_fusion_winner":     lastFusionWinner,
		"cache_hits":             telemetry.SearchMetrics.CacheHits.Load(),
		"cache_misses":           telemetry.SearchMetrics.CacheMisses.Load(),
		"vector_stale_skips":     telemetry.SearchMetrics.VectorStaleSkips.Load(),
		"embed_latency_ms":       telemetry.SearchMetrics.EmbedLatencyMs.Load(),
		"query_embed_latency_ms": telemetry.SearchMetrics.QueryEmbedLatencyMs.Load(),
		"graph_completeness":     math.Float64frombits(telemetry.SearchMetrics.GraphCompletenessRatio.Load()),
	}
}

func selfCheckRecallPayload(ctx context.Context, h *OrchestratorHandler) map[string]any {
	if h == nil || h.RecallClient == nil {
		return nil
	}
	recallStatus := map[string]any{
		"connected": h.RecallClient.RecallEnabled(),
	}
	if !h.RecallClient.RecallEnabled() {
		return recallStatus
	}
	raw := h.RecallClient.CallDatabaseTool(ctx, "list", map[string]any{
		keyNamespace: namespaceServerStatus,
		keyServerID:  serverMagictools,
		keyLimit:     defaultRecallLimit,
	})
	lastSnapshotAvailable := false
	if raw != "" {
		var envelope map[string]any
		if json.Unmarshal([]byte(raw), &envelope) == nil {
			var entries []any
			if e, ok := envelope["entries"].([]any); ok {
				entries = e
			} else if data, ok := envelope["data"].(map[string]any); ok {
				entries = sliceAnyOrWarn(data["entries"])
			}
			for _, entryRaw := range entries {
				entry, ok := entryRaw.(map[string]any)
				if !ok {
					continue
				}
				rec, ok := entry["record"].(map[string]any)
				if !ok {
					continue
				}
				if rec[keySessionID] == "magictools-diagnostics" {
					lastSnapshotAvailable = true
					break
				}
			}
		}
	}
	recallStatus["last_snapshot_available"] = lastSnapshotAvailable
	return recallStatus
}

func selfCheckArgumentRepairsPayload() map[string]any {
	return map[string]any{
		"double_encoded":      telemetry.ArgumentRepairs.DoubleEncoded.Load(),
		repairTypeXMLStripped: telemetry.ArgumentRepairs.XMLStripped.Load(),
		"flat_structure":      telemetry.ArgumentRepairs.FlatStructure.Load(),
		"trailing_comma":      telemetry.ArgumentRepairs.TrailingComma.Load(),
		"nested_unwrap":       telemetry.ArgumentRepairs.NestedUnwrap.Load(),
		"heuristic":           telemetry.ArgumentRepairs.Heuristic.Load(),
		"total_attempts":      telemetry.ArgumentRepairs.TotalAttempts.Load(),
		"total_failures":      telemetry.ArgumentRepairs.TotalFailures.Load(),
	}
}
