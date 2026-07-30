package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

type alignToolsInput struct {
	Query              string
	ServerName         string
	Category           string
	FullSchema         *bool
	Arguments          map[string]any
	BypassMinification bool
}

func (h *OrchestratorHandler) alignTryInlineExecution(
	ctx context.Context,
	req *mcp.CallToolRequest,
	args alignToolsInput,
	results []*db.ToolRecord,
) (*mcp.CallToolResult, string) {
	if len(args.Arguments) == 0 || len(results) == 0 {
		return nil, ""
	}
	topResult := results[0]
	gap := h.Config.ConfidenceGap
	if gap == 0 {
		gap = config.Intelligence.ConfidenceGap
	}
	isMutative := alignIsMutativeTool(topResult)
	if !isMutative {
		gap = math.Max(gap*0.75, 0.10)
	}
	clearSeparation := len(results) == 1 ||
		(len(results) > 1 && topResult.ConfidenceScore-results[1].ConfidenceScore > gap)
	if !isMutative && clearSeparation && len(results) > 1 {
		telemetry.SearchMetrics.DynamicGapPassesSafe.Add(1)
	}
	serverTrusted := slices.Contains(h.Config.GetTrustServers(), topResult.Server)
	inlineMinConfidence := h.Config.ScoreThreshold
	if inlineMinConfidence <= 0 {
		inlineMinConfidence = 0.5
	}
	confident := topResult.ConfidenceScore >= inlineMinConfidence
	if !clearSeparation || !confident || !serverTrusted || topResult.IsNative {
		return nil, alignInlineSkipReason(clearSeparation, confident, serverTrusted)
	}
	ps := NewProxyService(h)
	server, name, resolvedURN, toolRecord, resolveErr := ps.ResolveURN(ctx, topResult.URN)
	if resolveErr != nil {
		telemetry.ProxyOptimization.InlineSkipped.Add(1)
		slog.Info("align_tools: inline execution skipped, falling back to discovery", "reason", "resolve_error", "results", len(results))
		return nil, "resolve_error"
	}
	if h.Config.ValidateProxyCalls && toolRecord != nil {
		if valErr := ps.ValidateArguments(ctx, resolvedURN, toolRecord, args.Arguments); valErr != nil {
			slog.Warn("align_tools: inline execution validation failed, falling back to discovery", keyURN, resolvedURN, keyError, valErr)
			telemetry.ProxyOptimization.InlineSkipped.Add(1)
			slog.Info("align_tools: inline execution skipped, falling back to discovery", "reason", "validation_failure", "results", len(results))
			return nil, "validation_failure"
		}
	}
	telemetry.ProxyOptimization.InlineExecutions.Add(1)
	telemetry.ProxyOptimization.Tier1AutoExecutions.Add(1)
	result, execErr := h.executeProxyPipeline(ctx, ps, args.Arguments, req, args.BypassMinification, server, name, resolvedURN, toolRecord)
	if execErr == nil {
		return result, ""
	}
	slog.Warn("align_tools: inline execution failed, falling back to discovery", keyURN, resolvedURN, keyError, execErr)
	telemetry.ProxyOptimization.InlineSkipped.Add(1)
	slog.Info("align_tools: inline execution skipped, falling back to discovery", "reason", "execution_error", "results", len(results))
	return nil, "execution_error"
}

func alignIsMutativeTool(top *db.ToolRecord) bool {
	if top.Role == roleMutator {
		return true
	}
	mutativeVerbs := []string{
		verbDelete, "write", "create", verbUpdate, "remove", "drop", "purge",
		"destroy", "reset", "set", "put", "patch", "overwrite", "rm", "truncate", "clear",
	}
	nameLower := strings.ToLower(top.Name)
	for _, v := range mutativeVerbs {
		if strings.Contains(nameLower, v) {
			return true
		}
	}
	return false
}

func alignInlineSkipReason(clearSeparation, confident, serverTrusted bool) string {
	var reason string
	switch {
	case !clearSeparation:
		reason = "multiple_matches"
	case !confident:
		reason = "low_confidence"
	case !serverTrusted:
		reason = "untrusted_server"
	default:
		reason = "native_tool"
	}
	telemetry.ProxyOptimization.InlineSkipped.Add(1)
	slog.Info("align_tools: inline execution skipped, falling back to discovery", "reason", reason)
	return reason
}

