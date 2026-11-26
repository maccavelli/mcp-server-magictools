package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// pipelineSessionRecord aliases the store record for handler-side tests and
// helpers; the wire contract is the db type.
type pipelineSessionRecord = db.PipelineSessionRecord

// pipelineInstanceNonce identifies this server process for the interrupted-run
// rule: a RUNNING session claimed by another nonce is never resumed (0005's
// D2.5 L-a). Generated once per handler.
func newPipelineInstanceNonce() string {
	return util.GeneratePipelineSessionID()
}

// pipelineSessions lazily binds the session store to the handler's database.
func (h *OrchestratorHandler) pipelineSessionStore() *db.PipelineSessionStore {
	if h.Store == nil {
		return nil
	}
	h.pipelineSessionsOnce.Do(func() {
		h.pipelineSessions = db.NewPipelineSessionStore(h.Store)
		h.pipelineInstanceNonce = newPipelineInstanceNonce()
	})
	return h.pipelineSessions
}

// freshPipelineDryRun composes the plan, stores it as PLANNED, and returns the
// plan, the issued id, the expiry and the fixed disclosure (0005's D2, Mode 1).
// A plan holding a mutating stage is shown but not stored.
func (h *OrchestratorHandler) freshPipelineDryRun(ctx context.Context, args pipelineArgs) (*mcp.CallToolResult, error) {
	recallContext := h.freshLoadRecallContext(ctx, args.Target, args.Intent)
	normalizedIntent := strings.ToLower(strings.TrimSpace(args.Intent))
	pipelineRecords := h.freshGatherPipelineRecords(ctx, args.Intent)
	analysisRoles := freshAnalysisRoles(args.TargetRoles)

	stages, warnings := h.executeSwarmBidding(ctx, normalizedIntent, analysisRoles, pipelineRecords)
	stages = freshCapStages(args.Intent, stages)
	if strictWarnings := validateDAGSemantics(stages); len(strictWarnings) > 0 {
		warnings = append(warnings, strictWarnings...)
	}
	if h.pipelineTestInjectStage != nil {
		stages = append(stages, *h.pipelineTestInjectStage)
	}
	if len(stages) == 0 {
		return pipelineTextResult("execute_pipeline produced zero stages for intent: " + args.Intent), nil
	}

	issuedID := util.GeneratePipelineSessionID()
	expiresAt := time.Now().Add(24 * time.Hour)

	mutating := planHasMutatingStage(stages)
	stored := false
	if !mutating {
		if ps := h.pipelineSessionStore(); ps != nil {
			rec := &db.PipelineSessionRecord{
				SessionID:     issuedID,
				Target:        args.Target,
				Intent:        args.Intent,
				PlanHash:      args.PlanHash,
				Status:        db.SessionPlanned,
				Stages:        toDBStages(stages),
				Warnings:      warnings,
				RecallContext: capRecallContext(recallContext.String()),
				Roles:         args.TargetRoles,
				ExpiresAt:     expiresAt.Unix(),
			}
			if err := ps.CreatePlanned(ctx, rec); err != nil {
				return nil, fmt.Errorf("execute_pipeline: failed to store plan: %w", err)
			}
			stored = true
		}
	}

	res := h.buildDryRunResult(args.Target, args.Intent, stages, warnings, recallContext.String())
	text := resultTextOf(res)
	var sb strings.Builder
	sb.WriteString(text)
	sb.WriteString("\n\n")
	if stored {
		sb.WriteString(pipelineDisclosure(issuedID, expiresAt))
	} else {
		sb.WriteString("This plan is not stored: it holds a mutating stage (MUTATOR role or apply_vetted_edit), and a stored plan never contains a mutator. Mutating stages are added at run time, after the reporting stage, when the Socratic verdict and the analysis call for them.\n")
	}
	// The issued id is returned in the envelope metadata as well as the text.
	return pipelineTextResult(sb.String()), nil
}

