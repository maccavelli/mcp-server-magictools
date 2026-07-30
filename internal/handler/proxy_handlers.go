package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// isSearchNoMatchErr reports soft search misses that align_tools may treat as empty discovery.
func isSearchNoMatchErr(err error) bool {
	return errors.Is(err, db.ErrGatedNoMatch)
}

func sortResultsByConfidence(results []*db.ToolRecord) {
	sort.Slice(results, func(i, j int) bool {
		return results[i].ConfidenceScore > results[j].ConfidenceScore
	})
}

func noLocalToolMatchPayload(query string) string {
	payload := marshalJSONOrEmpty(map[string]string{
		keyStatus: proxyStatusNoLocalToolMatch,
		keyQuery:  query,
		"hint":    "No tool exceeded confidence gates. Try a more specific intent or set server_name.",
	})
	return string(payload)
}

// isAmbiguousURNFastPath reports when server_name + query would resolve to a misleading
// exact URN (e.g. query sourceRecall + server sourceRecall → recall:recall) instead of search.
func isAmbiguousURNFastPath(serverName, query, resolvedURN string) bool {
	if serverName == "" || query == "" {
		return false
	}
	qLower := strings.ToLower(strings.TrimSpace(query))
	sLower := strings.ToLower(strings.TrimSpace(serverName))
	if qLower == sLower {
		return true
	}
	if strings.EqualFold(resolvedURN, sLower+":"+sLower) {
		return true
	}
	if strings.Contains(query, ":") || strings.Contains(query, "_") {
		return false
	}
	return false
}

// registerProxyTools delegates registration to specialized tool logic.
func (h *OrchestratorHandler) registerProxyTools(s *mcp.Server) {
	// Descriptions sourced from inventory.go via addTool().
	h.addTool(s, &mcp.Tool{Name: toolAlignTools}, h.AlignTools)
	h.addTool(s, &mcp.Tool{Name: toolCallProxy}, h.CallProxy)
}

func (h *OrchestratorHandler) triggerAutoHeal() {
	if telemetry.SyncOutOfSync.Load() {
		if telemetry.IsHealing.CompareAndSwap(false, true) {
			slog.Warn("gateway: auto-healing triggered for OUT_OF_SYNC database")
			go func() {
				defer telemetry.IsHealing.Store(false)
				if err := h.Store.ReindexAllTools(); err == nil {
					telemetry.SyncOutOfSync.Store(false)
				} else {
					slog.Error("gateway: auto-healing failed", keyError, err)
				}
			}()
		}
	}
}

// AlignTools is undocumented but satisfies standard structural requirements.
// safeSessionID extracts the MCP session ID, returning "" if there is no session
// or the session is only partially constructed. ALIGN-7: ServerSession.ID() can
// nil-deref on a session without a live connection, so the call is recovered.
func safeSessionID(req *mcp.CallToolRequest) (id string) {
	defer recoverOrWarn()
	return req.GetSession().ID()
}

