package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

type similarityResult struct {
	ToolA           string
	ToolB           string
	BleveScore      float64
	StructuralScore float64
	TotalScore      float64
	Category        string
	Recommendation  string
}

func extractKeys(prefix string, schema map[string]any) map[string]string {
	keys := make(map[string]string)
	if schema == nil {
		return keys
	}
	for k, v := range schema {
		typeStr := "unknown"
		if vm, ok := v.(map[string]any); ok {
			if t, ok := vm[keyType].(string); ok {
				typeStr = t
			}
			if props, ok := vm[keyProperties].(map[string]any); ok {
				subKeys := extractKeys(prefix+k+".", props)
				maps.Copy(keys, subKeys)
			}
		}
		keys[prefix+k] = typeStr
	}
	return keys
}

func computeJaccard(mapA, mapB map[string]string) float64 {
	if len(mapA) == 0 && len(mapB) == 0 {
		return 1.0
	}
	intersection := 0
	for k, va := range mapA {
		if vb, ok := mapB[k]; ok && va == vb {
			intersection++
		}
	}
	union := len(mapA) + len(mapB) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

type similarityAuditArgs struct {
	targetServers map[string]bool
	artifactPath  string
}

func parseSimilarityAuditArgs(raw json.RawMessage) similarityAuditArgs {
	args := similarityAuditArgs{targetServers: make(map[string]bool)}
	if len(raw) == 0 {
		return args
	}
	var parsed map[string]any
	if json.Unmarshal(raw, &parsed) != nil {
		return args
	}
	if serversArg, ok := parsed["servers"].(string); ok && serversArg != "" {
		for s := range strings.FieldsSeq(serversArg) {
			args.targetServers[s] = true
		}
	}
	if ap, ok := parsed["artifact_path"].(string); ok {
		args.artifactPath = strings.TrimSpace(ap)
	}
	return args
}

func (h *OrchestratorHandler) similarityFindMatches(
	ctx context.Context,
	toolA *db.ToolRecord,
	targetServers map[string]bool,
) []similarityResult {
	if toolA.Description == "" {
		return nil
	}
	matches, err := h.similaritySearchCandidates(ctx, toolA)
	if err != nil {
		slog.Warn("semantic_similarity: search failed", keyURN, toolA.URN, keyError, err)
		return nil
	}
	if len(matches) == 0 {
		return nil
	}
	keysA := extractKeys("", toolA.InputSchema)
	var localResults []similarityResult
	for _, toolB := range matches {
		if toolB.URN == toolA.URN {
			continue
		}
		if len(targetServers) > 0 && !targetServers[toolB.Server] {
			continue
		}
		keysB := extractKeys("", toolB.InputSchema)
		structuralScore := computeJaccard(keysA, keysB)
		if len(keysA) < 2 && len(keysB) < 2 {
			structuralScore = 0.0
		}
		bleveNorm := math.Min(1.0, toolB.ConfidenceScore)
		roi := (bleveNorm * 0.8) + (structuralScore * 0.2)
		if roi <= 0.5 {
			continue
		}
		category := similarityCategory(roi)
		localResults = append(localResults, similarityResult{
			ToolA: toolA.URN, ToolB: toolB.URN,
			BleveScore: toolB.ConfidenceScore, StructuralScore: structuralScore,
			TotalScore: roi, Recommendation: category,
		})
		fmt.Fprintf(os.Stderr, "[semantic_similarity] MATCH FOUND | %s <-> %s | Bleve: %.2f | Struct: %.2f | ROI: %.2f\n",
			toolA.URN, toolB.URN, toolB.ConfidenceScore, structuralScore, roi)
	}
	return localResults
}

func (h *OrchestratorHandler) similaritySearchCandidates(ctx context.Context, toolA *db.ToolRecord) ([]*db.ToolRecord, error) {
	e := vector.GetEngine()
	if e != nil && e.VectorEnabled() {
		scoredNodes, sErr := e.SearchByNode(ctx, toolA.URN, 10)
		if sErr == nil && len(scoredNodes) > 0 {
			var matches []*db.ToolRecord
			for _, node := range scoredNodes {
				if tr, rErr := h.Store.GetTool(node.Key); rErr == nil {
					tr.ConfidenceScore = node.Score
					matches = append(matches, tr)
				}
			}
			return matches, nil
		}
	}
	return h.Store.SearchTools(ctx, toolA.Description, toolA.Category, "", 0.7, h.Config.ScoreFusionAlpha, db.DomainSystem, false)
}

func similarityCategory(roi float64) string {
	switch {
	case roi > 0.9:
		return "Redundant Duplicate: Immediate Refactor"
	case roi >= 0.7:
		return "Functional Overlap: Merge Candidate"
	default:
		return "Shared Domain: Consider Shared Utilities"
	}
}

func consolidateSimilarityMatches(resultsChan <-chan []similarityResult) map[string]similarityResult {
	uniqueMatches := make(map[string]similarityResult)
	for resBatch := range resultsChan {
		for _, res := range resBatch {
			a, b := res.ToolA, res.ToolB
			if a > b {
				a, b = b, a
			}
			key := a + ":::" + b
			if existing, ok := uniqueMatches[key]; ok {
				if res.TotalScore > existing.TotalScore {
					uniqueMatches[key] = res
				}
			} else {
				uniqueMatches[key] = res
			}
		}
	}
	return uniqueMatches
}

func buildSimilarityAuditReport(uniqueMatches map[string]similarityResult) (string, []similarityResult) {
	envelope := map[string]any{
		keyMetadata: map[string]any{"duplicate_count": len(uniqueMatches)},
		"matches":   make([]map[string]any, 0, len(uniqueMatches)),
	}
	if len(uniqueMatches) == 0 {
		envJSON := marshalIndentOrEmpty(envelope)
		return fmt.Sprintf("```json\n%s\n```\n\n# Semantic Similarity Audit\n\n*No overlapping tools found matching the threshold.*", string(envJSON)), nil
	}
	var sorted []similarityResult
	for _, v := range uniqueMatches {
		sorted = append(sorted, v)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].TotalScore > sorted[j].TotalScore })
	for _, res := range sorted {
		matchMap := map[string]any{
			"tool_a": res.ToolA, "tool_b": res.ToolB,
			"similarity": int(res.TotalScore * 100), "recommendation": res.Recommendation,
		}
		envelope["matches"] = append(matchesSliceOrWarn(envelope["matches"]), matchMap)
	}
	envJSON := marshalIndentOrEmpty(envelope)
	var summary strings.Builder
	fmt.Fprintf(&summary, "```json\n%s\n```\n\n", string(envJSON))
	summary.WriteString("# Semantic Similarity Audit\n\n")
	summary.WriteString("| Tool A | Tool B | Similarity | Socratic Recommendation |\n")
	summary.WriteString("| :--- | :--- | :--- | :--- |\n")
	for _, res := range sorted {
		fmt.Fprintf(&summary, "| %s | %s | %d%% | %s |\n", res.ToolA, res.ToolB, int(res.TotalScore*100), res.Recommendation)
	}
	return summary.String(), sorted
}
