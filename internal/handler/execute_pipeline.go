package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---------------------------------------------------------------------------
// execute_pipeline handler
// ---------------------------------------------------------------------------
// Unified DAG composer + executor. Runs all composed stages autonomously in
// a single pass. If the Socratic Trifecta approves proposed changes (ADOPT
// verdict) and actionable fixes are detected, MUTATOR stages are dynamically
// injected into the DAG before REPORTING and executed inline.

// handleExecutePipeline composes and optionally executes a pipeline DAG.
func (h *OrchestratorHandler) handleExecutePipeline(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if res, ok := h.pipelineGate(); !ok {
		return res, nil
	}

	var args struct {
		SessionID   string   `json:"session_id"`
		Target      string   `json:"target"`
		Intent      string   `json:"intent"`
		PlanHash    string   `json:"plan_hash"`
		DryRun      bool     `json:"dry_run"`
		TargetRoles []string `json:"target_roles"`
	}
	unmarshalArgsOrWarn(req.Params.Arguments, &args)

	if args.Target == "" || args.Intent == "" || args.SessionID == "" {
		res := &mcp.CallToolResult{}
		res.Content = []mcp.Content{&mcp.TextContent{Text: "execute_pipeline requires 'target', 'intent', and 'session_id' parameters."}}
		return res, nil
	}

	return h.freshPipeline(ctx, args.SessionID, args.Target, args.Intent, args.PlanHash, args.DryRun, args.TargetRoles)
}

// freshPipeline handles autonomous pipeline execution: DAG composition,
// sequential stage execution, verdict-driven MUTATOR injection, and completion.
func (h *OrchestratorHandler) freshPipeline(ctx context.Context, sessionID, target, intent, planHash string, dryRun bool, targetRoles []string) (*mcp.CallToolResult, error) {
	recallContext := h.freshLoadRecallContext(ctx, target, intent)
	normalizedIntent := strings.ToLower(strings.TrimSpace(intent))
	pipelineRecords := h.freshGatherPipelineRecords(ctx, intent)
	analysisRoles := freshAnalysisRoles(targetRoles)

	stages, warnings := h.executeSwarmBidding(ctx, normalizedIntent, analysisRoles, pipelineRecords)
	stages = freshCapStages(intent, stages)
	if strictWarnings := validateDAGSemantics(stages); len(strictWarnings) > 0 {
		warnings = append(warnings, strictWarnings...)
	}
	if len(stages) == 0 {
		res := &mcp.CallToolResult{}
		res.Content = []mcp.Content{&mcp.TextContent{Text: "execute_pipeline produced zero stages for intent: " + intent}}
		return res, nil
	}

	freshInitDAGTelemetry(stages)
	if dryRun {
		return h.buildDryRunResult(target, intent, stages, warnings, recallContext.String()), nil
	}

	slog.Info("execute_pipeline: starting autonomous DAG execution",
		keyTarget, target, "stages", len(stages), "warnings", len(warnings), "mode", "fresh")
	return h.freshPipelineExecute(ctx, sessionID, target, intent, planHash, stages, warnings, recallContext)
}

// buildDryRunResult formats the composed DAG as a human-readable markdown plan.
// This is functionally equivalent to the old compose_pipeline output.
func (h *OrchestratorHandler) buildDryRunResult(target, intent string, stages []PipelineStep, warnings []string, recallContext string) *mcp.CallToolResult {
	envelope := map[string]any{
		keyMetadata: map[string]any{
			keyTarget:    target,
			"intent":     intent,
			"node_count": len(stages),
			"warnings":   len(warnings),
			keyType:      dryRunType,
		},
	}
	envJSON := marshalIndentOrEmpty(envelope)

	var sb strings.Builder
	fmt.Fprintf(&sb, "```json\n%s\n```\n\n", string(envJSON))
	fmt.Fprintf(&sb, "# Pipeline Plan for: %s\n\n", target)
	fmt.Fprintf(&sb, "**Intent**: %s\n\n", intent)

	sb.WriteString("## Recommended Pipeline Stages\n\n")
	for i, stage := range stages {
		roleTag := ""
		if stage.Role != "" {
			roleTag = fmt.Sprintf(" [%s]", stage.Role)
		}
		phaseTag := ""
		if stage.Phase > 0 {
			phaseTag = fmt.Sprintf(" (Phase %d)", stage.Phase)
		}
		fmt.Fprintf(&sb, "%d. **%s**%s%s — %s\n", i+1, stage.ToolName, roleTag, phaseTag, stage.Purpose)
	}

	if len(warnings) > 0 {
		sb.WriteString("\n## ⚠️ Semantic Gatekeeper Warnings\n\n")
		for _, w := range warnings {
			sb.WriteString("- " + w + "\n")
		}
		sb.WriteString("\n")
	}

	if recallContext != "" {
		sb.WriteString("\n" + recallContext)
	}

	sb.WriteString("\n## Execution Notes\n")
	sb.WriteString("- Execute stages sequentially — each stage depends on the previous.\n")
	sb.WriteString("- Phase ordering: BOOTSTRAP → ANALYSIS → ADVERSARIAL → PROPOSAL → CRITIQUE → SYNTHESIS → PLANNER → MUTATOR → VALIDATION → TERMINAL.\n")
	sb.WriteString("- Only MUTATOR-role tools may write to the filesystem.\n")
	sb.WriteString("- To execute this plan, call execute_pipeline again with dry_run=false (default).\n")

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}
}

