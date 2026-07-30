package handler

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/intelligence"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type freshRunState struct {
	contextBuffer   strings.Builder
	previousOutput  string
	results         []stepResult
	socraticVerdict string
	socraticPillars []pillarResult
	mutatorInjected bool
}

func (h *OrchestratorHandler) freshRunPipelineLoop(
	ctx context.Context,
	ps *ProxyService,
	sessionID, target, intent, planHash string,
	stages []PipelineStep,
	warnings []string,
	recallHeader string,
) (*mcp.CallToolResult, []PipelineStep, *freshRunState) {
	state := &freshRunState{previousOutput: recallHeader}
	state.contextBuffer.WriteString(recallHeader)

	for i := 0; i < len(stages); i++ {
		step := stages[i]
		if res := freshCheckCancelled(ctx, state.results, warnings); res != nil {
			return res, stages, state
		}
		if res := h.freshRunSingleStep(ctx, ps, sessionID, target, intent, planHash, &stages, &i, step, warnings, recallHeader, state); res != nil {
			return res, stages, state
		}
	}
	return nil, stages, state
}

func freshCheckCancelled(ctx context.Context, results []stepResult, warnings []string) *mcp.CallToolResult {
	select {
	case <-ctx.Done():
		telemetry.GlobalDAGTracker.ClosePipeline("CANCELLED")
		return buildPipelineResult(results, "CANCELLED", warnings)
	default:
		return nil
	}
}

func (h *OrchestratorHandler) freshRunSingleStep(
	ctx context.Context,
	ps *ProxyService,
	sessionID, target, intent, planHash string,
	stages *[]PipelineStep,
	i *int,
	step PipelineStep,
	warnings []string,
	recallHeader string,
	state *freshRunState,
) *mcp.CallToolResult {
	slog.Info("execute_pipeline: executing step", "index", *i+1, "total", len(*stages), keyURN, step.ToolName, "role", step.Role, "phase", step.Phase)
	telemetry.GlobalDAGTracker.UpdateActiveNode(step.ToolName, 0, 0, 0, "EXECUTING", "")

	if step.Role == roleMutator && strings.Contains(step.ToolName, "apply_vetted_edit") {
		return h.freshRunMutatorASTStep(ctx, ps, sessionID, target, planHash, step, state)
	}

	sr := h.executeDAGStep(ctx, ps, step, sessionID, target, state.previousOutput)
	state.results = append(state.results, sr)
	intelligence.RecordIntentOutcome(h.Store, intent, step.ToolName, sr.Status != statusFailed)
	freshRecordTransitionSynergy(h, *stages, *i, step, sr.Status != statusFailed)

	if sr.Status == statusFailed {
		return freshHaltOnFailure(intent, step, sr, state.results, warnings)
	}
	freshPruneAnchorsOnSuccess(h, step)
	freshAppendStepOutput(state, *i, step, sr.Output, recallHeader)
	freshHandleDynamicInject(sr, stages, step)
	freshMaybeInjectMutators(stages, i, step, state, sessionID)
	freshCaptureSocraticVerdict(step, sr, state.results, *stages, sessionID, state)
	freshIngestRecall(ctx, h, sessionID, target, state, *i, step)
	return nil
}

func freshRecordTransitionSynergy(h *OrchestratorHandler, stages []PipelineStep, i int, step PipelineStep, succeeded bool) {
	if i <= 0 {
		return
	}
	prevURN := stages[i-1].ToolName
	transitionHash := fmt.Sprintf("%x", sha256Hash(prevURN+"->"+step.ToolName))
	h.Store.RecordSynergy(transitionHash, succeeded)
}

func freshHaltOnFailure(intent string, step PipelineStep, sr stepResult, results []stepResult, warnings []string) *mcp.CallToolResult {
	intelligence.RecordFailureAnchor(context.Background(), step.ToolName, intent, intelligence.ClassifyError(sr.Error))
	slog.Error("execute_pipeline: critical step failure detected, HALTING pipeline for troubleshooting", keyURN, step.ToolName, keyError, sr.Error)
	telemetry.GlobalDAGTracker.ClosePipeline(statusFailed)
	return buildPipelineResult(results, statusFailed, warnings)
}

func freshPruneAnchorsOnSuccess(h *OrchestratorHandler, step PipelineStep) {
	if intel, err := h.Store.GetIntelligence(step.ToolName); err == nil && intel != nil {
		intelligence.PruneFailureAnchors(h.Store, step.ToolName, intel.Metrics.ProxyReliability)
	}
}

func freshAppendStepOutput(state *freshRunState, i int, step PipelineStep, output, recallHeader string) {
	if output == "" {
		return
	}
	fmt.Fprintf(&state.contextBuffer, "\n\n---\n## Step %d: %s [%s]\n\n%s", i+1, step.ToolName, step.Role, output)
	state.previousOutput = enforceContextCap(&state.contextBuffer, recallHeader)
}