func (h *OrchestratorHandler) AlignTools(ctx context.Context, req *mcp.CallToolRequest) (resOut *mcp.CallToolResult, errOut error) {
	defer func(start time.Time) {
		telemetry.MetaLatencies.AlignTools.Record(time.Since(start).Milliseconds())
	}(time.Now())
	// ALIGN-7: local panic recovery so a malformed/edge request degrades to an
	// error result instead of relying on SDK-level recovery.
	defer func() {
		if r := recover(); r != nil {
			slog.Error("align_tools: panic recovered", "panic", r, "trace", string(debug.Stack()))
			resOut, errOut = nil, fmt.Errorf("align_tools internal error: %v", r)
		}
	}()

	h.triggerAutoHeal()

	var args struct {
		Query              string         `json:"query"`
		ServerName         string         `json:"server_name,omitempty,omitzero"`
		Category           string         `json:"category,omitempty,omitzero"`
		FullSchema         *bool          `json:"full_schema,omitempty,omitzero"`
		Arguments          map[string]any `json:"arguments,omitempty,omitzero"`
		BypassMinification bool           `json:"bypass_minification,omitempty,omitzero"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
		ps := NewProxyService(h)
		if repairErr := ps.repairSimpleArgs(req.Params.Arguments, &args); repairErr != nil {
			return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
		}
	}

	showFullSchema := true
	if args.FullSchema != nil {
		showFullSchema = *args.FullSchema
	}

	// ALIGN-10: a fully-unconstrained align (no query, server, or category) would
	// scan and cache the entire tool table. The query is required by the schema.
	if strings.TrimSpace(args.Query) == "" && args.ServerName == "" && args.Category == "" {
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "align_tools requires a non-empty 'query' (or a server_name/category filter)."}},
		}, nil
	}

	// 🛡️ INTENT PRE-FILTER: Use as boost signal, not hard constraint.
	// If the constrained search returns empty, retry without the constraint.
	var preferredServer string
	if args.ServerName == "" && args.Query != "" {
		preferredServers := h.Store.AnalyzeIntent(ctx, args.Query)
		if len(preferredServers) > 0 {
			preferredServer = preferredServers[0]
			args.ServerName = preferredServer
			slog.Log(ctx, util.LevelTrace, "[HANDLER] align_tools intent pre-filter", "injected_server", args.ServerName)
		}
	}

	slog.Log(ctx, util.LevelTrace, "[HANDLER] align_tools entry", keyQuery, args.Query, "server_name", args.ServerName, "category", args.Category)

	alignInput := alignToolsInput{
		Query: args.Query, ServerName: args.ServerName, Category: args.Category,
		FullSchema: args.FullSchema, Arguments: args.Arguments, BypassMinification: args.BypassMinification,
	}
	results, err := h.alignToolsResolveResults(ctx, &alignInput, showFullSchema, preferredServer)
	if err != nil {
		return nil, err
	}
	args.ServerName = alignInput.ServerName

	inlineResult, inlineSkipReason := h.alignTryInlineExecution(ctx, req, alignInput, results)
	if inlineResult != nil {
		return inlineResult, nil
	}

	if len(results) == 0 {
		text := "No specific tool found."
		if args.Query != "" {
			text = noLocalToolMatchPayload(args.Query)
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}, nil
	}

	return h.alignBuildDiscoveryResponse(ctx, req, results, showFullSchema, inlineSkipReason), nil
}

// buildCallTemplate generates a ready-to-use call_proxy template from a ToolRecord.
// Required fields are OMITTED from arguments and listed in required_missing.
// Optional fields are pre-filled with ZeroValues to reduce agent guesswork.
func buildCallTemplate(record *db.ToolRecord) map[string]any {
	if record == nil || record.InputSchema == nil {
		return nil
	}

	// ALIGN-4: templates are required-only. The previous optional-field pre-fill
	// was dead code — ComputeZeroValues only populates required keys, which are not
	// pre-filled — so arguments is intentionally empty; the agent fills the required
	// fields, guided by required_missing below.
	// ALIGN-9: iterate the schema's `required` array for deterministic ordering
	// (map iteration order is randomized).
	props := mapFrom(record.InputSchema[keyProperties])
	var missingRequired []string
	if req, ok := record.InputSchema[keyRequired].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				if _, inProps := props[s]; inProps {
					missingRequired = append(missingRequired, s)
				}
			}
		}
	}

	template := map[string]any{
		keyURN:       record.URN,
		keyArguments: map[string]any{},
	}
	if len(missingRequired) > 0 {
		// 🛡️ ENRICHED HINTS: type, description, and enum for each required field.
		template["required_missing"] = FormatRequiredMissingHints(record.InputSchema, missingRequired)
	}
	return template
}

// CallProxy is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) CallProxy(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	callStart := time.Now()
	defer func() {
		telemetry.MetaLatencies.CallProxy.Record(time.Since(callStart).Milliseconds())
	}()

	h.triggerAutoHeal()

	ps := NewProxyService(h)
	params, err := ps.unmarshalCallProxyArgs(req.Params.Arguments)
	if err != nil {
		return nil, err
	}

	server, name, urn, toolRecord, err := ps.ResolveURN(ctx, params.URN)
	if err != nil {
		telemetry.GlobalRouteTracker.InvalidRoutes.Add(1)
		return nil, err
	}
	slog.Log(ctx, util.LevelTrace, "gateway: call_proxy start", "server", server, "tool", name)

	if server != serverMagictools {
		submitBackground(func() {
			h.Store.UpdateToolUsage(urn)
		})
	}

	if h.Config.ValidateProxyCalls {
		if err := ps.ValidateArguments(ctx, urn, toolRecord, params.Arguments); err != nil {
			telemetry.GlobalRouteTracker.InvalidRoutes.Add(1)
			slog.Warn("gateway: pre-flight firewall trapped hallucination", "tool", name, keyError, err)
			return &mcp.CallToolResult{
				IsError: true,
				Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}},
			}, nil
		}
	} else {
		slog.Log(ctx, util.LevelTrace, "gateway: proxy validation disabled by config", "tool", name)
		// PROXY-L5: still apply safe coercion/strip when validation is off, so type
		// mismatches and reserved/extra keys don't go raw to sub-servers with
		// additionalProperties:false (CoerceAndStrip only normalizes; it never rejects).
		if toolRecord != nil {
			ps.hydrateRecord(toolRecord)
			ps.CoerceAndStrip(toolRecord, params.Arguments)
		}
	}

	return h.executeProxyPipeline(ctx, ps, params.Arguments, req, params.BypassMinification, server, name, urn, toolRecord)
}

func (h *OrchestratorHandler) executeProxyPipeline(ctx context.Context, ps *ProxyService, arguments map[string]any, req *mcp.CallToolRequest, bypassMinification bool, server, name, urn string, toolRecord *db.ToolRecord) (*mcp.CallToolResult, error) {
	callStart := time.Now()
	corrID := stripReservedKeys(arguments)

	defer func() {
		if corrID != "" && server != serverMagictools {
			execEvent := telemetry.ProxyMetricEvent{
				Timestamp:          time.Now().UTC(),
				ProxyCorrelationID: corrID,
				ToolURN:            urn,
				ServerName:         server,
				IntentClass:        "unknown",
				ResolutionType:     "execution",
				Tags:               []string{"server:" + server, "resolution:execution"},
			}
			if toolRecord != nil {
				execEvent.IntentClass = toolRecord.Category
				execEvent.Tags = []string{"server:" + server, "intent:" + toolRecord.Category, "resolution:execution"}
			}
			execEvent.Metrics.Execution.ExecutionLatencyMs = time.Since(callStart).Milliseconds()
			telemetry.DispatchProxyMetric(execEvent)
		}
	}()

	if server == serverMagictools {
		if name == toolAlignTools || name == toolCallProxy {
			return nil, fmt.Errorf("recursion_guard: tool %q cannot self-dispatch via loopback — use the resolved sub-server tool directly", name)
		}
		if handler, ok := h.loopbackHandlers[name]; ok {
			return h.proxyExecuteLoopback(ctx, req, name, arguments, urn, handler)
		}
		telemetry.GlobalRouteTracker.InvalidRoutes.Add(1)
		return nil, fmt.Errorf("unknown internal tool: %s", name)
	}

	cacheable := h.isSafeToCache(urn)
	cached, respCacheKey, hit := h.proxyCheckResponseCache(urn, arguments)
	if hit {
		h.Telemetry.AddLatency(server, 0)
		telemetry.GlobalDAGTracker.UpdateActiveNode(urn, 0, 0, 0, "HIT", respCacheKey)
		telemetry.GlobalDAGTracker.CompleteNode(urn, true)
		return cached, nil
	}

	bootLatency, bootErr := ps.EnsureServerReady(ctx, server)
	if bootErr != nil {
		return nil, fmt.Errorf("failed to lazy-boot sub-server %s: %w", server, bootErr)
	}
	h.Telemetry.AddLatency(server, bootLatency)
	if bootLatency > 0 {
		telemetry.MetaLatencies.BootLatency.Record(bootLatency)
	}
	hotStart := time.Now()

	if corrID == "" {
		corrID = telemetry.NewCorrelationID()
	}
	ctx, sourceServer := h.proxyBeginDispatchTrace(ctx, server, name, corrID)
	defer telemetry.ClearActiveDispatch(server)

	slog.Info("tool dispatch", "component", "backplane", "server", server, "tool", name, keyURN, urn, "corr_id", corrID)

	timeout := ResolveTimeout(toolRecord)
	if toolRecord != nil && toolRecord.Role == roleMutator {
		slog.Info("gateway: MUTATOR timeout escalation", keyURN, urn, "timeout_s", int(timeout.Seconds()))
	}
	if budgetRes := h.proxyCheckTokenBudget(); budgetRes != nil {
		return budgetRes, nil
	}

	telemetry.GlobalDAGTracker.UpdateActiveNode(urn, 0, 0, 0, "MISS", "")
	res, err := ps.ExecuteProxy(ctx, server, name, arguments, timeout)
	toolLatency := time.Since(hotStart).Milliseconds()
	proxyEndDispatchTrace(corrID, toolLatency)
	if err != nil {
		h.proxyRecordDispatchFailure(server, name, urn, corrID, req, toolLatency, err, sourceServer)
		return nil, fmt.Errorf("invoke proxy error (%s): %w", urn, err)
	}

	slog.Info("tool complete", "component", "backplane", "server", server, "tool", name, keyURN, urn,
		"latency_ms", toolLatency, keyStatus, "ok", "corr_id", corrID)
	telemetry.GlobalToolTracker.Record(urn, toolLatency, false)
	telemetry.GlobalRouteTracker.RecordRoute(sourceServer, server, false)
	submitBackground(func() { h.Store.IncrementToolCalls(urn, toolLatency) })

	if tier2Res := h.interceptTier2HFSC(ctx, res, server); tier2Res != nil {
		return tier2Res, nil
	}

	bypassMinification = h.proxyEvaluateSqueezeBypass(urn, bypassMinification)
	if !bypassMinification {
		slog.Log(ctx, util.LevelTrace, "gateway: not in squeeze bypass list", keyURN, urn)
	}
	if h.proxyIsRingBufferTarget(urn) && !bypassMinification && (len(res.Content) > 0 || res.StructuredContent != nil) {
		if tier1Res := h.interceptTier1Native(res, server, name); tier1Res != nil {
			return tier1Res, nil
		}
	}

	res, err = ps.PostProcessResponse(ctx, res, server, name, urn, toolRecord, arguments)
	if err != nil {
		return nil, err
	}

	rawSize, _ := proxyExtractDiagnosticSizes(res)
	h.proxyHandleMutationMandate(ps, urn, res, bypassMinification, rawSize)
	h.proxyFinalizeSuccess(server, name, urn, corrID, respCacheKey, cacheable, res, hotStart)
	return res, nil
}

func transformToHybrid(rawJSON []byte, tokenLimit int) string {
	var m map[string]any
	if err := json.Unmarshal(rawJSON, &m); err != nil {
		return "## Tool Result\n- **Error**: Failed to decode sub-server JSON response."
	}

	var sb strings.Builder
	var headers []string
	summaryKeys := []string{keyStatus, "count", keyError, "message", "summary", keyResultCount, keyOutcome, keySuccess}
	for _, key := range summaryKeys {
		if v, ok := getIgnoreCase(m, key); ok {
			label := strings.ToUpper(key[:1]) + key[1:]
			if key == keyResultCount {
				label = "Count"
			}
			headers = append(headers, fmt.Sprintf("- **%s**: %v", label, v))
		}
	}

	if len(headers) > 0 {
		sb.WriteString("### Summary\n")
		sb.WriteString(strings.Join(headers, "\n"))
		sb.WriteString("\n\n")
	} else {
		sb.WriteString("## Tool Result\n")
	}

	var metadata []string
	metaKeywords := []string{"id", "timestamp", "version", keyURN, keyType, "created", "modified", "author", "hash", "uuid"}
	var toDelete []string
	for k, v := range m {
		kLower := strings.ToLower(k)
		isMeta := false
		for _, kw := range metaKeywords {
			if strings.Contains(kLower, kw) {
				isMeta = true
				break
			}
		}
		if isMeta {
			label := cases.Title(language.English).String(strings.ReplaceAll(k, "_", " "))
			metadata = append(metadata, fmt.Sprintf("- **%s**: %v", label, v))
			toDelete = append(toDelete, k)
		}
	}
	for _, k := range toDelete {
		delete(m, k)
	}

	if len(metadata) > 0 {
		sb.WriteString("#### Metadata\n")
		sb.WriteString(strings.Join(metadata, "\n"))
		sb.WriteString("\n\n")
	}

	if len(m) > 0 {
		maxTokens := 1000
		if tokenLimit > 0 {
			maxTokens = tokenLimit
		}
		charLimit := maxTokens * 4
		dataJSON, dataErr := json.MarshalIndent(m, "", "  ")
		if dataErr != nil {
			slog.Warn("gateway: transformToHybrid failed to marshal subset", keyError, dataErr)
			dataJSON = fmt.Appendf(nil, `{"error": "failed to encode: %v"}`, dataErr)
		}

		if len(dataJSON) > charLimit {
			sb.WriteString("> [!IMPORTANT]\n")
			// PROXY-L4: don't print a literal get_raw(id=...) — the real call id isn't
			// known here; MinifyResponse appends the usable read-tool footer downstream.
			sb.WriteString("> [Data too large to inline; use the read-tool footer below to fetch the full payload.]\n")
		} else {
			sb.WriteString("```json:data\n")
			sb.WriteString(string(dataJSON))
			sb.WriteString("\n```")
		}
	}

	return sb.String()
}

