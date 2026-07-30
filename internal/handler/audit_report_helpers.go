package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

type auditTraceEntry struct {
	stageName string
	outcome   string
	summary   string
}

func auditExtractStageFromContent(contentStr string) (stageName, summary string) {
	var contentObj map[string]any
	if json.Unmarshal([]byte(contentStr), &contentObj) != nil {
		return "", ""
	}
	if s, ok := contentObj["stage"].(string); ok {
		stageName = s
	}
	if s, ok := contentObj["summary"].(string); ok && s != "" {
		summary = s
	} else if s, ok := contentObj["narrative"].(string); ok && s != "" {
		summary = s
	}
	if summary == "" {
		summary = auditSummaryFromTraceData(contentObj)
	}
	return stageName, summary
}

func auditSummaryFromTraceData(contentObj map[string]any) string {
	traceData, ok := contentObj["trace_data"].(map[string]any)
	if !ok {
		return ""
	}
	if diags, ok := traceData["diagnostics"].([]any); ok && len(diags) > 0 {
		firstDiag := fmt.Sprintf("%v", diags[0])
		if idx := strings.Index(firstDiag, "\n"); idx > 0 {
			firstDiag = firstDiag[:idx]
		}
		return firstDiag
	}
	if pm, ok := traceData["pillar_metrics"].(map[string]any); ok {
		if pillar := stringFrom(pm["pillar"]); pillar != "" {
			return fmt.Sprintf("Pillar: %s", pillar)
		}
	}
	return ""
}

func auditStageFromTags(tags []any) string {
	for _, tag := range tags {
		tagStr := stringFrom(tag)
		if after, ok := strings.CutPrefix(tagStr, "trace:"); ok {
			if after != "auto_publish" && after != "async_push" {
				return after
			}
		}
	}
	return ""
}

func auditOutcomeFromTags(tags []any) string {
	outcome := statusCompleted
	for _, tag := range tags {
		tagStr := stringFrom(tag)
		if after, ok := strings.CutPrefix(tagStr, "outcome:"); ok {
			switch after {
			case "idle", "injection_scanned", "saved":
				outcome = statusCompleted
			case keyError, "failed":
				outcome = statusBlocked
			default:
				outcome = strings.ToUpper(after)
			}
		}
	}
	return outcome
}

func auditShouldSkipStage(stageName string) bool {
	return stageName == "" || stageName == toolGenerateAuditReport ||
		stageName == toolExecutePipeline || stageName == "auto_publish" || stageName == "async_push"
}

func auditExtractTracesFromSession(sessionData map[string]any) []auditTraceEntry {
	entries, ok := sessionData["entries"].([]any)
	if !ok {
		return nil
	}
	var traces []auditTraceEntry
	for _, entryRaw := range entries {
		entry, ok := entryRaw.(map[string]any)
		if !ok {
			continue
		}
		record, ok := entry["record"].(map[string]any)
		if !ok {
			continue
		}
		stageName, summary := "", ""
		if contentStr, isStr := record["content"].(string); isStr {
			stageName, summary = auditExtractStageFromContent(contentStr)
		}
		if stageName == "" {
			if tags, ok := record["tags"].([]any); ok {
				stageName = auditStageFromTags(tags)
			}
		}
		if auditShouldSkipStage(stageName) {
			continue
		}
		if summary == "" {
			summary = "Structural trace executed successfully."
		}
		outcome := statusCompleted
		if tags, ok := record["tags"].([]any); ok {
			outcome = auditOutcomeFromTags(tags)
		}
		traces = append(traces, auditTraceEntry{stageName: stageName, outcome: outcome, summary: summary})
	}
	return auditDeduplicateTraces(traces)
}

func auditDeduplicateTraces(traces []auditTraceEntry) []auditTraceEntry {
	seen := make(map[string]int)
	var deduped []auditTraceEntry
	for _, t := range traces {
		if idx, exists := seen[t.stageName]; exists {
			deduped[idx] = t
		} else {
			seen[t.stageName] = len(deduped)
			deduped = append(deduped, t)
		}
	}
	return deduped
}

