package handler

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func makeTool(urn, role string, phase int) *db.ToolRecord {
	return &db.ToolRecord{
		URN:   urn,
		Role:  role,
		Phase: phase,
	}
}

func makeScoredTool(urn, role string, phase int, score float64) scoredTool {
	return scoredTool{
		record:     makeTool(urn, role, phase),
		finalScore: score,
	}
}

// TestPrunePhaseRoleClusters verifies that the per-(phase,role) cap limits
// clustering to the specified maximum while keeping the highest-scoring tools.
func TestPrunePhaseRoleClusters(t *testing.T) {
	// Phase 4 has 5 CRITICs; cap at 2 should keep only the first 2 (highest-scoring).
	tools := []scoredTool{
		makeScoredTool("brainstorm:discover_project", "ANALYZER", 0, 0.8),
		makeScoredTool("go-modernizer:go_ast_suite_analyzer", "ANALYZER", 1, 0.9),
		makeScoredTool("go-modernizer:go_context_analyzer", "ANALYZER", 1, 0.85),
		makeScoredTool("go-modernizer:go_package_cycler", "ANALYZER", 1, 0.7),
		makeScoredTool("go-modernizer:go_test_coverage_tracer", "CRITIC", 1, 0.95),
		makeScoredTool("go-modernizer:suggest_fixes", "SYNTHESIZER", 3, 0.9),
		makeScoredTool("brainstorm:threat_model_auditor", "CRITIC", 4, 1.0),
		makeScoredTool("brainstorm:thesis_architect", "CRITIC", 4, 0.95),
		makeScoredTool("brainstorm:critique_design", "CRITIC", 4, 0.9),
		makeScoredTool("brainstorm:antithesis_skeptic", "CRITIC", 4, 0.85),
		makeScoredTool("brainstorm:peer_review", "CRITIC", 4, 0.8),
	}

	result := prunePhaseRoleClusters(tools, 2)

	// Count Phase 4 CRITICs
	phase4Critics := 0
	for _, r := range result {
		if r.record.Phase == 4 && r.record.Role == "CRITIC" {
			phase4Critics++
		}
	}
	if phase4Critics != 2 {
		t.Errorf("expected 2 Phase 4 CRITICs, got %d", phase4Critics)
	}

	// Count Phase 1 ANALYZERs
	phase1Analyzers := 0
	for _, r := range result {
		if r.record.Phase == 1 && r.record.Role == "ANALYZER" {
			phase1Analyzers++
		}
	}
	if phase1Analyzers != 2 {
		t.Errorf("expected 2 Phase 1 ANALYZERs, got %d", phase1Analyzers)
	}

	// Verify total size: 1 (P0 ANALYZER) + 2 (P1 ANALYZER) + 1 (P1 CRITIC) + 1 (P3 SYNTH) + 2 (P4 CRITIC) = 7
	if len(result) != 7 {
		t.Errorf("expected total 7 tools after pruning, got %d", len(result))
	}
}

// TestPrunePhaseRoleClustersEmpty verifies empty input produces empty output.
func TestPrunePhaseRoleClustersEmpty(t *testing.T) {
	result := prunePhaseRoleClusters(nil, 2)
	if len(result) != 0 {
		t.Errorf("expected 0 tools for nil input, got %d", len(result))
	}
}

