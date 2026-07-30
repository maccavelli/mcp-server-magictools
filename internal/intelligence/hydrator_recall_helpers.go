package intelligence

import (
	"context"
	"encoding/json"
	"log/slog"
	"math"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

var recallMinerServers = []string{serverBrainstorm, serverGoModernizer}

func parseRecallEntries(sessionData map[string]any) []any {
	if entries, ok := anySliceFrom(sessionData["entries"]); ok && len(entries) > 0 {
		return entries
	}
	if stages, ok := anySliceFrom(sessionData["stages"]); ok {
		return stages
	}
	return nil
}

func extractDAGFromEntries(entries []any) (dagURNs []string, intent string, ok bool) {
	for _, entryRaw := range entries {
		entry, isMap := mapFrom(entryRaw)
		if !isMap {
			continue
		}
		if entryHasFailureOutcome(entry) {
			return nil, "", false
		}
		stageName := stageFromEntry(entry)
		if stageName == toolStageExecutePipeline {
			intent = intentFromEntry(entry)
		}
		if stageName != "" && stageName != toolStageExecutePipeline && stageName != toolStageGenerateAudit {
			dagURNs = append(dagURNs, stageName)
		}
	}
	return dagURNs, intent, true
}

func stageFromEntry(entry map[string]any) string {
	if content := stringFrom(entry["content"]); content != "" {
		var contentObj map[string]any
		if json.Unmarshal([]byte(content), &contentObj) == nil {
			if stage := stringFrom(contentObj["stage"]); stage != "" {
				return stage
			}
		}
	}
	return traceStageFromTags(entry["tags"])
}

func intentFromEntry(entry map[string]any) string {
	content := stringFrom(entry["content"])
	if content == "" {
		return ""
	}
	var contentObj map[string]any
	if json.Unmarshal([]byte(content), &contentObj) != nil {
		return ""
	}
	return stringFrom(contentObj["intent"])
}

func traceStageFromTags(tagsRaw any) string {
	tags, ok := anySliceFrom(tagsRaw)
	if !ok {
		return ""
	}
	for _, tag := range tags {
		tagStr := stringFrom(tag)
		if after, ok0 := strings.CutPrefix(tagStr, "trace:"); ok0 {
			candidate := after
			if candidate != "auto_publish" && candidate != "async_push" {
				return candidate
			}
		}
	}
	return ""
}

func entryHasFailureOutcome(entry map[string]any) bool {
	tags, ok := anySliceFrom(entry["tags"])
	if !ok {
		return false
	}
	for _, tag := range tags {
		tagStr := stringFrom(tag)
		if tagStr == tagOutcomeError || tagStr == tagOutcomeFailed {
			return true
		}
	}
	return false
}

func parseRecallSessionEnvelope(raw string) []any {
	var envelope map[string]any
	if err := json.Unmarshal([]byte(raw), &envelope); err != nil {
		return nil
	}
	if entries, ok := anySliceFrom(envelope["entries"]); ok {
		return entries
	}
	if data, ok := mapFrom(envelope["data"]); ok {
		if entries, ok := anySliceFrom(data["entries"]); ok {
			return entries
		}
	}
	return nil
}

type recallToolStats struct {
	success int
	total   int
}

func collectRecallCalibrationStats(ctx context.Context, rc RecallMiner) map[string]*recallToolStats {
	stats := make(map[string]*recallToolStats)
	for _, serverID := range recallMinerServers {
		raw := rc.ListSessionsByFilter(ctx, "", serverID, "", 30)
		if raw == "" {
			continue
		}
		mergeRecallSessionStats(stats, serverID, parseRecallSessionEnvelope(raw))
	}
	return stats
}

func mergeRecallSessionStats(stats map[string]*recallToolStats, serverID string, entries []any) {
	for _, entryRaw := range entries {
		entry, ok := mapFrom(entryRaw)
		if !ok {
			continue
		}
		record, ok := mapFrom(entry["record"])
		if !ok {
			continue
		}
		toolURN, isSuccess := toolURNFromRecallRecord(record, serverID)
		if toolURN == "" {
			continue
		}
		if _, exists := stats[toolURN]; !exists {
			stats[toolURN] = &recallToolStats{}
		}
		stats[toolURN].total++
		if isSuccess {
			stats[toolURN].success++
		}
	}
}

func toolURNFromRecallRecord(record map[string]any, serverID string) (toolURN string, isSuccess bool) {
	isSuccess = true
	tags, ok := anySliceFrom(record["tags"])
	if !ok {
		return "", false
	}
	for _, tag := range tags {
		tagStr := stringFrom(tag)
		if after, ok0 := strings.CutPrefix(tagStr, "trace:"); ok0 {
			candidate := after
			if candidate != "auto_publish" && candidate != "async_push" {
				toolURN = serverID + ":" + candidate
			}
		}
		if tagStr == tagOutcomeError || tagStr == tagOutcomeFailed {
			isSuccess = false
		}
	}
	return toolURN, isSuccess
}

func applyRecallCalibration(store *db.Store, stats map[string]*recallToolStats) int {
	var calibrated int
	for urn, s := range stats {
		if s.total < 3 {
			continue
		}
		empiricalRate := float64(s.success) / float64(s.total)
		intel, err := store.GetIntelligence(urn)
		if err != nil || intel == nil {
			continue
		}
		blended := (intel.Metrics.ProxyReliability * 0.6) + (empiricalRate * 0.4)
		blended = math.Max(0.5, math.Min(1.3, blended))
		if blended == intel.Metrics.ProxyReliability {
			continue
		}
		intel.Metrics.ProxyReliability = blended
		if err := store.SaveIntelligence(urn, intel); err != nil {
			slog.Warn("recall_calibration: failed to save intelligence", "urn", urn, "error", err)
			continue
		}
		calibrated++
	}
	return calibrated
}

func mineServerRecallPatterns(ctx context.Context, rc RecallMiner, store *db.Store, serverID string) int {
	sessionData, err := rc.AggregateSessionFromRecall(ctx, serverID, "global")
	if err != nil {
		slog.Log(ctx, util.LevelTrace, "recall_miner: no sessions for server", "server", serverID, "error", err)
		return 0
	}
	entries := parseRecallEntries(sessionData)
	if len(entries) == 0 {
		return 0
	}
	dagURNs, intent, ok := extractDAGFromEntries(entries)
	if !ok || len(dagURNs) < 2 || intent == "" {
		return 0
	}
	if err := store.Index.IndexSyntheticIntent(intent, dagURNs); err != nil {
		slog.Warn("recall_miner: failed to index empirical intent", "intent", intent, "error", err)
		return 0
	}
	return 1
}