func (h *OrchestratorHandler) alignBuildDiscoveryResponse(
	ctx context.Context,
	req *mcp.CallToolRequest,
	results []*db.ToolRecord,
	showFullSchema bool,
	inlineSkipReason string,
) *mcp.CallToolResult {
	var text strings.Builder
	lruUpdated := false
	corrID := uuid.New().String()
	envelope := struct {
		Metadata map[string]any      `json:"metadata"`
		Tools    []map[string]string `json:"tools"`
	}{
		Metadata: make(map[string]any),
		Tools:    make([]map[string]string, 0),
	}
	envelope.Metadata["proxy_correlation_id"] = corrID
	nativeSchemas := make(map[string]map[string]any)
	h.toolsMu.RLock()
	for _, it := range h.InternalTools {
		nativeSchemas[it.Name] = h.toSchemaMap(it.InputSchema)
	}
	h.toolsMu.RUnlock()
	for i, r := range results {
		if i >= h.Config.AlignMaxResults {
			break
		}
		schema, updated := h.alignPrepareToolSchema(ctx, req, r, nativeSchemas, showFullSchema, results[i])
		if updated {
			lruUpdated = true
		}
		toolEntry := alignToolEntry(r, schema, showFullSchema)
		metricEvent := telemetry.ProxyMetricEvent{
			Timestamp:          time.Now().UTC(),
			ProxyCorrelationID: corrID,
			ToolURN:            r.URN,
			ServerName:         r.Server,
			IntentClass:        r.Category,
			ResolutionType:     "discovery_fallback",
			Tags:               []string{"server:" + r.Server, "intent:" + r.Category, "resolution:discovery_fallback"},
		}
		metricEvent.Metrics.Alignment.FusedScore = r.ConfidenceScore
		metricEvent.Metrics.Alignment.ConfidenceGap = h.Config.ConfidenceGap
		telemetry.DispatchProxyMetric(metricEvent)
		envelope.Tools = append(envelope.Tools, toolEntry)
		alignWriteToolDescription(&text, r, schema, showFullSchema)
	}
	envelope.Metadata["mode"] = "discovery"
	envelope.Metadata["intent_match_count"] = len(results)
	envelope.Metadata["system_prompt_updated"] = lruUpdated
	if inlineSkipReason != "" {
		envelope.Metadata["inline_execution_skipped"] = true
		envelope.Metadata["skip_reason"] = inlineSkipReason
	}
	if lruUpdated && h.Server != nil {
		slog.Log(ctx, util.LevelTrace, "align_tools: triggering tools/list_changed notification")
		dummy := &mcp.Tool{
			Name:        "__magic_lru_sync__",
			Description: "Internal synchronization signal",
			InputSchema: map[string]any{keyType: schemaTypeObject},
		}
		h.Server.AddTool(dummy, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return nil, nil
		})
		h.Server.RemoveTools("__magic_lru_sync__")
	}
	envJSON := marshalIndentOrEmpty(envelope)
	finalText := fmt.Sprintf("```json\n%s\n```\n\n### Descriptions\n\n%s", string(envJSON), text.String())
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: finalText}}}
}

func (h *OrchestratorHandler) alignPrepareToolSchema(
	ctx context.Context,
	req *mcp.CallToolRequest,
	r *db.ToolRecord,
	nativeSchemas map[string]map[string]any,
	showFullSchema bool,
	rec *db.ToolRecord,
) (map[string]any, bool) {
	if r.IsNative {
		return nativeSchemas[r.Name], false
	}
	schema := h.Store.ResolveToolSchema(r)
	if schema == nil {
		slog.Warn("gateway: failed to resolve schema", keyURN, r.URN, "hash", r.SchemaHash)
		return nil, false
	}
	rec.InputSchema = schema
	rec.ZeroValues = db.ComputeZeroValues(schema)
	t := &mcp.Tool{Name: r.URN, Description: r.Description, InputSchema: schema}
	if !strings.Contains(t.Name, ":") {
		t.Name = fmt.Sprintf("%s:%s", r.Server, r.Name)
	}
	t = util.SanitizeToolSchema(t)
	sessionID := safeSessionID(req)
	if sessionID == "" {
		sessionID = "active-session"
	}
	var lruCache *util.S3FIFOCache[string, *mcp.Tool]
	if lruInterface, ok := h.ActiveSessions.Load(sessionID); ok {
		lruCache = lruCacheFromAny(lruInterface)
	} else {
		lruCache = util.NewS3FIFOCache[string, *mcp.Tool](h.Config.SessionLRUSize)
		h.ActiveSessions.Store(sessionID, lruCache)
	}
	lruCache.Add(r.URN, t)
	_ = showFullSchema
	_ = ctx
	return schema, true
}

func alignToolEntry(r *db.ToolRecord, schema map[string]any, showFullSchema bool) map[string]string {
	schemaStatus := "Loaded directly into System Prompt"
	if showFullSchema {
		schemaStatus = "Included in Description payload below"
	}
	entry := map[string]string{
		keyURN: r.URN, "server": r.Server, "category": r.Category, "schema_status": schemaStatus,
	}
	if !r.IsNative && r.InputSchema != nil {
		if tmpl := buildCallTemplate(r); tmpl != nil {
			entry["call_template"] = string(marshalJSONOrEmpty(tmpl))
			telemetry.ProxyOptimization.TemplatesServed.Add(1)
		}
	}
	_ = schema
	return entry
}

func alignWriteToolDescription(text *strings.Builder, r *db.ToolRecord, schema map[string]any, showFullSchema bool) {
	if showFullSchema {
		schemaJSON := marshalIndentOrEmpty(schema)
		fmt.Fprintf(text, "**[%s]**\nDescription: %s\nInputSchema:\n```json\n%s\n```\n\n", r.URN, r.Description, string(schemaJSON))
		return
	}
	fmt.Fprintf(text, "**[%s]**\nDescription: %s\n", r.URN, r.Description)
	if schema != nil {
		if summary := FormatCompactSchema(schema); summary != "" {
			text.WriteString(summary)
		}
	}
	text.WriteString("\n")
}
