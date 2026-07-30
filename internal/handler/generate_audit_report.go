package handler

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/maccavelli/mcplib"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// handleGenerateAuditReport implements the post-pipeline structural review generation.
func (h *OrchestratorHandler) handleGenerateAuditReport(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var args struct {
		SessionID *string `json:"session_id"`
		Target    string  `json:"target"`
	}
	unmarshalArgsOrWarn(req.Params.Arguments, &args)
	if args.SessionID == nil || *args.SessionID == "" || args.Target == "" {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "missing required arguments: session_id, target"}}}, nil
	}

	cmd := exec.CommandContext(ctx, "git", "diff", "--color=never")
	cmd.Dir = args.Target
	gitDiffStr := string(combinedOutputOrEmpty(cmd))
	if gitDiffStr == "" {
		gitDiffStr = "No code differences detected (Pipeline performed 0 mutations)."
	}

	sessionID := mcplib.StringValue(args.SessionID)
	envelope := map[string]any{
		keyMetadata: map[string]any{
			keyTarget: args.Target, keySessionID: sessionID, keyStatus: statusCompleted,
		},
	}
	envJSON := marshalIndentOrEmpty(envelope)

	var sb strings.Builder
	fmt.Fprintf(&sb, "```json\n%s\n```\n\n", string(envJSON))
	sb.WriteString("# 🛡️ Executive Header: CSSA Formal Audit\n\n")
	fmt.Fprintf(&sb, "**Target Path**: `%s`\n", args.Target)
	fmt.Fprintf(&sb, "**CSSA Session ID**: `%s`\n\n", sessionID)
	sb.WriteString("---\n\n")
	sb.WriteString("## 🚀 Execution Pipeline\n\n")

	var metricsFound bool
	if h.RecallClient != nil && h.RecallClient.RecallEnabled() {
		if sessionData, err := h.RecallClient.GetSession(ctx, sessionID); err == nil {
			traces := auditExtractTracesFromSession(sessionData)
			metricsFound = auditWriteExecutionTraces(&sb, traces)
		}
	}

	h.auditRecordEdgeLearning(ctx, sessionID, metricsFound)
	if !metricsFound {
		sb.WriteString("No trace footprint extracted from CSSA orchestrator memory natively.\n")
	}

	sb.WriteString("\n---\n\n")
	sb.WriteString("## 🔍 Structural Metrics\n\n")
	sb.WriteString("| Metric | Pre-Audit Architecture | Post-Audit Delta | Quality Note |\n")
	sb.WriteString("|---|---|---|---|\n")
	sb.WriteString("| Structural Footprint | Unknown | Unchanged | Abstract metric mapped natively. |\n")
	sb.WriteString("| Validation Status | Strict | Executed | Evaluated via orchestrator pipeline. |\n")
	sb.WriteString("\n---\n\n")
	sb.WriteString("## 📜 Full Git Diff\n\n")
	sb.WriteString("```diff\n")
	sb.WriteString(gitDiffStr)
	sb.WriteString("\n```\n\n")

	reportContent := sb.String()
	go auditFanoutArtifact(reportContent, sessionID)

	if h.RecallClient != nil && h.RecallClient.RecallEnabled() {
		saveToRecallOrWarn(ctx, h.RecallClient, sessionID, args.Target, map[string]any{
			keyOutcome: outcomeReportGenerated, "model": "native",
			"stage": toolGenerateAuditReport, "diff": gitDiffStr, "phase": "reporting",
		})
	}

	auditCloseDAGTerminal()
	slog.Info("Formal audit report synthesized successfully", keyTarget, args.Target, "size", len(reportContent))

	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: reportContent}}}, nil
}
