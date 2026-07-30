package handler

import (
	"context"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func (h *OrchestratorHandler) freshLoadRecallContext(ctx context.Context, target, intent string) strings.Builder {
	var recallContext strings.Builder
	if h.RecallClient == nil || !h.RecallClient.RecallEnabled() {
		return recallContext
	}
	if history := h.RecallClient.ListSessionsByFilter(ctx, target, "", "", 5); history != "" {
		recallContext.WriteString("## Project History (from Recall)\n")
		recallContext.WriteString(history + "\n\n")
	}
	standardsQuery := intent + " best practices code quality"
	if standards := h.RecallClient.SearchStandards(ctx, standardsQuery, "", "", 10); standards != "" {
		recallContext.WriteString("## Applicable Standards (from Recall)\n")
		recallContext.WriteString(standards + "\n\n")
	}
	return recallContext
}

func (h *OrchestratorHandler) freshGatherPipelineRecords(ctx context.Context, intent string) []*db.ToolRecord {
	normalizedIntent := strings.ToLower(strings.TrimSpace(intent))
	const preFilterThreshold = 0.05
	rawRecords := searchToolsOrEmpty(ctx, h.Store, normalizedIntent, "", "", preFilterThreshold, h.Config.ScoreFusionAlpha, db.DomainPipelineOrchestration, false)

	pipelineRecords := make([]*db.ToolRecord, 0, len(rawRecords))
	seen := make(map[string]bool)
	for _, r := range rawRecords {
		if r == nil || seen[r.URN] {
			continue
		}
		if r.URN == urnGoModernizerGoTestValidation {
			continue
		}
		if r.Server == serverMagictools && r.URN != urnMagictoolsGenerateAuditReport {
			continue
		}
		pipelineRecords = append(pipelineRecords, r)
		seen[r.URN] = true
	}
	structuralURNs := []string{
		urnBrainstormDiscoverProject, urnBrainstormThesisArchitect, urnBrainstormAntithesisSkeptic,
		urnBrainstormAporiaEngine, urnGoModernizerGoASTSuiteAnalyzer, "go-modernizer:suggest_fixes",
		urnGoModernizerGenerateImplPlan, "brainstorm:architectural_diagrammer", urnBrainstormGenerateFinalReport,
		"brainstorm:brainstorm_complexity_forecaster", "brainstorm:analyze_evolution", "brainstorm:critique_design",
		"brainstorm:peer_review", "brainstorm:brainstorm_ast_probe", "go-modernizer:go_memory_analyzer",
		"go-modernizer:go_context_analyzer", "go-modernizer:go_dead_code_pruner",
	}
	for _, urn := range structuralURNs {
		if seen[urn] {
			continue
		}
		if rec, err := h.Store.GetTool(urn); err == nil && rec != nil {
			pipelineRecords = append(pipelineRecords, rec)
			seen[urn] = true
		}
	}
	return pipelineRecords
}

func freshAnalysisRoles(targetRoles []string) []string {
	analysisRoles := targetRoles
	if len(analysisRoles) == 0 {
		analysisRoles = []string{roleAnalyzer, roleCritic, roleSynthesizer, rolePlanner, roleReporting, roleThreat}
	}
	return filterOutRole(analysisRoles, roleMutator)
}

func freshCapStages(intent string, stages []PipelineStep) []PipelineStep {
	scope := classifyScope(intent)
	maxTools := 8
	switch scope {
	case "narrow":
		maxTools = 4
	case "broad":
		maxTools = 24
	}
	if len(stages) > maxTools {
		return smartCap(stages, maxTools)
	}
	return stages
}