func getIgnoreCase(m map[string]any, target string) (any, bool) {
	targetLower := strings.ToLower(target)
	for k, v := range m {
		if strings.EqualFold(k, targetLower) {
			delete(m, k)
			return v, true
		}
	}
	return nil, false
}

func rawURN(urn string) string {
	urn = strings.TrimPrefix(urn, "urn:")
	urn = strings.TrimPrefix(urn, "tool:")
	return urn
}

// measureResponseSize calculates the total byte size of a CallToolResult
// by summing text content and marshalled structured content.
func measureResponseSize(res *mcp.CallToolResult, structBytes []byte) int64 {
	var size int64
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			size += int64(len(tc.Text))
		}
	}
	if structBytes != nil {
		size += int64(len(structBytes))
	} else if res.StructuredContent != nil {
		if b, err := json.Marshal(res.StructuredContent); err == nil {
			size += int64(len(b))
		}
	}
	return size
}

func summarize(text string) string {
	lines := strings.Split(text, "\n")
	if len(lines) > 10 {
		return strings.Join(lines[:10], "\n") + "\n... (more lines available in full resource)"
	}
	return text
}

// isTargetMatched determines if the routing 'target' (e.g. "magicskills" or "magicskills:magicskills_match")
// correctly intercepts the specific server/tool pair (URN format).
func isTargetMatched(urn string, target string) bool {
	if !strings.Contains(target, ":") {
		// Broad server match
		return strings.HasPrefix(urn, target+":")
	}
	// Specific tool match
	return urn == target
}