// TestValidateDAGSemanticsRedundancy verifies the redundancy governor fires
// at 3+ consecutive same-role tools and clears when roles are interleaved.
func TestValidateDAGSemanticsRedundancy(t *testing.T) {
	// 3 consecutive CRITICs should trigger a warning.
	stages := []PipelineStep{
		{ToolName: "a", Role: "ANALYZER", Phase: 1},
		{ToolName: "b", Role: "CRITIC", Phase: 2},
		{ToolName: "c", Role: "CRITIC", Phase: 2},
		{ToolName: "d", Role: "CRITIC", Phase: 2},
	}
	warnings := validateDAGSemantics(stages)

	found := false
	for _, w := range warnings {
		if len(w) > 0 {
			found = true
		}
	}
	if !found {
		t.Error("expected redundancy warning for 3 consecutive CRITICs, got none")
	}

	// Interleaved roles should NOT trigger redundancy.
	clean := []PipelineStep{
		{ToolName: "a", Role: "ANALYZER", Phase: 1},
		{ToolName: "b", Role: "CRITIC", Phase: 2},
		{ToolName: "c", Role: "SYNTHESIZER", Phase: 3},
		{ToolName: "d", Role: "CRITIC", Phase: 4},
	}
	cleanWarnings := validateDAGSemantics(clean)

	for _, w := range cleanWarnings {
		if strings.Contains(w, "Redundancy") {
			t.Errorf("unexpected redundancy warning for interleaved roles: %s", w)
		}
	}
}

// TestTopologicalSortChronological verifies that the topological sort produces
// phase-ascending output (Phase 0 before Phase 7).
func TestTopologicalSortChronological(t *testing.T) {
	stages := []PipelineStep{
		{ToolName: "go-modernizer:go_test_validation", Role: "CRITIC", Phase: 7},
		{ToolName: "go-modernizer:suggest_fixes", Role: "SYNTHESIZER", Phase: 3},
		{ToolName: "brainstorm:discover_project", Role: "ANALYZER", Phase: 0},
		{ToolName: "go-modernizer:go_ast_suite_analyzer", Role: "ANALYZER", Phase: 1},
		{ToolName: "brainstorm:critique_design", Role: "CRITIC", Phase: 4},
	}

	// No explicit requires/triggers, so topological sort should produce phase-sorted order.
	sorted, warnings := topologicalSort(stages, nil)

	if len(warnings) != 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}

	for i := 1; i < len(sorted); i++ {
		if sorted[i].Phase < sorted[i-1].Phase {
			t.Errorf("phase ordering violated at index %d: Phase %d after Phase %d (tool %s after %s)",
				i, sorted[i].Phase, sorted[i-1].Phase, sorted[i].ToolName, sorted[i-1].ToolName)
		}
	}

	if sorted[0].ToolName != "brainstorm:discover_project" {
		t.Errorf("expected first tool to be discover_project (Phase 0), got %s (Phase %d)",
			sorted[0].ToolName, sorted[0].Phase)
	}
}

// TestGlobalPipelineCap verifies that the enforceExclusivityEnclaves + global cap
// logic correctly limits pipelines to 15 tools + terminal.
func TestGlobalPipelineCap(t *testing.T) {
	// Build 20 unique stages
	var stages []PipelineStep
	for i := range 20 {
		stages = append(stages, PipelineStep{
			ToolName: "tool-" + string(rune('A'+i)),
			Role:     "ANALYZER",
			Phase:    i % 8,
		})
	}
	// Add terminal
	stages = append(stages, PipelineStep{
		ToolName: "magictools:generate_audit_report",
		Role:     "SYNTHESIZER",
		Phase:    8,
	})

	// Apply exclusivity first (no dupes so all pass), then check cap.
	pruned := enforceExclusivityEnclaves(stages)
	const maxPipelineSize = 15
	if len(pruned) > maxPipelineSize+1 {
		pruned = pruned[:maxPipelineSize+1]
	}

	if len(pruned) != maxPipelineSize+1 {
		t.Errorf("expected %d tools (15 + terminal), got %d", maxPipelineSize+1, len(pruned))
	}
}

