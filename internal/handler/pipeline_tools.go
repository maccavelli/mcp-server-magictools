package handler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/dag"
	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// Phase 3: Project Manager Pipeline Tools
// ---------------------------------------------------------------------------
// These tools are ONLY available when activePipelineEnabled is true
// (recall + brainstorm + go-modernizer all online).

// pipelineGate checks if the active pipeline is enabled.
// Returns an error result if the pipeline is disabled.
func (h *OrchestratorHandler) pipelineGate() (*mcp.CallToolResult, bool) {
	if h.PipelineEnabled == nil || !h.PipelineEnabled.Load() {
		res := &mcp.CallToolResult{}
		res.Content = []mcp.Content{
			&mcp.TextContent{Text: "pipeline_disabled: Active code generation pipeline is offline. Required servers (recall, brainstorm, go-modernizer) are not all available."},
		}
		return res, false
	}
	return nil, true
}

// RegisterPipelineTools registers the PM tools on the MCP server.
func (h *OrchestratorHandler) RegisterPipelineTools(s *mcp.Server) {
	h.addTool(s, &mcp.Tool{Name: toolExecutePipeline}, h.handleExecutePipeline)
	h.addTool(s, &mcp.Tool{Name: toolValidatePipelineStep}, h.handleValidatePipelineStep)
	h.addTool(s, &mcp.Tool{Name: "cross_server_quality_gate"}, h.handleQualityGate)
	h.addTool(s, &mcp.Tool{Name: toolGenerateAuditReport}, h.handleGenerateAuditReport)

	slog.Info("pipeline tools registered", "component", "pipeline", "count", 4)
}

// handleComposePipeline has been merged into handleExecutePipeline.
// Use execute_pipeline with dry_run=true for plan preview.

// PipelineStep represents a single recommended stage natively mapping tight Go bounds.
type PipelineStep struct {
	ToolName       string         `json:"name"`
	Role           string         `json:"role"`
	Phase          int            `json:"phase"`
	Purpose        string         `json:"purpose"`
	Args           map[string]any `json:"args,omitempty,omitzero"`
	InputContract  string         `json:"input_contract,omitempty,omitzero"`
	OutputContract string         `json:"output_contract,omitempty,omitzero"`
}

// scoredTool pairs a tool record with its computed pipeline score.
type scoredTool struct {
	record     *db.ToolRecord
	finalScore float64
}

// executeSwarmBidding uses intent-weighted tri-factor scoring with phase sequencing
// to compose a pipeline DAG from go-modernizer and brainstorm tools only.
//
// Scoring formula: S_final = ((S_engine × wEngine) + (S_synergy × wSynergy) + (R_role × wRole) + (S_intent × wIntent)) × failurePenalty
// where S_engine = (α × cosine) + ((1-α) × bm25) for hybrid mode, or pure bm25 for offline mode.
// S_intent is the Option 3 intent→tool outcome score from real-time synergy tracking.
// failurePenalty is the Option 6 contrastive failure anchor proximity multiplier.
func (h *OrchestratorHandler) executeSwarmBidding(ctx context.Context, intent string, targetRoles []string, pipelineTools []*db.ToolRecord) ([]PipelineStep, []string) {
	candidates, engineScores, engineType := h.swarmScoreAllCandidates(ctx, intent, targetRoles, pipelineTools)
	roleMap := swarmBuildRoleMap(targetRoles)
	threshold := swarmComputeMADThreshold(candidates)

	qualified, hasMutator := swarmQualifyCandidates(candidates, threshold)
	qualified = swarmPreserveCriticalRoles(candidates, qualified, roleMap, threshold)
	qualified = swarmFillPhaseCoverageGaps(candidates, qualified, threshold)
	qualified = swarmInjectMutatorCritics(candidates, qualified, threshold, hasMutator)

	sort.SliceStable(qualified, func(i, j int) bool {
		if qualified[i].record.Phase != qualified[j].record.Phase {
			return qualified[i].record.Phase < qualified[j].record.Phase
		}
		return qualified[i].finalScore > qualified[j].finalScore
	})

	swarmApplyEdgeBoost(h, qualified)
	qualified = prunePhaseRoleClusters(qualified, 3)
	stages := swarmComposeStages(qualified, engineType, engineScores)
	stages = resolveDynamicDAG(stages, pipelineTools, intent)
	stages, warnings := topologicalSort(stages, pipelineTools)
	stages = enforceExclusivityEnclaves(stages)
	return stages, warnings
}