func freshHandleDynamicInject(sr stepResult, stages *[]PipelineStep, step PipelineStep) {
	if !strings.Contains(sr.Output, "DYNAMIC_INJECT:") {
		return
	}
	dynParts := strings.Split(sr.Output, "DYNAMIC_INJECT:")
	if len(dynParts) <= 1 {
		return
	}
	injectedURN := strings.TrimSpace(strings.Split(dynParts[1], "\n")[0])
	if injectedURN == "" {
		return
	}
	slog.Info("execute_pipeline: dynamic DAG mitigation triggered, injecting stage", keyURN, injectedURN)
	*stages = append(*stages, PipelineStep{
		ToolName: injectedURN, Role: roleDiagnostic, Phase: step.Phase,
		Purpose: "Dynamically injected to mitigate unforeseen pipeline discovery.",
	})
}

func freshMaybeInjectMutators(stages *[]PipelineStep, i *int, step PipelineStep, state *freshRunState, sessionID string) {
	if step.Role != roleReporting || state.mutatorInjected {
		return
	}
	state.mutatorInjected = true
	decision := shouldInjectMutators(state.socraticVerdict, state.previousOutput, state.socraticPillars)
	slog.Info("execute_pipeline: MUTATOR gate evaluation",
		"socratic_verdict", state.socraticVerdict, "analysis_has_fixes", analysisContainsFixes(state.previousOutput),
		"pillar_count", len(state.socraticPillars), "decision", decision, keySessionID, sessionID)
	if !decision {
		return
	}
	mutatorStages := composeMutatorStages()
	*stages = slices.Insert(*stages, *i, mutatorStages...)
	slog.Info("execute_pipeline: autonomous MUTATOR injection", "verdict", state.socraticVerdict,
		"injected_count", len(mutatorStages), keySessionID, sessionID)
}

func freshCaptureSocraticVerdict(step PipelineStep, sr stepResult, results []stepResult, stages []PipelineStep, sessionID string, state *freshRunState) {
	if !strings.Contains(step.ToolName, "aporia") || sr.Status != statusDone {
		return
	}
	if v := extractSocraticVerdict(results, stages); v != nil {
		state.socraticVerdict = v.Verdict
		state.socraticPillars = v.Pillars
		slog.Info("execute_pipeline: Socratic verdict captured", "verdict", state.socraticVerdict,
			"pillar_count", len(state.socraticPillars), keySessionID, sessionID)
	}
}

func freshIngestRecall(ctx context.Context, h *OrchestratorHandler, sessionID, target string, state *freshRunState, i int, step PipelineStep) {
	if h.RecallClient == nil || !h.RecallClient.RecallEnabled() || state.previousOutput == "" {
		return
	}
	go func(bgCtx context.Context, sid, tgt, stepCtx string, stepNum int, urn string) { //nolint:gosec // G118: detached recall ingest
		saveToRecallOrWarn(bgCtx, h.RecallClient, sid, tgt, map[string]any{
			"cumulative_context": stepCtx, "last_step": stepNum, "last_urn": urn,
		})
	}(ctx, sessionID, target, state.previousOutput, i+1, step.ToolName)
}

func (h *OrchestratorHandler) freshRunMutatorASTStep(
	ctx context.Context,
	ps *ProxyService,
	sessionID, target, planHash string,
	step PipelineStep,
	state *freshRunState,
) *mcp.CallToolResult {
	slog.Info("execute_pipeline: executing MUTATOR AST path", keyURN, step.ToolName, keySessionID, sessionID)
	telemetry.GlobalDAGTracker.UpdateActiveNode(step.ToolName, 0, 0, 0, "EXECUTING", "")
	checkpointRef := createGitCheckpoint(target, sessionID)
	activePlanHash := freshResolvePlanHash(planHash, state.results)
	h.freshSeedPlanApproval(ctx, ps, sessionID, target, activePlanHash)
	mutResults := h.executeMutatorAST(ctx, ps, sessionID, target, activePlanHash, state.previousOutput)
	state.results = append(state.results, mutResults...)
	if len(mutResults) > 0 {
		telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, true)
	}
	freshHandleMutatorCheckpoint(target, checkpointRef, mutResults)
	return nil
}

func freshResolvePlanHash(planHash string, results []stepResult) string {
	if planHash != "" {
		return planHash
	}
	if extracted := extractPlanHashFromResults(results); extracted != "" {
		return extracted
	}
	return planHashAutoApproved
}

func (h *OrchestratorHandler) freshSeedPlanApproval(ctx context.Context, ps *ProxyService, sessionID, target, activePlanHash string) {
	if activePlanHash == "" || activePlanHash == planHashAutoApproved {
		return
	}
	seedArgs := map[string]any{
		keyServerID: serverBrainstorm, keyProjectID: target, keySessionID: sessionID,
		keyOutcome: outcomeApproved, keyStateData: fmt.Sprintf(planHashApprovalFmt, activePlanHash),
	}
	if _, seedErr := ps.ExecuteProxy(ctx, sourceRecall, "save_to_recall", seedArgs, 10*time.Second); seedErr != nil {
		slog.Warn("execute_pipeline: failed to seed plan approval in recall", "plan_hash", activePlanHash[:16]+"...", keyError, seedErr)
	} else {
		slog.Info("execute_pipeline: plan hash seeded as approved in recall", "plan_hash", activePlanHash[:16]+"...", keySessionID, sessionID)
	}
}