// pipelineDisclosure is the fixed disclosure block of 0005's D2 (Decided 9),
// word for word, with the issued id and expiry filled in. Held by a test.
func pipelineDisclosure(id string, expiresAt time.Time) string {
	return fmt.Sprintf(`Approval of this plan covers the analysis stages listed above, not file writes.
Mutating stages are not part of this plan: they are added at run time, after the
reporting stage, when the Socratic verdict and the analysis call for them. With no
plan_hash, mutation proceeds under the placeholder approval
"auto-approved-orchestrator-override".
To continue: call execute_pipeline with session_id "%s" (valid until %s).
To discard: call it with session_id "%s" and reject: true.`,
		id, expiresAt.UTC().Format(time.RFC3339), id)
}

// planHasMutatingStage reports whether any stage is MUTATOR-role or an
// apply_vetted_edit tool: such a plan is shown but never stored.
func planHasMutatingStage(stages []PipelineStep) bool {
	for _, s := range stages {
		if s.Role == roleMutator || strings.Contains(s.ToolName, "apply_vetted_edit") {
			return true
		}
	}
	return false
}

func toDBStages(stages []PipelineStep) []db.PipelineStage {
	out := make([]db.PipelineStage, len(stages))
	for i, s := range stages {
		out[i] = db.PipelineStage{
			ToolName: s.ToolName, Role: s.Role, Phase: s.Phase, Purpose: s.Purpose,
			Args: s.Args, InputContract: s.InputContract, OutputContract: s.OutputContract,
		}
	}
	return out
}

func fromDBStages(stages []db.PipelineStage) []PipelineStep {
	out := make([]PipelineStep, len(stages))
	for i, s := range stages {
		out[i] = PipelineStep{
			ToolName: s.ToolName, Role: s.Role, Phase: s.Phase, Purpose: s.Purpose,
			Args: s.Args, InputContract: s.InputContract, OutputContract: s.OutputContract,
		}
	}
	return out
}

// capRecallContext bounds the stored recall context at the existing context
// ceiling (maxContextBytes), keeping the tail.
func capRecallContext(s string) string {
	if len(s) <= maxContextBytes {
		return s
	}
	return s[len(s)-maxContextBytes:]
}