// ---------------------------------------------------------------------------
// Step execution
// ---------------------------------------------------------------------------

type stepResult struct {
	URN    string `json:"urn"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty,omitzero"`
	Output string `json:"output,omitempty,omitzero"`
}

// executeDAGStep runs a single pipeline step through the proxy service.
func (h *OrchestratorHandler) executeDAGStep(ctx context.Context, ps *ProxyService, step PipelineStep, sessionID, target, prevOutput string) (sr stepResult) {
	// Per-step panic isolation: convert any downstream panic into a FAILED stepResult
	// instead of crashing the entire orchestrator goroutine.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("execute_pipeline: PANIC RECOVERED in DAG step",
				keyURN, step.ToolName, "panic", r)
			telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, false)
			sr = stepResult{URN: step.ToolName, Status: statusFailed, Error: fmt.Sprintf("PANIC: %v", r)}
		}
	}()

	parts := strings.SplitN(step.ToolName, ":", 2)
	if len(parts) < 2 {
		telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, false)
		return stepResult{URN: step.ToolName, Status: statusFailed, Error: "invalid URN format"}
	}
	server, tool := parts[0], parts[1]

	// ── Schema-Aware Argument Injection ──
	// Instead of blindly injecting target/context/session_id, introspect the
	// tool's InputSchema to discover what properties it actually accepts.
	// This prevents additionalProperties:false schemas from rejecting our
	// injected arguments.
	arguments := buildSchemaAwareArgs(h.Store, step.ToolName, sessionID, target, prevOutput)

	// ── Loopback Dispatch for magictools:* URNs ──
	// The orchestrator is NOT a registered sub-server in WarmRegistry, so
	// routing magictools:* tools through CallProxy fails with "server
	// magictools not running". Instead, dispatch directly through the
	// loopbackHandlers map which was populated at tool registration time.
	if server == serverMagictools {
		handler, ok := h.loopbackHandlers[tool]
		if !ok {
			telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, false)
			return stepResult{URN: step.ToolName, Status: statusFailed, Error: fmt.Sprintf("no loopback handler registered for magictools:%s", tool)}
		}

		argsJSON := marshalJSONOrEmpty(arguments)
		loopbackReq := &mcp.CallToolRequest{
			Params: &mcp.CallToolParamsRaw{
				Name:      tool,
				Arguments: argsJSON,
			},
		}

		res, err := handler(ctx, loopbackReq)
		if err != nil {
			telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, false)
			return stepResult{URN: step.ToolName, Status: statusFailed, Error: err.Error()}
		}

		output := extractTextOutput(res)
		telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, true)
		return stepResult{URN: step.ToolName, Status: statusDone, Output: output}
	}

	if _, bootErr := ps.EnsureServerReady(ctx, server); bootErr != nil {
		telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, false)
		return stepResult{URN: step.ToolName, Status: statusFailed, Error: fmt.Sprintf("failed to lazy-boot sub-server %s: %s", server, bootErr.Error())}
	}

	timeout := 120 * time.Second
	res, err := ps.ExecuteProxy(ctx, server, tool, arguments, timeout)
	if err != nil {
		telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, false)
		return stepResult{URN: step.ToolName, Status: statusFailed, Error: err.Error()}
	}

	output := extractTextOutput(res)
	telemetry.GlobalDAGTracker.CompleteNode(step.ToolName, true)
	return stepResult{URN: step.ToolName, Status: statusDone, Output: output}
}

// extractTextOutput pulls the first text content from a tool result.
func extractTextOutput(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	if res.StructuredContent != nil {
		data, err := json.Marshal(res.StructuredContent)
		if err == nil {
			return string(data)
		}
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok && tc.Text != "" {
			return tc.Text
		}
	}
	return ""
}