func auditWriteExecutionTraces(sb *strings.Builder, traces []auditTraceEntry) bool {
	if len(traces) == 0 {
		return false
	}
	for i, t := range traces {
		fmt.Fprintf(sb, "%d. **`%s`** - *%s*: %v\n", i+1, t.stageName, t.outcome, t.summary)
	}
	return true
}

func (h *OrchestratorHandler) auditRecordEdgeLearning(ctx context.Context, sessionID string, metricsFound bool) {
	if h.Store == nil || !metricsFound || h.RecallClient == nil || !h.RecallClient.RecallEnabled() {
		return
	}
	sessionData, err := h.RecallClient.GetSession(ctx, sessionID)
	if err != nil {
		return
	}
	hasAporia, aporiaFailed, sessionIntent, dagURNs := auditScanChronologicalDAG(sessionData)
	if len(dagURNs) <= 1 {
		return
	}
	slog.Info("edge_learning: recording transition weights", "aporia_triggered", hasAporia, "dag_size", len(dagURNs), "dag", dagURNs)
	for i := 0; i < len(dagURNs)-1; i++ {
		transitionHash := fmt.Sprintf("%x", sha256.Sum256([]byte(dagURNs[i]+"->"+dagURNs[i+1])))
		h.Store.RecordSynergy(transitionHash, !aporiaFailed || !hasAporia)
	}
	if (!aporiaFailed || !hasAporia) && sessionIntent != "" {
		go func() {
			if err := h.Store.Index.IndexSyntheticIntent(sessionIntent, dagURNs); err != nil {
				slog.Error("edge_learning: GhostIndex intent registration failed", keyError, err)
			}
		}()
	}
}

func auditScanChronologicalDAG(sessionData map[string]any) (hasAporia, aporiaFailed bool, sessionIntent string, dagURNs []string) {
	entries, ok := sessionData["entries"].([]any)
	if !ok {
		return false, false, "", nil
	}
	for _, entryRaw := range entries {
		entry := mapFrom(entryRaw)
		record := mapFrom(entry["record"])
		stageName, payload := auditParseEntryPayload(record)
		if payload == nil {
			continue
		}
		if stageName == toolExecutePipeline {
			if iVal, ok := payload["intent"].(string); ok {
				sessionIntent = iVal
			} else if dVal, ok := payload["description"].(string); ok {
				sessionIntent = dVal
			}
		}
		if stageName != "" && stageName != toolGenerateAuditReport && stageName != toolExecutePipeline {
			dagURNs = append(dagURNs, stageName)
		}
		if stageName == "aporia_engine" {
			hasAporia = true
			if errRaw, hasErr := payload[keyError]; hasErr && errRaw != nil && errRaw != "" {
				aporiaFailed = true
			}
		}
	}
	return hasAporia, aporiaFailed, sessionIntent, dagURNs
}

func auditParseEntryPayload(record map[string]any) (string, map[string]any) {
	var payload map[string]any
	var stageName string
	switch content := record["content"].(type) {
	case string:
		if json.Unmarshal([]byte(content), &payload) == nil {
			stageName = stringFrom(payload["stage"])
		}
	case map[string]any:
		payload = content
		stageName = stringFrom(payload["stage"])
	}
	return stageName, payload
}

func auditFanoutArtifact(content, sessionID string) {
	if sessionID == "" {
		return
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return
	}
	basePath := filepath.Join(homeDir, ".gemini", "antigravity", "brain", sessionID)
	mkdirAllOrWarn(basePath, 0o750) //nolint:gosec // G301: artifact dir under user home
	mdPath := filepath.Join(basePath, "walkthrough.md")
	writeFileOrWarn(mdPath, []byte(content), 0o600) //nolint:gosec // G306: user-local artifact
	jsonPath := mdPath + ".metadata.json"
	metaContent := `{"artifactType": "ARTIFACT_TYPE_WALKTHROUGH", "summary": "Walkthrough report automatically surfaced from pipeline telemetry.", "requestFeedback": false, "isArtifact": true}`
	writeFileOrWarn(jsonPath, []byte(metaContent), 0644)
}

func auditCloseDAGTerminal() {
	telemetry.GlobalDAGTracker.ClosePipeline(statusCompleted)
}