func freshHandleMutatorCheckpoint(target, checkpointRef string, mutResults []stepResult) {
	mutFailed := false
	for _, r := range mutResults {
		if r.Status == statusBlocked || r.Status == statusFailed {
			mutFailed = true
			break
		}
	}
	if mutFailed && checkpointRef != "" {
		slog.Warn("execute_pipeline: mutation failure detected — triggering rollback", "checkpoint", checkpointRef)
		rollbackGitCheckpoint(target, checkpointRef)
	} else if !mutFailed && checkpointRef != "" {
		cleanupGitCheckpoint(target)
	}
}

func (h *OrchestratorHandler) freshRunPostLoopMutators(
	ctx context.Context,
	ps *ProxyService,
	sessionID, target, planHash string,
	warnings []string,
	recallHeader string,
	state *freshRunState,
) *mcp.CallToolResult {
	if state.mutatorInjected || !shouldInjectMutators(state.socraticVerdict, state.previousOutput, state.socraticPillars) {
		return nil
	}
	slog.Info("execute_pipeline: post-loop MUTATOR injection (no REPORTING stages in DAG)",
		"verdict", state.socraticVerdict, keySessionID, sessionID)
	for _, step := range composeMutatorStages() {
		if res := freshCheckCancelled(ctx, state.results, warnings); res != nil {
			return res
		}
		slog.Info("execute_pipeline: executing post-loop MUTATOR step", keyURN, step.ToolName, "role", step.Role)
		telemetry.GlobalDAGTracker.UpdateActiveNode(step.ToolName, 0, 0, 0, "EXECUTING", "")
		if step.Role == roleMutator && strings.Contains(step.ToolName, "apply_vetted_edit") {
			h.freshRunMutatorASTStep(ctx, ps, sessionID, target, planHash, step, state)
			continue
		}
		sr := h.executeDAGStep(ctx, ps, step, sessionID, target, state.previousOutput)
		state.results = append(state.results, sr)
		if sr.Status == statusFailed {
			slog.Error("execute_pipeline: MUTATOR step failed", keyURN, step.ToolName, keyError, sr.Error)
			telemetry.GlobalDAGTracker.ClosePipeline(statusFailed)
			return buildPipelineResult(state.results, statusFailed, warnings)
		}
		freshAppendStepOutput(state, 0, step, sr.Output, recallHeader)
	}
	state.mutatorInjected = true
	return nil
}

func (h *OrchestratorHandler) freshIndexSyntheticIntent(intent string, results []stepResult) {
	if h.Store == nil || h.Store.Index == nil {
		return
	}
	go func(i string, r []stepResult) {
		var dag []string
		for _, res := range r {
			dag = append(dag, res.URN)
		}
		if err := h.Store.Index.IndexSyntheticIntent(i, dag); err != nil {
			slog.Warn("execute_pipeline: failed to index synthetic intent", keyError, err)
		}
	}(intent, results)
}

func freshInitDAGTelemetry(stages []PipelineStep) {
	nodeNames := make([]string, len(stages))
	var treeDepth int64 = 1
	for i, s := range stages {
		nodeNames[i] = s.ToolName
		if int64(s.Phase) > treeDepth {
			treeDepth = int64(s.Phase)
		}
	}
	edges := max(int64(len(stages)-1), 0)
	entropy := 1.0
	if treeDepth > 0 {
		entropy = float64(len(stages)) / float64(treeDepth)
	}
	telemetry.GlobalDAGTracker.InitializePipeline(
		fmt.Sprintf("exec-%d", time.Now().Unix()), nodeNames, entropy, edges, treeDepth,
	)
}

func (h *OrchestratorHandler) freshPipelineExecute(
	ctx context.Context,
	sessionID, target, intent, planHash string,
	stages []PipelineStep,
	warnings []string,
	recallContext strings.Builder,
) (*mcp.CallToolResult, error) {
	ps := NewProxyService(h)
	recallHeader := recallContext.String()
	res, _, state := h.freshRunPipelineLoop(ctx, ps, sessionID, target, intent, planHash, stages, warnings, recallHeader)
	if res != nil {
		return res, nil
	}
	if res := h.freshRunPostLoopMutators(ctx, ps, sessionID, target, planHash, warnings, recallHeader, state); res != nil {
		return res, nil
	}
	telemetry.GlobalDAGTracker.ClosePipeline(statusCompleted)
	slog.Info("execute_pipeline: DAG execution complete", "total_steps", len(state.results), "mutator_injected", state.mutatorInjected)
	h.freshIndexSyntheticIntent(intent, state.results)
	artifactURIs := savePipelineArtifacts(ctx, h, sessionID, state.results)
	return buildPipelineResultWithArtifacts(state.results, statusCompleted, warnings, sessionID, artifactURIs), nil
}
