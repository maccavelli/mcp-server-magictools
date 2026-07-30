package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/intelligence"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *OrchestratorHandler) proxyExecuteLoopback(
	ctx context.Context,
	req *mcp.CallToolRequest,
	name string,
	arguments map[string]any,
	urn string,
	handler func(context.Context, *mcp.CallToolRequest) (*mcp.CallToolResult, error),
) (*mcp.CallToolResult, error) {
	slog.Info("gateway: loopback dispatch", "tool", name)
	telemetry.GlobalDAGTracker.UpdateActiveNode(urn, 0, 0, 0, "MISS", "")

	unwrappedBytes := marshalJSONOrEmpty(arguments)
	nativeReq := *req
	paramsCopy := *req.Params
	paramsCopy.Name = name
	paramsCopy.Arguments = unwrappedBytes
	nativeReq.Params = &paramsCopy

	res, err := handler(ctx, &nativeReq)
	var resSize int64
	if res != nil {
		var structBytes []byte
		if res.StructuredContent != nil {
			structBytes = marshalJSONOrEmpty(res.StructuredContent)
		}
		resSize = measureResponseSize(res, structBytes)
	}
	if err == nil && (res == nil || !res.IsError) {
		telemetry.GlobalDAGTracker.UpdateActiveNode(urn, 0, resSize, resSize, "HIT", "")
		telemetry.GlobalDAGTracker.CompleteNode(urn, true)
	} else {
		telemetry.GlobalDAGTracker.RecordFault(urn, "halt", 1, 1, "")
		telemetry.GlobalDAGTracker.CompleteNode(urn, false)
	}
	return res, err
}

func (h *OrchestratorHandler) proxyCheckResponseCache(urn string, arguments map[string]any) (*mcp.CallToolResult, string, bool) {
	if !h.isSafeToCache(urn) {
		return nil, "", false
	}
	respCacheKey := h.getCacheKey(urn, arguments)
	if cached, ok := h.Responses.Get(respCacheKey); ok {
		return cached, respCacheKey, true
	}
	return nil, respCacheKey, false
}

func (h *OrchestratorHandler) proxyEvaluateSqueezeBypass(urn string, bypassMinification bool) bool {
	if bypassMinification {
		return true
	}
	for _, target := range h.Config.GetSqueezeBypass() {
		if isTargetMatched(urn, target) {
			telemetry.OptMetrics.SqueezeBypassCount.Add(1)
			slog.Debug("gateway: squeeze bypass activated", keyURN, urn, "bypass_list", h.Config.GetSqueezeBypass())
			return true
		}
	}
	return false
}

func (h *OrchestratorHandler) proxyIsRingBufferTarget(urn string) bool {
	for _, target := range h.Config.GetRingBufferTargets() {
		if isTargetMatched(urn, target) {
			return true
		}
	}
	return false
}

func responseContainsMutationRequired(res *mcp.CallToolResult) bool {
	if res.StructuredContent != nil {
		if structBytes, err := json.Marshal(res.StructuredContent); err == nil {
			if bytes.Contains(structBytes, []byte(`"mutation_required":true`)) ||
				bytes.Contains(structBytes, []byte(`"mutation_required": true`)) {
				return true
			}
		}
	}
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			if strings.Contains(tc.Text, `"mutation_required":true`) ||
				strings.Contains(tc.Text, `"mutation_required": true`) {
				return true
			}
		}
	}
	return false
}

func (h *OrchestratorHandler) proxyHandleMutationMandate(
	ps *ProxyService,
	urn string,
	res *mcp.CallToolResult,
	bypassMinification bool,
	rawSize int64,
) {
	if bypassMinification || rawSize <= 0 || !responseContainsMutationRequired(res) {
		return
	}
	depth := telemetry.GlobalDAGTracker.IncrementMutationDepth()
	if depth > 3 {
		slog.Error("gateway: Topological Evolution bound exceeded (max 3), breaking infinite DAG loop")
		return
	}
	slog.Warn("gateway: mid-pipeline mutation mandate detected natively. Synthesizing structural evolution.", "depth", depth)
	socraticNodes := []string{
		"brainstorm:brainstorm_complexity_forecaster",
		urnBrainstormAntithesisSkeptic,
		"brainstorm:architectural_diagrammer",
		urnGoModernizerGenerateImplPlan,
	}
	telemetry.GlobalDAGTracker.SpliceNodes(urn, socraticNodes)
	snapshot := telemetry.GlobalDAGTracker.Snapshot()
	sid, ok := snapshot[keySessionID].(string)
	if !ok || sid == "" {
		return
	}
	go func(sessionID string, state map[string]any) { //nolint:gosec // G118: detached recall commit with bounded timeout
		bgCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		stateJSON := marshalJSONOrEmpty(state)
		projectID := stringFrom(state[keyTarget])
		args := map[string]any{
			keyServerID:  serverMagictools,
			keyProjectID: projectID,
			keySessionID: sessionID,
			keyOutcome:   outcomeDAGState,
			keyStateData: string(stateJSON),
		}
		if _, err := ps.ExecuteProxy(bgCtx, sourceRecall, "save_to_recall", args, 10*time.Second); err != nil {
			slog.Warn("gateway: failed to async commit DAG state to recall sessions", keyError, err)
		}
	}(sid, snapshot)
}