// resolveDynamicDAG continuously walks the explicit Requires/Triggers constraints provided by local MCP Sub-Servers.
// Recursively extracts properties ensuring no dependency limit violates pure topology limits physically tracking graph logic dynamically.
func resolveDynamicDAG(stages []PipelineStep, pipelineTools []*db.ToolRecord, intent string) []PipelineStep {
	registry := dagBuildRegistry(pipelineTools)
	stages = dagInjectTrifecta(stages, registry, intent)
	stages = dagInjectMandatoryPlanner(stages, registry, intent)
	stages = dagInjectMandatoryReporting(stages, registry)
	return dagExpandDependencyLoop(stages, pipelineTools)
}

// topologicalSort implements strict DFS Topological bounding organically tracing Acyclic structures ensuring formal Requires constraints are mapped mathematically safely bypassing SliceStable index faults natively.
func topologicalSort(stages []PipelineStep, pipelineTools []*db.ToolRecord) ([]PipelineStep, []string) {
	registry := make(map[string]*db.ToolRecord)
	for _, t := range pipelineTools {
		registry[t.URN] = t
	}

	var warnings []string

	// Fallback Phase precedence sort structurally natively avoiding timeline jitter iteratively.
	sort.SliceStable(stages, func(i, j int) bool {
		return stages[i].Phase < stages[j].Phase
	})

	adj := make(map[string][]string)
	nodes := make(map[string]PipelineStep)
	var orderedNames []string

	for _, s := range stages {
		nodes[s.ToolName] = s
		orderedNames = append(orderedNames, s.ToolName)
	}

	for _, s := range stages {
		if rec, exists := registry[s.ToolName]; exists {
			for _, requiredURN := range rec.Requires {
				if _, ok := nodes[requiredURN]; ok {
					adj[requiredURN] = append(adj[requiredURN], s.ToolName) // requiredURN -> s.ToolName
				}
			}
			for _, triggerURN := range rec.Triggers {
				if _, ok := nodes[triggerURN]; ok {
					adj[s.ToolName] = append(adj[s.ToolName], triggerURN) // s.ToolName -> triggerURN
				}
			}
		}
	}

	state := make(map[string]int) // 0=Unvisited, 1=Visiting, 2=Visited
	var resultNames []string

	var dfs func(urn string) bool
	dfs = func(urn string) bool {
		if state[urn] == 1 {
			return true // Cycle detected
		}
		if state[urn] == 2 {
			return false
		}

		state[urn] = 1
		for _, neighbor := range adj[urn] {
			if dfs(neighbor) {
				warnings = append(warnings, fmt.Sprintf("⚠️ Semantic Gatekeeper Warning: Circular Dependency Detected connecting `%s` natively.", neighbor))
			}
		}
		state[urn] = 2
		resultNames = append([]string{urn}, resultNames...)
		return false
	}

	// Traverse roots explicitly preserving internal chronological boundaries structurally cleanly.
	inDegree := make(map[string]int)
	for _, neighbors := range adj {
		for _, n := range neighbors {
			inDegree[n]++
		}
	}

	for i := len(orderedNames) - 1; i >= 0; i-- {
		urn := orderedNames[i]
		if inDegree[urn] == 0 && state[urn] == 0 {
			dfs(urn)
		}
	}

	// Sweep disjoint components actively.
	for i := len(orderedNames) - 1; i >= 0; i-- {
		urn := orderedNames[i]
		if state[urn] == 0 {
			dfs(urn)
		}
	}

	var finalStages []PipelineStep
	for _, urn := range resultNames {
		finalStages = append(finalStages, nodes[urn])
	}

	return finalStages, warnings
}

func getOfflineBM25Scores(intent string, pipelineTools []*db.ToolRecord) map[string]float64 {
	pruned := pruneIntent(intent)
	return dag.ScoreGroupedByServer(pruned, pipelineTools)
}

// pruneIntent mathematically strips grammatical buzzwords strictly guaranteeing dense token scoring matching natively structurally.
func pruneIntent(text string) string {
	stopWords := []string{"i", "need", "to", "the", "a", "an", "and", "or", "in", "on", "for", "with", "this", "that", "it", "of", "by", "as", "is", "are"}
	words := strings.Fields(strings.ToLower(text))
	var result []string

	stopMap := make(map[string]bool)
	for _, w := range stopWords {
		stopMap[w] = true
	}

	for _, w := range words {
		if !stopMap[w] {
			result = append(result, w)
		}
	}
	return strings.Join(result, " ")
}