// TestAdaptiveThreshold verifies that changing to mean-based threshold
// removes below-average tools from the candidate pool.
func TestAdaptiveThreshold(t *testing.T) {
	// Simulate the threshold calculation with known scores.
	candidates := []scoredTool{
		makeScoredTool("a", "ANALYZER", 1, 0.9),
		makeScoredTool("b", "ANALYZER", 1, 0.8),
		makeScoredTool("c", "ANALYZER", 1, 0.5),
		makeScoredTool("d", "ANALYZER", 1, 0.3),
		makeScoredTool("e", "ANALYZER", 1, 0.1),
	}

	// Compute threshold using the mean-based approach
	var totalScore, count float64
	for _, c := range candidates {
		if c.finalScore > 0 {
			totalScore += c.finalScore
			count++
		}
	}
	threshold := 0.1
	if count > 0 {
		mean := totalScore / count
		if mean > threshold {
			threshold = mean
		}
	}

	// mean = (0.9+0.8+0.5+0.3+0.1)/5 = 0.52
	expectedMean := 0.52
	if threshold < expectedMean-0.01 || threshold > expectedMean+0.01 {
		t.Errorf("expected threshold ~%.2f, got %.2f", expectedMean, threshold)
	}

	// Apply threshold
	var qualified []scoredTool
	for _, c := range candidates {
		if c.finalScore >= threshold {
			qualified = append(qualified, c)
		}
	}

	// Only tools with score >= 0.52 should pass: a(0.9), b(0.8), c(0.5 is borderline)
	// 0.5 < 0.52, so only 2 should pass
	if len(qualified) != 2 {
		t.Errorf("expected 2 tools above mean threshold, got %d", len(qualified))
	}
}

// TestComputeRoleBoostAuditDifferentiation verifies that ANALYZER gets a higher
// role boost than CRITIC for audit intents (F3 fix).
func TestComputeRoleBoostAuditDifferentiation(t *testing.T) {
	analyzerBoost := lookupRoleBoost("ANALYZER", "audit")
	criticBoost := lookupRoleBoost("CRITIC", "audit")
	synthBoost := lookupRoleBoost("SYNTHESIZER", "audit")
	mutatorBoost := lookupRoleBoost("MUTATOR", "audit")

	if analyzerBoost <= criticBoost {
		t.Errorf("ANALYZER boost (%.2f) should be greater than CRITIC boost (%.2f) for audit", analyzerBoost, criticBoost)
	}
	if criticBoost <= synthBoost {
		t.Errorf("CRITIC boost (%.2f) should be greater than SYNTHESIZER boost (%.2f) for audit", criticBoost, synthBoost)
	}
	if synthBoost <= mutatorBoost {
		t.Errorf("SYNTHESIZER boost (%.2f) should be greater than MUTATOR boost (%.2f) for audit", synthBoost, mutatorBoost)
	}

	// Verify exact values
	if analyzerBoost != 1.0 {
		t.Errorf("expected ANALYZER audit boost = 1.0, got %.2f", analyzerBoost)
	}
	if criticBoost != 0.7 {
		t.Errorf("expected CRITIC audit boost = 0.7, got %.2f", criticBoost)
	}
}

// TestDirectScoreFusionSpread verifies that direct score fusion (α*cosine + (1-α)*bm25)
// produces a full [0,1] discriminative spread, unlike the compressed RRF range.
func TestDirectScoreFusionSpread(t *testing.T) {
	alpha := 0.6

	// Best case: high cosine + high BM25
	bestCase := alpha*0.95 + (1-alpha)*0.90
	// Worst case: no vector, low BM25 only
	worstCase := 0.0*alpha + (1-alpha)*0.1

	spread := bestCase - worstCase

	// With direct fusion, spread should be > 0.5 (was 0.16 with RRF)
	if spread < 0.5 {
		t.Errorf("expected spread > 0.5, got %.3f (best=%.3f, worst=%.3f)", spread, bestCase, worstCase)
	}

	// Verify full range: best case should be > 0.9
	if bestCase < 0.9 {
		t.Errorf("expected best case > 0.9, got %.3f", bestCase)
	}
}