func (h *OrchestratorHandler) proxyRecordDispatchFailure(
	server, name, urn, corrID string,
	req *mcp.CallToolRequest,
	toolLatency int64,
	err error,
	sourceServer string,
) {
	telemetry.ErrorTaxonomy.Classify(err)
	telemetry.RecentErrors.Record(server, corrID, err.Error())
	telemetry.GlobalToolTracker.Record(urn, toolLatency, true)
	telemetry.GlobalRouteTracker.RecordRoute(sourceServer, server, true)
	sentSize := int64(len(req.Params.Arguments))
	h.Telemetry.AddBytes(server, sentSize, 0, 0)
	h.Telemetry.RecordFault(server)
	slog.Error("tool complete", "component", "backplane", "server", server, "tool", name, keyURN, urn,
		"latency_ms", toolLatency, keyStatus, "error", keyError, err, "corr_id", corrID)
	telemetry.GlobalDAGTracker.RecordFault(urn, "halt", 1, 1, "")
	telemetry.GlobalDAGTracker.CompleteNode(urn, false)
	submitBackground(func() {
		h.Store.IncrementToolCalls(urn, toolLatency)
		h.Store.RecordToolError(urn, intelligence.ClassifyError(err.Error()))
	})
}

func (h *OrchestratorHandler) proxyCheckTokenBudget() *mcp.CallToolResult {
	if h.Config.TokenSpendThresh <= 0 || telemetry.GetTotalTokens() < int64(h.Config.TokenSpendThresh) {
		return nil
	}
	slog.Warn("gateway: circuit breaker triggered - global token budget exceeded", "threshold", h.Config.TokenSpendThresh)
	return &mcp.CallToolResult{
		IsError: true,
		Content: append([]mcp.Content{}, &mcp.TextContent{Text: fmt.Sprintf(
			"🛡️ ORCHESTRATOR MUZZLE EXCEPTION: Global session LLM token boundary heavily exceeded (%d / %d). Terminating runaway pipeline natively to prevent runaway budget burns.",
			telemetry.GetTotalTokens(), h.Config.TokenSpendThresh,
		)}),
	}
}

func (h *OrchestratorHandler) proxyBeginDispatchTrace(ctx context.Context, server, name, corrID string) (context.Context, string) {
	parentID := telemetry.GetActiveCascadeParent()
	sourceServer := telemetry.GetActiveCascadeSource()
	if telemetry.GlobalRingBuffer != nil {
		spanJSON := fmt.Sprintf(`{"type":"SPAN_START","trace_id":%q,"parent_id":%q,"server":%q,"tool":%q,"start_time":%d}`,
			corrID, parentID, server, name, time.Now().UnixMilli())
		telemetry.GlobalRingBuffer.WriteRecord([]byte(spanJSON))
	}
	telemetry.RecordActiveDispatch(server, corrID)
	return telemetry.WithCorrelationID(ctx, corrID), sourceServer
}

func proxyEndDispatchTrace(corrID string, toolLatency int64) {
	if telemetry.GlobalRingBuffer != nil {
		endJSON := fmt.Sprintf(`{"type":"SPAN_END","trace_id":%q,"latency_ms":%d}`, corrID, toolLatency)
		telemetry.GlobalRingBuffer.WriteRecord([]byte(endJSON))
	}
}

func proxyExtractDiagnosticSizes(res *mcp.CallToolResult) (rawSize, postSize int64) {
	if res == nil || res.Meta == nil {
		return 0, 0
	}
	diag, ok := res.Meta["_diagnostics"].(map[string]any)
	if !ok {
		return 0, 0
	}
	if rs, ok := diag["raw_bytes"].(int64); ok {
		rawSize = rs
	}
	if ps, ok := diag["post_bytes"].(int64); ok {
		postSize = ps
	}
	return rawSize, postSize
}

func (h *OrchestratorHandler) proxyFinalizeSuccess(
	server, name, urn, corrID, respCacheKey string,
	cacheable bool,
	res *mcp.CallToolResult,
	hotStart time.Time,
) {
	var rawSize, postSize int64
	if res != nil {
		rawSize, postSize = proxyExtractDiagnosticSizes(res)
	}
	telemetry.GlobalDAGTracker.UpdateActiveNode(urn, telemetry.GetTotalTokens(), rawSize, postSize, "MISS", "")
	telemetry.GlobalDAGTracker.CompleteNode(urn, true)
	telemetry.MetaLatencies.CallProxyHot.Record(time.Since(hotStart).Milliseconds())
	if cacheable && respCacheKey != "" && res != nil {
		h.Responses.Set(respCacheKey, res, proxyResponseCacheTTL)
	}
	_ = server
	_ = name
	_ = corrID
}