// enforceExclusivityEnclaves structurally removes dynamic DAG looping nodes gracefully filtering array density.
func enforceExclusivityEnclaves(stages []PipelineStep) []PipelineStep {
	var finalStages []PipelineStep
	seen := make(map[string]bool)

	for _, s := range stages {
		// Prevent duplicates natively guaranteeing linear purity locally
		if seen[s.ToolName] {
			continue
		}
		seen[s.ToolName] = true
		finalStages = append(finalStages, s)
	}
	return finalStages
}

// prunePhaseRoleClusters caps the number of tools per (phase, role) pair.
// The input must already be sorted by phase ascending, score descending.
// This prevents pathological clustering (e.g., 7 CRITICs in Phase 4)
// while keeping the highest-scoring tools from each group.
func prunePhaseRoleClusters(tools []scoredTool, maxPerGroup int) []scoredTool {
	type phaseRole struct {
		phase int
		role  string
	}
	counts := make(map[phaseRole]int)
	var result []scoredTool

	for _, t := range tools {
		key := phaseRole{phase: t.record.Phase, role: t.record.Role}
		if counts[key] >= maxPerGroup {
			continue
		}
		counts[key]++
		result = append(result, t)
	}
	return result
}

// classifyIntentWeights determines proportional intent category weights from
// the user's request. Instead of a single winner, returns normalized weights
// across all matching categories. This eliminates cliff-edge instability where
// "evaluate and refactor" vs "refactor and evaluate" would flip the entire
// role boost table.
func classifyIntentWeights(intent string) map[string]float64 {
	lower := strings.ToLower(intent)

	auditSignals := []string{signalAudit, "analyze", "review", "inspect", "assess", "evaluate", "check", "scan", "trace"}
	refactorSignals := []string{signalRefactor, signalFix, signalModernize, "optimize", "clean", "prune", "migrate", "upgrade"}
	planSignals := []string{signalPlan, "design", "architect", "propose", "strategy", "blueprint", "feature"}

	weights := map[string]float64{signalAudit: 0, signalRefactor: 0, signalPlan: 0}
	for _, s := range auditSignals {
		if strings.Contains(lower, s) {
			weights[signalAudit]++
		}
	}
	for _, s := range refactorSignals {
		if strings.Contains(lower, s) {
			weights["refactor"]++
		}
	}
	for _, s := range planSignals {
		if strings.Contains(lower, s) {
			weights[signalPlan]++
		}
	}

	// Normalize to proportions [0,1]
	total := weights[signalAudit] + weights["refactor"] + weights[signalPlan]
	if total > 0 {
		for k := range weights {
			weights[k] /= total
		}
	} else {
		// No signals matched — default to audit (analysis-only)
		weights[signalAudit] = 1.0
	}

	return weights
}

// computeRoleBoostBlended returns a weighted role boost blended across all
// matched intent categories. For "evaluate and refactor" (audit=0.5, refactor=0.5):
//
//	ANALYZER = (1.0 × 0.5) + (0.75 × 0.5) = 0.875
//	CRITIC   = (0.7 × 0.5) + (0.5 × 0.5)  = 0.60
//	MUTATOR  = (0.2 × 0.5) + (1.0 × 0.5)  = 0.60
func computeRoleBoostBlended(role string, weights map[string]float64) float64 {
	boost := 0.0
	for intentType, weight := range weights {
		boost += lookupRoleBoost(role, intentType) * weight
	}
	return boost
}

// lookupRoleBoost returns the single-category role boost value.
// This is the lookup table used by computeRoleBoostBlended.
func lookupRoleBoost(role, intentType string) float64 {
	switch intentType {
	case signalAudit:
		switch role {
		case roleAnalyzer:
			return 1.0
		case roleCritic:
			return 0.7
		case roleSynthesizer:
			return 0.4
		case rolePlanner:
			return 0.3
		case roleMutator:
			return 0.2
		}
	case "refactor":
		switch role {
		case roleMutator:
			return 1.0
		case roleAnalyzer:
			return 0.75
		case rolePlanner:
			return 0.7
		case roleCritic:
			return 0.5
		case roleSynthesizer:
			return 0.25
		}
	case signalPlan:
		switch role {
		case roleCritic, roleSynthesizer, rolePlanner:
			return 1.0
		case roleAnalyzer:
			return 0.5
		case roleMutator:
			return 0.25
		}
	}
	return 0.0
}