func resultTextOf(res *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// continuePipelineSession claims the stored session, checks the plan hash,
// pre-flights every stage, then runs the stored stages through the existing
// loop (0005's D2, Mode 2). The mutator gate, git checkpoint and rollback are
// unchanged because the same freshPipelineExecute runs them.
func (h *OrchestratorHandler) continuePipelineSession(ctx context.Context, sessionID, planHash string) (*mcp.CallToolResult, error) {
	ps := h.pipelineSessionStore()
	if ps == nil {
		return pipelineTextResult("execute_pipeline: unknown_session — no store available."), nil
	}

	rec, err := ps.GetForInstance(ctx, sessionID, h.pipelineInstanceNonce)
	if err != nil {
		return pipelineTextResult(pipelineSessionRefusalText(err.Error(), nil)), nil
	}
	if rec.Status == db.SessionRejected {
		return pipelineTextResult(pipelineSessionRefusalText("session_rejected", nil)), nil
	}

	// Plan-hash and pre-flight checks run BEFORE the claim, so a refusal never
	// leaves the session stuck RUNNING (independent review, important finding).
	if rec.Status == db.SessionPlanned {
		if rec.PlanHash != "" && planHash != rec.PlanHash {
			return pipelineTextResult(pipelineSessionRefusalText("plan_hash_mismatch", nil)), nil
		}
		for _, stage := range rec.Stages {
			if h.Store != nil {
				if tool, terr := h.Store.GetTool(stage.ToolName); terr != nil || tool == nil {
					return pipelineTextResult(pipelineSessionRefusalText("plan_stale", nil) + " Stage: " + stage.ToolName), nil
				}
			}
		}
	}

	claimed, err := ps.ClaimRunning(ctx, sessionID, h.pipelineInstanceNonce)
	if err != nil {
		var uris []string
		if errors.Is(err, db.ErrSessionAlreadyRun) {
			if got, gerr := ps.Get(ctx, sessionID); gerr == nil {
				uris = got.ArtifactURIs
			}
		}
		return pipelineTextResult(pipelineSessionRefusalText(err.Error(), uris)), nil
	}

	stages := fromDBStages(claimed.Stages)
	var recallCtx strings.Builder
	recallCtx.WriteString(claimed.RecallContext)
	res, runErr := h.freshPipelineExecute(ctx, sessionID, claimed.Target, claimed.Intent, claimed.PlanHash, stages, claimed.Warnings, recallCtx)
	if runErr != nil {
		if ferr := ps.Fail(ctx, sessionID); ferr != nil {
			slog.Warn("execute_pipeline: failed to mark session failed", keySessionID, sessionID, keyError, ferr)
		}
		return nil, runErr
	}

	status := resultStatusOf(res)
	if status == statusCompleted {
		uris := artifactURIsOf(res)
		if cerr := ps.Complete(ctx, sessionID, uris); cerr != nil {
			slog.Warn("execute_pipeline: failed to mark session completed", keySessionID, sessionID, keyError, cerr)
		}
	} else {
		if ferr := ps.Fail(ctx, sessionID); ferr != nil {
			slog.Warn("execute_pipeline: failed to mark session failed", keySessionID, sessionID, keyError, ferr)
		}
	}
	return res, nil
}

// rejectPipelineSession transitions PLANNED → REJECTED and runs nothing. This
// function is given no proxy service: the reject path has no executor in reach
// (0005's D2, Mode 2).
func (h *OrchestratorHandler) rejectPipelineSession(ctx context.Context, sessionID string) (*mcp.CallToolResult, error) {
	ps := h.pipelineSessionStore()
	if ps == nil {
		return pipelineTextResult("execute_pipeline: unknown_session — no store available."), nil
	}
	rec, err := ps.Reject(ctx, sessionID, h.pipelineInstanceNonce)
	if err != nil {
		return pipelineTextResult(pipelineSessionRefusalText(err.Error(), nil)), nil
	}
	envelope := map[string]any{
		keyMetadata: map[string]any{
			keyStatus:     "REJECTED",
			keySessionID:  rec.SessionID,
			"steps_total": 0,
		},
	}
	return pipelineTextResult(fmt.Sprintf("```json\n%s\n```\n\n# Pipeline Rejected\n\nSession %s rejected; nothing ran.",
		marshalIndentOrEmpty(envelope), rec.SessionID)), nil
}

// resultStatusOf reads the status from the result envelope's metadata, parsed
// as JSON rather than substring-matched (independent review: a step error
// containing the substring must not decide the session's terminal state).
func resultStatusOf(res *mcp.CallToolResult) string {
	text := resultTextOf(res)
	start := strings.Index(text, "```json\n")
	end := strings.Index(text, "\n```")
	if start >= 0 && end > start {
		var envelope struct {
			Metadata struct {
				Status string `json:"status"`
			} `json:"metadata"`
		}
		if err := json.Unmarshal([]byte(text[start+8:end]), &envelope); err == nil && envelope.Metadata.Status != "" {
			return envelope.Metadata.Status
		}
	}
	return statusFailed
}

// artifactURIsOf extracts the artifact URIs from a completed result envelope.
func artifactURIsOf(res *mcp.CallToolResult) []string {
	text := resultTextOf(res)
	start := strings.Index(text, "```json\n")
	end := strings.Index(text, "\n```")
	if start < 0 || end < 0 || end <= start {
		return nil
	}
	var envelope struct {
		Metadata struct {
			Artifacts map[string]string `json:"artifacts"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(text[start+8:end]), &envelope); err != nil {
		return nil
	}
	var uris []string
	for _, u := range envelope.Metadata.Artifacts {
		uris = append(uris, u)
	}
	return uris
}

// Test seams (set only by tests): count stage executions and inject a stage
// into a composed dry-run plan.
func (h *OrchestratorHandler) countPipelineStepForTest() {
	if h.pipelineTestCountStep != nil {
		h.pipelineTestCountStep()
	}
}