// extractStagingURI parses the staging_uri from a tool's JSON output.
func extractStagingURI(output string) string {
	var parsed struct {
		StagingURI string `json:"staging_uri"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err == nil {
		return parsed.StagingURI
	}
	return ""
}

// extractPlanHashFromResults scans the executed steps for generate_implementation_plan
// and attempts to parse out the generated plan_hash to feed into apply_vetted_edit.
func extractPlanHashFromResults(results []stepResult) string {
	for _, r := range results {
		if strings.Contains(r.URN, "generate_implementation_plan") && r.Status == statusDone {
			var parsed struct {
				Metadata struct {
					PlanHash string `json:"plan_hash"`
				} `json:"metadata"`
			}
			if err := json.Unmarshal([]byte(r.Output), &parsed); err == nil && parsed.Metadata.PlanHash != "" {
				return parsed.Metadata.PlanHash
			}
		}
	}
	return ""
}

// buildSchemaAwareArgs introspects the tool's InputSchema from the store to
// build an arguments map containing only properties the tool actually declares.
// This prevents additionalProperties:false schemas from rejecting injected args.
//
// Mapping rules:
//   - session_id: injected if the schema has a keySessionID property
//   - target: injected if the schema has keyTarget; mapped to "pkg" if the schema
//     has "pkg" but not keyTarget (e.g. go_test_coverage_tracer)
//   - context: injected only if the schema has a "context" property
//
// Falls back to the full set {session_id, target, context} if the schema cannot
// be looked up (defensive: better to try and fail than silently skip args).
func buildSchemaAwareArgs(store *db.Store, urn, sessionID, target, prevOutput string) map[string]any {
	args := make(map[string]any)

	// Attempt schema lookup.
	props := schemaProperties(store, urn)
	if props == nil {
		// Fallback: can't introspect schema — inject everything and hope for the best.
		slog.Debug("buildSchemaAwareArgs: schema lookup failed, using full injection", keyURN, urn)
		args[keySessionID] = sessionID
		args[keyTarget] = target
		if prevOutput != "" {
			args["context"] = prevOutput
		}
		return args
	}

	// session_id
	if _, ok := props[keySessionID]; ok {
		args[keySessionID] = sessionID
	}

	// target → target or target → pkg
	if _, ok := props[keyTarget]; ok {
		args[keyTarget] = target
	} else if _, ok := props["pkg"]; ok {
		args["pkg"] = target
	} else if _, ok := props["project_path"]; ok {
		args["project_path"] = target
	} else if _, ok := props["path"]; ok {
		args["path"] = target
	}

	// context
	if prevOutput != "" {
		if _, ok := props["context"]; ok {
			args["context"] = prevOutput
		}
	}

	// Auto-populate unknown required string properties.
	// Tools like peer_review require 'focus' and 'component_design' that the
	// orchestrator doesn't have specific knowledge of. Rather than failing with
	// a schema validation error, populate them from the chained analysis context.
	required := schemaRequired(store, urn)
	for _, field := range required {
		if _, alreadySet := args[field]; alreadySet {
			continue
		}
		// Only auto-populate string-typed properties.
		if propDef, ok := props[field]; ok {
			if propMap, isMap := propDef.(map[string]any); isMap {
				if propMap[keyType] == schemaTypeString {
					if prevOutput != "" {
						args[field] = prevOutput
					} else {
						args[field] = target
					}
					slog.Debug("buildSchemaAwareArgs: auto-populated required field",
						keyURN, urn, "field", field)
				}
			}
		}
	}

	return args
}

// schemaProperties extracts the keyProperties map from a tool's InputSchema.
// Returns nil if the store, tool, or schema structure is unavailable.
func schemaProperties(store *db.Store, urn string) map[string]any {
	if store == nil {
		return nil
	}
	rec, err := store.GetTool(urn)
	if err != nil || rec == nil {
		return nil
	}
	schema := rec.InputSchema
	// The sync pipeline stores schemas separately under SchemaHash to deduplicate.
	// ToolRecord.InputSchema is typically nil — resolve via GetSchema fallback.
	if schema == nil && rec.SchemaHash != "" {
		schema = schemaFromStoreOrWarn(store, rec.SchemaHash)
	}
	if schema == nil {
		return nil
	}
	propsRaw, ok := schema[keyProperties]
	if !ok {
		return nil
	}
	props, ok := propsRaw.(map[string]any)
	if !ok {
		return nil
	}
	return props
}

// schemaRequired extracts the keyRequired array from a tool's InputSchema.
// Returns nil if the store, tool, or schema structure is unavailable.
func schemaRequired(store *db.Store, urn string) []string {
	if store == nil {
		return nil
	}
	rec, err := store.GetTool(urn)
	if err != nil || rec == nil {
		return nil
	}
	schema := rec.InputSchema
	// Fallback: resolve schema from hash-addressed store.
	if schema == nil && rec.SchemaHash != "" {
		schema = schemaFromStoreOrWarn(store, rec.SchemaHash)
	}
	if schema == nil {
		return nil
	}
	reqRaw, ok := schema[keyRequired]
	if !ok {
		return nil
	}
	reqSlice, ok := reqRaw.([]any)
	if !ok {
		return nil
	}
	result := make([]string, 0, len(reqSlice))
	for _, r := range reqSlice {
		if s, ok := r.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// Mutation budget constants. These hard ceilings prevent runaway pipeline
// mutations from exhausting system resources or modifying too many files.
const (
	maxMutationFiles   = 50             // Maximum files the MUTATOR stage will modify.
	maxPayloadPerFile  = 1 << 20        // 1MB per-file payload ceiling.
	maxCumulativeBytes = 10 * (1 << 20) // 10MB total mutation budget.
)

// executeMutatorAST walks all .go files in the target project, applies
// automated AST transformations (missing godoc, struct tag fixes), and
// writes each modified file through apply_vetted_edit.
//
// Uses a 3-phase shadow buffer pattern for transactional safety:
//   - Phase 1 (ACCUMULATE): Collect all modified files in-memory — zero disk writes
//   - Phase 2 (VALIDATE): Write to temp staging dir, run go build -overlay validation
//   - Phase 3 (COMMIT OR ABORT): Flush via apply_vetted_edit only if build passes
//
// Budget enforcement: stops processing after maxMutationFiles or
// maxCumulativeBytes is reached. Per-file payload exceeding
// maxPayloadPerFile is skipped.
func (h *OrchestratorHandler) executeMutatorAST(ctx context.Context, ps *ProxyService, sessionID, target, planHash, _ string) []stepResult {
	var results []stepResult

	var goFiles []string
	walkDirOrWarn(target, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			goFiles = append(goFiles, path)
		}
		return nil
	})

	slog.Info("execute_pipeline: MUTATOR AST scan", "files", len(goFiles), keyTarget, target)

	// ─────────────────────────────────────────────────────────────────
	// Phase 1: ACCUMULATE — collect all modifications in-memory
	// ─────────────────────────────────────────────────────────────────
	shadowBuffer := make(map[string][]byte) // path → modified source
	mutationCount := 0
	cumulativeBytes := 0

	for _, path := range goFiles {
		// Budget ceiling: stop after maxMutationFiles.
		if mutationCount >= maxMutationFiles {
			results = append(results, stepResult{
				URN:    "apply_vetted_edit:budget",
				Status: "SKIPPED",
				Output: fmt.Sprintf("Mutation budget reached: %d/%d files", mutationCount, maxMutationFiles),
			})
			slog.Warn("execute_pipeline: mutation budget reached", "count", mutationCount, "max", maxMutationFiles)
			break
		}

		src, err := os.ReadFile(path) //nolint:gosec // G304: path from filepath.Walk of validated pipeline target
		if err != nil {
			slog.Warn("execute_pipeline: MUTATOR read failed", "path", path, keyError, err)
			continue
		}

		// Per-file payload ceiling.
		if len(src) > maxPayloadPerFile {
			slog.Warn("execute_pipeline: skipping oversized file",
				"path", path, "size", len(src), "max", maxPayloadPerFile)
			continue
		}

		modified, changed := applyASTTransformations(path, src)
		if !changed {
			continue
		}

		// Lightweight delta pre-check: reject if declaration count decreased.
		if decreased, origCount, modCount := quickDeclCountCheck(src, modified); decreased {
			slog.Error("execute_pipeline: DESTRUCTIVE_MUTATION_BLOCKED — declaration count decreased",
				"path", path, "orig_decls", origCount, "mod_decls", modCount)
			results = append(results, stepResult{
				URN:    "apply_vetted_edit:" + filepath.Base(path),
				Status: statusBlocked,
				Error:  fmt.Sprintf("DESTRUCTIVE_MUTATION_BLOCKED: declarations %d→%d", origCount, modCount),
			})
			continue
		}

		// Cumulative budget check.
		if cumulativeBytes+len(modified) > maxCumulativeBytes {
			results = append(results, stepResult{
				URN:    "apply_vetted_edit:budget",
				Status: "SKIPPED",
				Output: fmt.Sprintf("Cumulative budget reached: %d/%d bytes", cumulativeBytes, maxCumulativeBytes),
			})
			slog.Warn("execute_pipeline: cumulative mutation budget reached",
				"cumulative", cumulativeBytes, "max", maxCumulativeBytes)
			break
		}

		// Stage in shadow buffer — NO disk write yet.
		shadowBuffer[path] = modified
		mutationCount++
		cumulativeBytes += len(modified)
	}

	if len(shadowBuffer) == 0 {
		if len(results) == 0 {
			results = append(results, stepResult{URN: "apply_vetted_edit", Status: statusDone, Output: "No AST transformations required."})
		}
		return results
	}

	slog.Info("execute_pipeline: shadow buffer accumulated",
		"files", len(shadowBuffer), "total_bytes", cumulativeBytes)

	// ─────────────────────────────────────────────────────────────────
	// Phase 2: VALIDATE — write to staging dir, run go build -overlay
	// ─────────────────────────────────────────────────────────────────
	buildPassed := h.validateShadowBuffer(ctx, sessionID, target, shadowBuffer)
	if !buildPassed {
		slog.Error("execute_pipeline: shadow buffer VALIDATION FAILED — aborting ALL mutations, zero files written")
		results = append(results, stepResult{
			URN:    "shadow_buffer:validation",
			Status: statusFailed,
			Error:  "Build validation failed against staged changes — zero files written to disk",
		})
		return results
	}

	results = append(results, stepResult{
		URN:    "shadow_buffer:validation",
		Status: statusDone,
		Output: fmt.Sprintf("Build validation passed for %d staged files", len(shadowBuffer)),
	})

	// ─────────────────────────────────────────────────────────────────
	// Phase 3: COMMIT — flush all validated files through apply_vetted_edit
	// ─────────────────────────────────────────────────────────────────
	for path, content := range shadowBuffer {
		slog.Info("execute_pipeline: MUTATOR committing validated edit", "path", path, "size", len(content))

		if _, bootErr := ps.EnsureServerReady(ctx, "go-modernizer"); bootErr != nil {
			results = append(results, stepResult{
				URN:    "apply_vetted_edit:" + filepath.Base(path),
				Status: statusFailed,
				Error:  fmt.Sprintf("failed to lazy-boot go-modernizer: %s", bootErr.Error()),
			})
			return results
		}
		editArgs := map[string]any{
			keySessionID: sessionID,
			keyTarget:    path,
			"context":    string(content),
			"flags": map[string]any{
				"plan_hash": planHash,
			},
		}

		res, err := ps.ExecuteProxy(ctx, "go-modernizer", "apply_vetted_edit", editArgs, 30*time.Second)
		if err != nil {
			results = append(results, stepResult{URN: "apply_vetted_edit:" + filepath.Base(path), Status: statusFailed, Error: err.Error()})
			continue
		}
		results = append(results, stepResult{
			URN:    "apply_vetted_edit:" + filepath.Base(path),
			Status: statusDone,
			Output: extractTextOutput(res),
		})
	}

	return results
}

// validateShadowBuffer writes the staged files to a temporary directory,
// generates a go build overlay JSON, and runs build validation against it.
// Returns true if the build passes, false otherwise.
func (h *OrchestratorHandler) validateShadowBuffer(ctx context.Context, sessionID, target string, shadowBuffer map[string][]byte) bool {
	stagingDir, err := os.MkdirTemp("", "pipeline-stage-"+sessionID+"-")
	if err != nil {
		slog.Error("execute_pipeline: failed to create staging directory", keyError, err)
		return false
	}
	defer os.RemoveAll(stagingDir) //nolint:errcheck // best-effort cleanup

	// Write staged files to temp directory and build the overlay map.
	overlayReplace := make(map[string]string) // real path → staging path
	for realPath, content := range shadowBuffer {
		// Preserve the relative path structure under staging dir.
		relPath := relPathOrBase(target, realPath)
		stagingPath := filepath.Join(stagingDir, relPath)

		if mkErr := os.MkdirAll(filepath.Dir(stagingPath), 0o750); mkErr != nil {
			slog.Error("execute_pipeline: staging mkdir failed", "path", stagingPath, keyError, mkErr)
			return false
		}
		if writeErr := os.WriteFile(stagingPath, content, 0o600); writeErr != nil {
			slog.Error("execute_pipeline: staging write failed", "path", stagingPath, keyError, writeErr)
			return false
		}
		overlayReplace[realPath] = stagingPath
	}

	// Generate the overlay JSON file.
	overlayData := map[string]any{"Replace": overlayReplace}
	overlayJSON, err := json.Marshal(overlayData)
	if err != nil {
		slog.Error("execute_pipeline: overlay JSON marshal failed", keyError, err)
		return false
	}

	overlayPath := filepath.Join(stagingDir, "overlay.json")
	if writeErr := os.WriteFile(overlayPath, overlayJSON, 0o600); writeErr != nil {
		slog.Error("execute_pipeline: overlay JSON write failed", keyError, writeErr)
		return false
	}

	slog.Info("execute_pipeline: running overlay build validation",
		"overlay_path", overlayPath, "staged_files", len(overlayReplace))

	// Pre-flight: check if the unmodified project builds first.
	preCheck, preErr := exec.CommandContext(ctx, mcpGoBinary(), "build", "./...").CombinedOutput() //nolint:gosec // G204: go binary from mcpGoBinary(), target path validated
	if preErr != nil {
		slog.Warn("execute_pipeline: project does not build cleanly before mutations — skipping overlay validation",
			keyError, preErr, "output", string(preCheck))
		return true // Can't validate overlay if the project is already broken.
	}

	// Run the overlay build check.
	cmd := exec.CommandContext(ctx, mcpGoBinary(), "build", "-overlay", overlayPath, "./...") //nolint:gosec // G204: overlayPath is a temp file we just wrote; go binary resolved via mcpGoBinary()
	cmd.Dir = target
	out, buildErr := cmd.CombinedOutput()
	if buildErr != nil {
		slog.Error("execute_pipeline: overlay build validation FAILED",
			keyError, buildErr, "output", string(out))
		return false
	}

	slog.Info("execute_pipeline: overlay build validation PASSED")
	return true
}

// applyASTTransformations parses a Go source file read-only to detect which
// exported symbols lack Godoc comments, then uses text-based line insertion
// (not AST position manipulation) to inject the missing comments. Returns
// the modified source and whether any change was made.
//
// This approach avoids the fundamental Go AST comment positioning bugs:
//   - token.Pos arithmetic ("somePos - 1") means one BYTE earlier, not one LINE above
//   - Unsorted file.Comments arrays cause format.Node to emit garbage
//   - format.Node can mangle original formatting when reconstructing from a modified AST
//
// Instead, we:
//  1. Parse AST read-only to detect missing Godoc
//  2. Collect (lineNumber, commentText) pairs
//  3. Sort by line number descending (to avoid offset shifts during insertion)
//  4. Insert comment lines into the raw source text via line splitting
//  5. Validate the result with parser.ParseFile + format.Source
func applyASTTransformations(path string, src []byte) ([]byte, bool) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, src, parser.ParseComments)
	if err != nil {
		slog.Warn("execute_pipeline: AST parse failed", "path", path, keyError, err)
		return src, false
	}

	// Skip generated files.
	if isGeneratedFile(src) {
		slog.Debug("execute_pipeline: skipping generated file", "path", path)
		return src, false
	}

	// Collect insertion points: (1-indexed line number, comment text).
	type insertion struct {
		line int    // Insert BEFORE this line number (1-indexed).
		text string // The comment line to insert.
	}
	var insertions []insertion

	// Check for missing package-level doc comment.
	if file.Doc == nil || len(file.Doc.List) == 0 {
		pkgLine := fset.Position(file.Package).Line
		pkgName := file.Name.Name
		insertions = append(insertions, insertion{
			line: pkgLine,
			text: fmt.Sprintf("// Package %s provides functionality for the %s subsystem.", pkgName, pkgName),
		})
	}

	// Check for missing godoc on exported declarations.
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok == token.TYPE && d.Doc == nil {
				for _, spec := range d.Specs {
					ts, ok := spec.(*ast.TypeSpec)
					if !ok || !ts.Name.IsExported() {
						continue
					}
					declLine := fset.Position(d.Pos()).Line
					insertions = append(insertions, insertion{
						line: declLine,
						text: fmt.Sprintf("// %s defines the %s structure.", ts.Name.Name, ts.Name.Name),
					})
				}
			}
		case *ast.FuncDecl:
			if d.Doc == nil && d.Name.IsExported() {
				declLine := fset.Position(d.Pos()).Line
				insertions = append(insertions, insertion{
					line: declLine,
					text: fmt.Sprintf("// %s performs the %s operation.", d.Name.Name, d.Name.Name),
				})
			}
		}
	}

	if len(insertions) == 0 {
		return src, false
	}

	// Sort insertions by line number DESCENDING to avoid offset shifts.
	sort.Slice(insertions, func(i, j int) bool {
		return insertions[i].line > insertions[j].line
	})

	// Split source into lines, insert comments, rejoin.
	lines := strings.Split(string(src), "\n")
	for _, ins := range insertions {
		idx := min(
			// Convert to 0-indexed.
			max(

				ins.line-1, 0), len(lines))
		// Insert the comment line before the target line.
		newLines := make([]string, 0, len(lines)+1)
		newLines = append(newLines, lines[:idx]...)
		newLines = append(newLines, ins.text)
		newLines = append(newLines, lines[idx:]...)
		lines = newLines
	}

	modified := []byte(strings.Join(lines, "\n"))

	// Validate: the modified source must parse and gofmt successfully.
	if _, parseErr := parser.ParseFile(token.NewFileSet(), path, modified, parser.ParseComments); parseErr != nil {
		slog.Warn("execute_pipeline: post-transformation validation failed (parse)",
			"path", path, keyError, parseErr)
		return src, false
	}
	formatted, fmtErr := format.Source(modified)
	if fmtErr != nil {
		slog.Warn("execute_pipeline: post-transformation validation failed (gofmt)",
			"path", path, keyError, fmtErr)
		return src, false
	}

	return formatted, true
}

// isGeneratedFile checks if a Go source file contains the standard "Code generated"
// marker indicating it should not be manually edited.
func isGeneratedFile(src []byte) bool {
	// Check first 1KB for the generated marker to avoid scanning large files.
	header := src
	if len(header) > 1024 {
		header = header[:1024]
	}
	return strings.Contains(string(header), "Code generated") ||
		strings.Contains(string(header), "DO NOT EDIT")
}

// quickDeclCountCheck is a fast-path delta pre-check that counts top-level
// declarations (func, type, var, const) in both the original and modified
// source. If the modified source has fewer declarations, this indicates a
// destructive mutation that should be blocked.
func quickDeclCountCheck(originalSrc, modifiedSrc []byte) (decreased bool, origCount, modCount int) {
	origCount = countDecls(originalSrc)
	modCount = countDecls(modifiedSrc)
	if origCount < 0 {
		return false, 0, 0 // Cannot parse original — skip check.
	}
	if modCount < 0 {
		return true, origCount, 0 // Modified source doesn't parse — always block.
	}
	return modCount < origCount, origCount, modCount
}

// countDecls parses Go source and returns the number of top-level declarations.
// Returns -1 if the source cannot be parsed.
func countDecls(src []byte) int {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return -1
	}
	count := 0
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			count++
		case *ast.GenDecl:
			count += len(d.Specs)
		}
	}
	return count
}

// ---------------------------------------------------------------------------
// Git checkpoint operations for transactional mutation safety
// ---------------------------------------------------------------------------

// createGitCheckpoint creates a git stash checkpoint before mutations begin.
// Returns the stash reference string if successful, or empty string if the
// target is not a git repository or the stash fails.
func createGitCheckpoint(target, sessionID string) string {
	// Verify this is a git repo.
	if _, err := exec.Command("git", "-C", target, "rev-parse", "--git-dir").Output(); err != nil { //nolint:gosec // G204: target is a validated pipeline project path, git binary is hardcoded
		slog.Debug("execute_pipeline: target is not a git repo, skipping checkpoint", keyTarget, target)
		return ""
	}

	// Stage all current changes so stash captures everything.
	_ = exec.Command("git", "-C", target, "add", "-A").Run() //nolint:errcheck,gosec // best-effort staging; G204: hardcoded git binary

	msg := fmt.Sprintf("pipeline-checkpoint-%s", sessionID)
	out, err := exec.Command("git", "-C", target, "stash", "push", "-m", msg).CombinedOutput() //nolint:gosec // G204: hardcoded git binary with validated target path
	if err != nil {
		slog.Warn("execute_pipeline: git stash checkpoint failed", keyError, err, "output", string(out))
		return ""
	}
	// Check if stash actually created something (vs "No local changes to save").
	if strings.Contains(string(out), "No local changes") {
		slog.Debug("execute_pipeline: no changes to checkpoint")
		return ""
	}
	return msg
}

// rollbackGitCheckpoint restores the working directory to the pre-mutation state
// by applying the stash checkpoint and discarding current changes.
func rollbackGitCheckpoint(target, checkpointRef string) {
	// First, discard the corrupted mutations.
	if err := exec.Command("git", "-C", target, "checkout", "--", ".").Run(); err != nil { //nolint:gosec // G204: hardcoded git binary
		slog.Error("execute_pipeline: git checkout failed during rollback", keyError, err)
	}
	// Then, restore the stashed pre-mutation state.
	out, err := exec.Command("git", "-C", target, "stash", "pop").CombinedOutput() //nolint:gosec // G204: hardcoded git binary
	if err != nil {
		slog.Error("execute_pipeline: git stash pop failed during rollback",
			keyError, err, "output", string(out), "checkpoint", checkpointRef)
		return
	}
	slog.Info("execute_pipeline: rollback complete — pre-mutation state restored",
		"checkpoint", checkpointRef, keyTarget, target)
}

// cleanupGitCheckpoint drops the most recent stash entry after a successful mutation.
func cleanupGitCheckpoint(target string) {
	out, err := exec.Command("git", "-C", target, "stash", "drop").CombinedOutput() //nolint:gosec // G204: hardcoded git binary
	if err != nil {
		slog.Warn("execute_pipeline: git stash cleanup failed (non-critical)",
			keyError, err, "output", string(out))
	}
}

// ---------------------------------------------------------------------------
// Result builder
// ---------------------------------------------------------------------------

// mcpGoBinary returns the Go binary to use for pipeline build validation.
// Priority: MCP_GO_BIN_PATH env var → common SDK paths → "go" fallback.
func mcpGoBinary() string {
	if v := os.Getenv("MCP_GO_BIN_PATH"); v != "" {
		return v
	}
	home := homeDirOrDot()
	candidates := []string{
		filepath.Join(home, "sdk", "go1.26.5", "bin", "go"),
		filepath.Join(home, "sdk", "go1.26.1", "bin", "go"),
		filepath.Join(home, ".local", "go", "bin", "go"),
		filepath.Join(home, "go", "bin", "go"),
		"/usr/local/go/bin/go",
	}
	if matches, err := filepath.Glob(filepath.Join(home, "sdk", "go*", "bin", "go")); err == nil {
		candidates = append(matches, candidates...)
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0 {
			return c
		}
	}
	return "go"
}

func buildPipelineResult(results []stepResult, status string, warnings []string) *mcp.CallToolResult {
	return buildPipelineResultWithArtifacts(results, status, warnings, "", nil)
}

func buildPipelineResultWithArtifacts(results []stepResult, status string, warnings []string, sessionID string, artifactURIs map[string]string) *mcp.CallToolResult {
	metadata := map[string]any{
		keyStatus:     status,
		"steps_total": len(results),
		"warnings":    len(warnings),
	}
	if sessionID != "" {
		metadata[keySessionID] = sessionID
	}
	if len(artifactURIs) > 0 {
		metadata["artifacts"] = artifactURIs
	}
	envelope := map[string]any{keyMetadata: metadata}
	envJSON := marshalIndentOrEmpty(envelope)

	var sb strings.Builder
	fmt.Fprintf(&sb, "```json\n%s\n```\n\n", string(envJSON))
	fmt.Fprintf(&sb, "# Pipeline Execution: %s\n\n", status)
	fmt.Fprintf(&sb, "**Steps executed**: %d\n\n", len(results))

	sb.WriteString("| # | URN | Status | Error |\n")
	sb.WriteString("|---|-----|--------|-------|\n")
	for i, r := range results {
		errCol := ""
		if r.Error != "" {
			errCol = r.Error
			if len(errCol) > 80 {
				errCol = errCol[:80] + "..."
			}
		}
		fmt.Fprintf(&sb, "| %d | %s | %s | %s |\n", i+1, r.URN, r.Status, errCol)
	}

	if len(warnings) > 0 {
		sb.WriteString("\n## Warnings\n")
		for _, w := range warnings {
			sb.WriteString("- " + w + "\n")
		}
	}

	// Append terminal REPORTING/SYNTHESIZER step outputs so the agent
	// receives the final reports (architecture diagrams, summary reports)
	// as content rather than discarding them.
	terminalStart := max(0, len(results)-3)
	for i := terminalStart; i < len(results); i++ {
		r := results[i]
		if r.Output != "" && r.Status == statusDone {
			fmt.Fprintf(&sb, "\n---\n## %s Output\n\n%s\n", r.URN, r.Output)
		}
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}},
	}
}

// savePipelineArtifacts persists key pipeline outputs to the raw resource store
// with predictable session-scoped keys for agent retrieval.
func savePipelineArtifacts(ctx context.Context, h *OrchestratorHandler, sessionID string, results []stepResult) map[string]string {
	if h.Store == nil || sessionID == "" {
		return nil
	}

	store := h.Store
	artifacts := make(map[string]string)

	// Save the last occurrence of each key output type.
	// Multiple generate_audit_report runs may occur (pre-mutation + post-mutation);
	// we want the final one.
	var lastPlan, lastAudit string
	for _, r := range results {
		if r.Status != statusDone || r.Output == "" {
			continue
		}
		switch {
		case strings.Contains(r.URN, "generate_implementation_plan"):
			lastPlan = r.Output
			// Attempt to resolve staging URI into raw markdown natively
			if stagingURI := extractStagingURI(r.Output); stagingURI != "" {
				if srv, ok := h.Registry.GetServer("go-modernizer"); ok && srv.Session != nil {
					ctxStaging, cancelStaging := context.WithTimeout(ctx, 10*time.Second)
					req := &mcp.ReadResourceParams{URI: stagingURI}
					if res, err := srv.Session.ReadResource(ctxStaging, req); err == nil && len(res.Contents) > 0 {
						if tc := res.Contents[0].Text; tc != "" {
							lastPlan = tc
						}
					}
					cancelStaging()
				}
			}
		case r.URN == urnMagictoolsGenerateAuditReport:
			lastAudit = r.Output
		}
	}

	if lastPlan != "" {
		key := "pipeline_" + sessionID + "_plan"
		if err := store.SaveRawResource(key, []byte(lastPlan)); err != nil {
			slog.Warn("savePipelineArtifacts: failed to save plan", keyError, err)
		} else {
			artifacts["implementation_plan"] = "mcp://magictools/raw/" + key
		}
	}

	if lastAudit != "" {
		key := "pipeline_" + sessionID + "_audit"
		if err := store.SaveRawResource(key, []byte(lastAudit)); err != nil {
			slog.Warn("savePipelineArtifacts: failed to save audit", keyError, err)
		} else {
			artifacts["audit_report"] = "mcp://magictools/raw/" + key
		}
	}

	// Per-step summary: compact JSON with status and output excerpt for each step.
	summary := buildStepSummaryJSON(results)
	summaryKey := "pipeline_" + sessionID + "_summary"
	if err := store.SaveRawResource(summaryKey, summary); err != nil {
		slog.Warn("savePipelineArtifacts: failed to save summary", keyError, err)
	} else {
		artifacts["step_summary"] = "mcp://magictools/raw/" + summaryKey
	}

	slog.Info("savePipelineArtifacts: artifacts persisted",
		keySessionID, sessionID, "artifact_count", len(artifacts))
	return artifacts
}

// buildStepSummaryJSON creates a compact JSON summary of all pipeline steps
// with status and a truncated output excerpt for each.
func buildStepSummaryJSON(results []stepResult) []byte {
	type stepSummary struct {
		Step    int    `json:"step"`
		URN     string `json:"urn"`
		Status  string `json:"status"`
		Error   string `json:"error,omitempty,omitzero"`
		Excerpt string `json:"excerpt,omitempty,omitzero"`
	}

	summaries := make([]stepSummary, 0, len(results))
	for i, r := range results {
		excerpt := r.Output
		if len(excerpt) > 500 {
			excerpt = excerpt[:500] + "..."
		}
		summaries = append(summaries, stepSummary{
			Step:    i + 1,
			URN:     r.URN,
			Status:  r.Status,
			Error:   r.Error,
			Excerpt: excerpt,
		})
	}

	data := marshalIndentOrEmpty(summaries)
	return data
}

// sha256Hash computes a SHA256 digest for a string, used for transition synergy keys.
func sha256Hash(s string) [32]byte {
	return sha256.Sum256([]byte(s))
}

// maxContextBytes is the hard ceiling for the cumulative context window.
// At 64KB, this is large enough to hold a full pipeline's diagnostic output
// while preventing unbounded memory growth in long-running DAGs.
const maxContextBytes = 128 * 1024

// enforceContextCap applies a sliding-window truncation to the context buffer
// when it exceeds maxContextBytes. It preserves the recall header (standards,
// history) and the most recent step outputs, truncating the oldest step bodies
// while keeping their section headers as breadcrumbs.
//
// Returns the (potentially truncated) context string for the next step.
func enforceContextCap(buf *strings.Builder, recallHeader string) string {
	if buf.Len() <= maxContextBytes {
		return buf.String()
	}

	full := buf.String()
	headerLen := len(recallHeader)

	// Target: keep recall header + last 48KB of step outputs.
	keepBytes := 48 * 1024
	keepFrom := max(len(full)-keepBytes, headerLen)

	// Scan forward to the next section boundary to avoid mid-sentence cuts.
	const sectionMarker = "\n---\n## Step"
	if idx := strings.Index(full[keepFrom:], sectionMarker); idx >= 0 {
		keepFrom += idx
	}

	// Also check for continuation step markers.
	const contMarker = "\n---\n## Continuation Step"
	if alt := strings.Index(full[keepFrom:], contMarker); alt >= 0 && alt < len(sectionMarker) {
		keepFrom += alt
	}

	truncated := full[:headerLen] +
		"\n\n> [!NOTE]\n> Earlier pipeline steps truncated for context budget.\n" +
		full[keepFrom:]

	buf.Reset()
	buf.WriteString(truncated)
	return truncated
}