// validateDAGSemantics checks the composed pipeline DAG for structural
// anti-patterns that would produce invalid execution orderings natively.
// Returns a list of human-readable warnings (empty = clean DAG).
func validateDAGSemantics(stages []PipelineStep) []string {
	roleIdx := dagFirstRoleIndices(stages)
	warnings := dagGrammarWarnings(roleIdx)
	warnings = append(warnings, dagContractWarnings(stages)...)

	// 🛡️ REDUNDANCY GOVERNOR: Warn if 3+ consecutive stages share the same Role.
	// This detects pathological DAGs (e.g., three analyzers without an intervening mutator)
	// that waste agent execution cycles without advancing the pipeline.
	consecutiveCount := 1
	for i := 1; i < len(stages); i++ {
		if stages[i].Role != "" && stages[i].Role == stages[i-1].Role {
			consecutiveCount++
			if consecutiveCount >= 3 {
				warnings = append(warnings, fmt.Sprintf(
					"Redundancy Warning: %d consecutive %s-role tools (%s..%s) — consider interleaving a different role.",
					consecutiveCount, stages[i].Role, stages[i-consecutiveCount+1].ToolName, stages[i].ToolName))
			}
		} else {
			consecutiveCount = 1
		}
	}

	return warnings
}

// ---------------------------------------------------------------------------
// validate_pipeline_step handler
// ---------------------------------------------------------------------------