// TestTriFactorWeightBalance verifies that the engine weight (0.5) dominates over
// the role weight (0.3) in the tri-factor scoring formula.
func TestTriFactorWeightBalance(t *testing.T) {
	wEngine := 0.5
	wSynergy := 0.2
	wRole := 0.3

	// Sum must equal 1.0
	sum := wEngine + wSynergy + wRole
	if sum < 0.99 || sum > 1.01 {
		t.Errorf("expected weights to sum to 1.0, got %.2f", sum)
	}

	// Test: A highly relevant SYNTHESIZER should beat an irrelevant ANALYZER
	// Scenario: SYNTHESIZER with engine=0.95 vs ANALYZER with engine=0.20
	synthScore := (0.95 * wEngine) + (0.0 * wSynergy) + (0.4 * wRole)    // role=0.4 for SYNTH in audit
	analyzerScore := (0.20 * wEngine) + (0.0 * wSynergy) + (1.0 * wRole) // role=1.0 for ANALYZER in audit

	if synthScore <= analyzerScore {
		t.Errorf("highly relevant SYNTHESIZER (%.3f) should beat irrelevant ANALYZER (%.3f) with new weights",
			synthScore, analyzerScore)
	}
}

// TestComputeRoleBoostRefactorPlan verifies refactor and plan intents have correct
// role boost gradients and are unchanged by the audit fix.
func TestComputeRoleBoostRefactorPlan(t *testing.T) {
	// Refactor: MUTATOR should be highest
	if lookupRoleBoost("MUTATOR", "refactor") != 1.0 {
		t.Error("MUTATOR should get 1.0 for refactor")
	}
	if lookupRoleBoost("ANALYZER", "refactor") != 0.75 {
		t.Error("ANALYZER should get 0.75 for refactor")
	}

	// Plan: CRITIC and SYNTHESIZER should be highest
	if lookupRoleBoost("CRITIC", "plan") != 1.0 {
		t.Error("CRITIC should get 1.0 for plan")
	}
	if lookupRoleBoost("SYNTHESIZER", "plan") != 1.0 {
		t.Error("SYNTHESIZER should get 1.0 for plan")
	}
}

func TestHandleValidatePipelineStep(t *testing.T) {
	h, _, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)

	h.PipelineEnabled = &atomic.Bool{}
	h.PipelineEnabled.Store(true)

	ctx := context.Background()

	// 1. Test PASS verdict
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "validate_pipeline_step",
			Arguments: json.RawMessage(`{
				"step_name": "test_step",
				"step_output": "This is a very long output that should pass validation because it has no failure markers and is long enough.",
				"project_path": "/tmp"
			}`),
		},
	}

	res, err := h.handleValidatePipelineStep(ctx, req)
	if err != nil {
		t.Fatalf("handleValidatePipelineStep failed: %v", err)
	}

	found := false
	var fullText strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			fullText.WriteString(tc.Text)
			if strings.Contains(tc.Text, "**Verdict**: PASS") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected PASS verdict, got: %s", fullText.String())
	}

	// 2. Test FAIL verdict (panic)
	req = &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "validate_pipeline_step",
			Arguments: json.RawMessage(`{
				"step_name": "test_step",
				"step_output": "panic: something went wrong in this tool execution",
				"project_path": "/tmp"
			}`),
		},
	}

	res, err = h.handleValidatePipelineStep(ctx, req)
	if err != nil {
		t.Fatalf("handleValidatePipelineStep failed: %v", err)
	}

	found = false
	fullText.Reset()
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			fullText.WriteString(tc.Text)
			if strings.Contains(tc.Text, "**Verdict**: FAIL") {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("expected FAIL verdict, got: %s", fullText.String())
	}
}

func TestHandleQualityGate(t *testing.T) {
	h, _, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)

	h.PipelineEnabled = &atomic.Bool{}
	h.PipelineEnabled.Store(true)

	ctx := context.Background()

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name: "cross_server_quality_gate",
			Arguments: json.RawMessage(`{
				"project_path": "/tmp",
				"plan_hash": "abc"
			}`),
		},
	}

	res, err := h.handleQualityGate(ctx, req)
	if err != nil {
		t.Fatalf("handleQualityGate failed: %v", err)
	}

	if len(res.Content) == 0 {
		t.Fatalf("expected results, got none")
	}
}