// interceptTier1Native natively scans vanilla JSON-RPC results for massive payload boundaries
// (>8KB strings). If tripped, it strips the text from RAM, streams it directly
// to the CSSA GlobalRingBuffer natively, and returns an ultra-compact pointer to the LLM agent without ever touching the Squeezer minifier.
func (h *OrchestratorHandler) interceptTier1Native(res *mcp.CallToolResult, _, _ string) *mcp.CallToolResult {
	if res == nil {
		return nil
	}

	// Structural Payload Check
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		if err == nil && len(b) > config.Proxy.MaxResponseBytes {
			if telemetry.GlobalRingBuffer != nil {
				// Native payload serialization strictly memory mapped to GlobalRingBuffer
				telemetry.GlobalRingBuffer.WriteRecord(b)
			}

			// Hybrid JSON markdown_payload extraction: if the StructuredContent
			// contains a pre-rendered GFM markdown payload, surface it directly
			// as TextContent for model-agnostic consumption.
			if mdPayload := extractMarkdownPayload(b); mdPayload != "" {
				res.StructuredContent = nil
				// Surface the GFM markdown directly as TextContent
				res.Content = []mcp.Content{
					&mcp.TextContent{Text: mdPayload},
				}
				if res.Meta == nil {
					res.Meta = make(map[string]any)
				}
				res.Meta["tier1_extracted"] = true
				res.Meta["hybrid_extracted"] = true
				return res
			}

			res.StructuredContent = nil
			res.Content = []mcp.Content{
				&mcp.TextContent{
					Text: "High-Fidelity structural JSON payload natively extracted over high-speed telemetry pipe into CSSA GlobalRingBuffer.\n\n> This payload exceeds backplane context visibility. Verify with specific read tools.",
				},
			}

			if res.Meta == nil {
				res.Meta = make(map[string]any)
			}
			res.Meta["tier1_extracted"] = true
			return res
		}
	}

	for i, content := range res.Content {
		tc, ok := content.(*mcp.TextContent)
		if !ok || len(tc.Text) <= config.Proxy.MaxResponseBytes {
			continue // Below threshold or not text
		}

		// Large payload detected! Intercept directly over native RingBuffer stream pipeline natively.
		if telemetry.GlobalRingBuffer != nil {
			telemetry.GlobalRingBuffer.WriteRecord([]byte(tc.Text))
		}

		// Mutate the original text to a pure LLM artifact reference
		// using Absolute URI standard
		res.Content[i] = &mcp.TextContent{
			Text: "High-Fidelity payload natively extracted over high-speed telemetry pipe into CSSA GlobalRingBuffer.\n\n> This payload exceeds backplane context visibility. Verify with specific read tools.",
		}

		if res.Meta == nil {
			res.Meta = make(map[string]any)
		}
		res.Meta["tier1_extracted"] = true
		// Fast exit upon first large extraction match
		return res
	}

	// Nothing intercepted or disk write failed implicitly
	return nil
}