func (h *OrchestratorHandler) handleValidatePipelineStep(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if res, ok := h.pipelineGate(); !ok {
		return res, nil
	}

	var args struct {
		StepName    string `json:"step_name"`
		StepOutput  string `json:"step_output"`
		ProjectPath string `json:"project_path"`
	}
	unmarshalArgsOrWarn(req.Params.Arguments, &args)

	// Query relevant standards for this specific tool.
	var standards string
	if h.RecallClient != nil && h.RecallClient.RecallEnabled() {
		standards = h.RecallClient.SearchStandards(ctx, fmt.Sprintf("%s validation quality criteria", args.StepName), "", "", 5)
	}

	// Validate step output.
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Step Validation: %s\n\n", args.StepName)

	// Check for error indicators in output.
	verdict := "PASS"
	var issues []string

	outputLower := strings.ToLower(args.StepOutput)
	if strings.Contains(outputLower, keyError) || strings.Contains(outputLower, "fatal") {
		issues = append(issues, "Output contains error indicators")
		verdict = "NEEDS_REVIEW"
	}
	if strings.Contains(outputLower, "panic") || strings.Contains(outputLower, "crash") {
		issues = append(issues, "Output contains critical failure indicators")
		verdict = "FAIL"
	}
	if len(args.StepOutput) < 50 {
		issues = append(issues, "Output is suspiciously short — may indicate tool failure")
		verdict = "NEEDS_REVIEW"
	}

	fmt.Fprintf(&sb, "**Verdict**: %s\n\n", verdict)

	if len(issues) > 0 {
		sb.WriteString("## Issues Detected\n")
		for _, issue := range issues {
			fmt.Fprintf(&sb, "- %s\n", issue)
		}
		sb.WriteString("\n")
	}

	if standards != "" {
		sb.WriteString("## Applicable Standards\n")
		sb.WriteString(standards)
		sb.WriteString("\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}, nil
}

// ---------------------------------------------------------------------------
// cross_server_quality_gate handler
// ---------------------------------------------------------------------------

func (h *OrchestratorHandler) handleQualityGate(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if res, ok := h.pipelineGate(); !ok {
		return res, nil
	}

	var args struct {
		ProjectPath string `json:"project_path"`
		PlanHash    string `json:"plan_hash"`
	}
	unmarshalArgsOrWarn(req.Params.Arguments, &args)

	var sb strings.Builder
	sb.WriteString("# Cross-Server Quality Gate\n\n")
	fmt.Fprintf(&sb, "**Project**: %s\n", args.ProjectPath)
	fmt.Fprintf(&sb, "**Plan Hash**: %s\n\n", args.PlanHash)

	checks := make(map[string]string)

	// Check 1: Recall — verify standards compliance.
	if h.RecallClient != nil && h.RecallClient.RecallEnabled() {
		cats := h.RecallClient.ListStandardsCategories(ctx, "")
		if cats != "" {
			checks["recall_standards"] = "✅ Standards database accessible"
		} else {
			checks["recall_standards"] = "⚠️ Standards database empty or unreachable"
		}

		// Check for approval in sessions.
		approved, err := h.RecallClient.CheckApprovalExists(ctx, args.ProjectPath, args.PlanHash)
		if err != nil {
			checks["brainstorm_approval"] = fmt.Sprintf("⚠️ Approval check failed: %v", err)
		} else if approved {
			checks["brainstorm_approval"] = "✅ Plan approved by brainstorm vetting"
		} else {
			checks["brainstorm_approval"] = "❌ No approval found for this plan_hash"
		}

		// Check for go-modernizer analysis completion.
		refactorHistory := h.RecallClient.ListSessionsByFilter(ctx, args.ProjectPath, "go-modernizer", "completed", 5)
		if refactorHistory != "" {
			checks["go-modernizer_analysis"] = "✅ Go-refactor analysis completed"
		} else {
			checks["go-modernizer_analysis"] = "⚠️ No completed go-modernizer analysis found"
		}
	} else {
		checks["recall_connection"] = "❌ Recall client not available"
	}

	// Determine overall gate verdict.
	sb.WriteString("## Quality Checks\n\n")
	gatePass := true
	for check, status := range checks {
		fmt.Fprintf(&sb, "- **%s**: %s\n", check, status)
		if strings.HasPrefix(status, "❌") {
			gatePass = false
		}
	}

	sb.WriteString("\n## Gate Verdict\n\n")
	if gatePass {
		sb.WriteString("**✅ GATE PASSED** — All quality checks satisfied. Safe to proceed with `apply_vetted_edit`.\n")
	} else {
		sb.WriteString("**❌ GATE FAILED** — One or more quality checks failed. Do NOT proceed with filesystem writes.\n")
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}, nil
}

// trifectaURNs lists the Socratic Trifecta members that must never be
// amputated by the scope cap.
var trifectaURNs = map[string]bool{
	urnBrainstormThesisArchitect:   true,
	urnBrainstormAntithesisSkeptic: true,
	urnBrainstormAporiaEngine:      true,
}

// smartCap applies a role-aware cap that protects structural guarantees:
//  1. Sole representatives of a role are unconditionally preserved.
//  2. Trifecta members are unconditionally preserved.
//  3. Excess is trimmed from the most over-represented role first (by count).
//
// This replaces the naive stages[:maxTools] truncation that blindly amputated
// critical pipeline members landing past the scope cut.
func smartCap(stages []PipelineStep, maxTools int) []PipelineStep {
	if len(stages) <= maxTools {
		return stages
	}

	// Phase 1: Identify protected stages.
	roleCounts := make(map[string]int)
	for _, s := range stages {
		roleCounts[s.Role]++
	}

	protected := make(map[int]bool) // index → is protected
	for i, s := range stages {
		// Protect Trifecta members unconditionally.
		if trifectaURNs[s.ToolName] {
			protected[i] = true
			continue
		}
		// Protect sole role representatives.
		if roleCounts[s.Role] == 1 {
			protected[i] = true
		}
	}

	// Phase 2: Trim unprotected stages from the most over-represented role.
	excess := len(stages) - maxTools
	for excess > 0 {
		// Find the most over-represented role (by unprotected count).
		roleUnprotected := make(map[string]int)
		for i, s := range stages {
			if !protected[i] {
				roleUnprotected[s.Role]++
			}
		}

		// Find the role with the most unprotected members.
		var trimRole string
		trimMax := 0
		for role, count := range roleUnprotected {
			if count > trimMax {
				trimMax = count
				trimRole = role
			}
		}
		if trimMax == 0 {
			break // All remaining are protected — cannot trim further.
		}

		// Remove the LAST unprotected member of the most over-represented role
		// (last = lowest priority since stages are sorted by score descending).
		for i := len(stages) - 1; i >= 0; i-- {
			if stages[i].Role != trimRole || protected[i] {
				continue
			}
			stages = append(stages[:i], stages[i+1:]...)
			newProtected := make(map[int]bool)
			for k := range protected {
				if k < i {
					newProtected[k] = true
				} else if k > i {
					newProtected[k-1] = true
				}
			}
			protected = newProtected
			excess--
			break
		}
	}

	return stages
}