// interceptTier2HFSC manages the extreme payload continuous base64 log streaming boundaries.
// It traps heavily specialized tool results holding the `hfsc_stream` Meta boolean, completely
// freezing the JSON-RPC proxy response loop indefinitely until the Sub-Server manually
// pushes the `HFSC_FINALIZE` log instruction closing the pipeline channel via `doneCh`.
func (h *OrchestratorHandler) interceptTier2HFSC(ctx context.Context, res *mcp.CallToolResult, server string) *mcp.CallToolResult {
	// Check Meta["hfsc_stream"] — guarantees explicit sub-server intervention requested
	if res.Meta == nil {
		return nil
	}
	enabled, ok := res.Meta["hfsc_stream"].(bool)
	if !ok || !enabled {
		return nil
	}

	sessionID := stringFrom(res.Meta[keySessionID])
	filename := stringFrom(res.Meta["filename"])
	projectID := stringFrom(res.Meta[keyProjectID])
	model := stringFrom(res.Meta["model"])

	if sessionID == "" {
		slog.Warn("tier2: extreme stream handshake completely malformed", "server", server, "meta", res.Meta)
		return nil
	}

	slog.Info("tier2: extreme stream handshake intercepted — locking proxy thread",
		"server", server,
		keySessionID, sessionID,
		"filename", filename,
	)

	// Register the Tier 2 stream trap and open immediate Host file descriptor pipe
	doneCh, err := h.Registry.HFSC.Register(sessionID, filename, projectID, model, server)
	if err != nil {
		slog.Error("tier2: cataclysmic stream pipe initialization failure", "server", server, "session", sessionID, keyError, err)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("HFSC Fatal File Pipe Error: %v", err)}},
			IsError: true,
		}
	}

	// 🛡️ TRAP: Complete suspension of process pipe proxy until remote host Log Stream issues FINALIZE
	tier2Timeout := config.Proxy.Tier2Timeout
	select {
	case <-doneCh:
		// Artifact has formally been materialized internally to disk!
	case <-time.After(tier2Timeout):
		slog.Error("tier2: cataclysmic timeout waiting for FINALIZE lock release", "session", sessionID, "server", server)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("HFSC Pipeline Timeout: %s extreme payload log stream stalled.", filename)}},
			IsError: true,
		}
	case <-ctx.Done():
		slog.Warn("tier2: connection violently severed by proxy client context expiration", keySessionID, sessionID)
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "HFSC Pipeline Terminated: Client context cancelled connection mid-stream."}},
			IsError: true,
		}
	}

	finalSafeFilename := fmt.Sprintf("%s_%s", sessionID, filepath.Base(filename))
	artifactPath := filepath.Join(h.Registry.HFSC.ArtifactDir(), finalSafeFilename)

	slog.Info("tier2: trap released, returning absolute artifact path natively",
		keySessionID, sessionID,
		"server", server,
		"artifact", artifactPath,
	)

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Extreme Payload Stream Successfully Synchronized.\n\n[Registry Volume Mount](file://%s)", artifactPath),
		}},
		Meta: map[string]any{
			"hfsc_delivered": true,
			keySessionID:     sessionID,
			"filename":       filename,
			"server":         server,
			"artifact_path":  artifactPath,
		},
	}
}

// extractMarkdownPayload checks if raw JSON bytes contain a top-level
// "markdown_payload" string field. If present, it returns the value.
// This enables the HFSC proxy to surface pre-rendered GFM markdown from
// Hybrid JSON envelopes (used by generate_final_report in brainstorm and
// go-modernizer) directly as TextContent to the AI agent, bypassing the
// generic transformToHybrid squeeze path.
func extractMarkdownPayload(raw []byte) string {
	var envelope struct {
		MarkdownPayload string `json:"markdown_payload"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return ""
	}
	return envelope.MarkdownPayload
}

// cloneToolRecords returns shallow copies so cache entries are not mutated by failure-proximity scoring.
func cloneToolRecords(in []*db.ToolRecord) []*db.ToolRecord {
	if len(in) == 0 {
		return nil
	}
	out := make([]*db.ToolRecord, len(in))
	for i, r := range in {
		if r == nil {
			continue
		}
		cp := *r
		out[i] = &cp
	}
	return out
}
