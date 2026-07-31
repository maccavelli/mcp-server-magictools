package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/logging"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util/logutil"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
	"github.com/maccavelli/mcplib"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *OrchestratorHandler) registerDiagnosticTools(s *mcp.Server) {
	mcplib.RegisterDiagnosticTool(s, logging.GlobalLogBuffer, serverMagictools)
	h.addTool(s, &mcp.Tool{Name: "get_session_stats"}, h.GetSessionStats)
	h.addTool(s, &mcp.Tool{Name: "get_health_report"}, h.GetHealthReport)
	h.addTool(s, &mcp.Tool{Name: toolAnalyzeSystemLogs}, h.AnalyzeSystemLogs)
	h.addTool(s, &mcp.Tool{Name: "update_config"}, h.UpdateConfig)
	h.addTool(s, &mcp.Tool{Name: "self_check"}, h.SelfCheck)
	h.addTool(s, &mcp.Tool{Name: "list_tools"}, h.ListToolsInfo)
	h.addTool(s, &mcp.Tool{Name: "semantic_similarity"}, h.SemanticSimilarityAudit)
	h.addTool(s, &mcp.Tool{Name: "query_compliance"}, h.QueryCompliance)
}

// GetSessionStats is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) GetSessionStats(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	stats := h.Telemetry.GetSessionStats()
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal telemetry: %w", err)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
}

// QueryCompliance is completely undocumented but provides extreme LLM memory mapping directly back into standard text dynamically matching user queries directly from Hydrator memory banks securely natively.
func (h *OrchestratorHandler) QueryCompliance(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
		return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		if h.RecallClient != nil && h.RecallClient.RecallEnabled() {
			standardsText := h.RecallClient.SearchStandards(ctx, input.Query, "", "", 5)

			envelope := map[string]any{
				keyMetadata: map[string]any{
					keyQuery:  input.Query,
					keySource: sourceRecallFallbackBM25,
				},
			}
			envJSON := marshalIndentOrEmpty(envelope)

			if standardsText == "" {
				return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("```json\n%s\n```\n\nNo specific standards matching the query were found via offline BM25 fallback natively.", string(envJSON))}}}, nil
			}
			return &mcp.CallToolResult{
				Content: []mcp.Content{
					&mcp.TextContent{
						Text: fmt.Sprintf("```json\n%s\n```\n\n✨ Offline Standards Memory Bank Response:\n%s\n\n(Note: Generated natively via offline Recall fallback bypassing Vector dependencies.)", string(envJSON), standardsText),
					},
				},
			}, nil
		}
		return &mcp.CallToolResult{
			IsError: true,
			Content: []mcp.Content{&mcp.TextContent{Text: "Vector Intelligence Offline and Recall Unreachable. Set LLM API Environment Variables securely to unlock Hydrator Database mapping."}},
		}, nil
	}

	urns, err := e.Search(ctx, input.Query, 5) // Return top 5 closest matched conceptual artifacts intuitively
	if err != nil {
		return nil, fmt.Errorf("failed semantic index bounding search: %w", err)
	}

	envelope := map[string]any{
		keyMetadata: map[string]any{
			keyQuery:      input.Query,
			keySource:     "vector_rag",
			"match_count": len(urns),
			"matches":     urns,
		},
	}
	envJSON := marshalIndentOrEmpty(envelope)

	if len(urns) == 0 {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("```json\n%s\n```\n\nNo specific standards matching the query were physically extracted dynamically natively.", string(envJSON))}}}, nil
	}

	// Simulate document parsing logically
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: fmt.Sprintf("```json\n%s\n```\n\n✨ RAG Standards Memory Bank Response:\nFound %d relevant context bounds exactly matching concept structurally.\nMatched Identifiers natively: %v\n\n(Note: Full document fetching from these identifiers requires recall or direct filesystem interaction.)", string(envJSON), len(urns), urns),
			},
		},
	}, nil
}

// GetHealthReport is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) GetHealthReport(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	h.Registry.PingAll(ctx)
	var names []string
	for _, sc := range h.Config.GetManagedServers() {
		names = append(names, sc.Name)
	}
	statusReport := h.Registry.GetStatusReport(names)

	report := map[string]any{
		"servers": statusReport,
	}

	// 🛡️ RECALL ENRICHMENT: Add historical trends from past boot snapshots
	if h.RecallClient != nil && h.RecallClient.RecallEnabled() {
		raw := h.RecallClient.CallDatabaseTool(ctx, "list", map[string]any{
			keyNamespace: namespaceServerStatus,
			keyServerID:  serverMagictools,
			keyLimit:     50,
		})
		if raw != "" {
			var envelope map[string]any
			if json.Unmarshal([]byte(raw), &envelope) == nil {
				var entries []any
				if e, ok := envelope["entries"].([]any); ok {
					entries = e
				} else if data, ok := envelope["data"].(map[string]any); ok {
					entries = sliceAnyOrWarn(data["entries"])
				}
				var validEntriesCount int
				for _, entryRaw := range entries {
					if entry, ok := entryRaw.(map[string]any); ok {
						if rec, ok := entry["record"].(map[string]any); ok {
							if rec[keySessionID] == "magictools-diagnostics" {
								validEntriesCount++
							}
						}
					}
				}
				if validEntriesCount > 0 {
					report["historical_trends"] = map[string]any{
						"boot_snapshots_available": validEntriesCount,
						keySource:                  sourceRecall,
					}
				}
			}
		}
	}

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		slog.Error("diagnostic: failed to marshal status report", keyError, err)
		return nil, fmt.Errorf("failed to generate health report")
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
}

// AnalyzeSystemLogs is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) AnalyzeSystemLogs(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		ServerID string `json:"server_id"`
		Lines    int    `json:"lines"`
		Severity string `json:"severity"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
		return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	linesToScan := 50
	if input.Lines > 0 {
		linesToScan = input.Lines
	}
	// Cap at 1000 lines for performance and to prevent OOM
	if linesToScan > 1000 {
		linesToScan = 1000
	}

	logPath := h.Config.LogPath
	if logPath == "" {
		logPath = config.DefaultLogPath()
	}

	// 1. Efficient Tail
	candidateLines, err := logutil.TailFile(logPath, linesToScan)
	if err != nil {
		if strings.Contains(err.Error(), "no such file or directory") {
			return nil, fmt.Errorf("log file not found at %s: ensure logging is enabled in config", logPath)
		}
		return nil, fmt.Errorf("tail failed: %w", err)
	}

	// 2. Multi-dimensional Filter
	filtered := logutil.FilterLogs(candidateLines, input.ServerID, input.Severity)

	// 3. ✨ PREDICTIVE TELEMETRY: Augment crash lines utilizing semantic vector distance organically
	if len(filtered) > 0 {
		if e := vector.GetEngine(); e != nil && e.VectorEnabled() && input.Severity == severityError {
			fixes, sErr := e.Search(ctx, filtered[0], 1)
			if sErr == nil && len(fixes) > 0 && fixes[0] != "" {
				filtered = append(filtered, "---", "✨ Semantic Telemetry: Correlated Historical Diagnostic Context:", fixes[0])
			}
		}

		// 🛡️ RECALL ENRICHMENT: Query past diagnostic snapshots for historical error patterns
		if input.Severity == severityError && h.RecallClient != nil && h.RecallClient.RecallEnabled() {
			raw := h.RecallClient.CallDatabaseTool(ctx, "list", map[string]any{
				keyNamespace: namespaceServerStatus,
				keyServerID:  serverMagictools,
				keyLimit:     5,
			})
			if raw != "" {
				filtered = append(filtered, "---", "📊 Historical Context (from Recall):",
					fmt.Sprintf("  Cross-session diagnostic data available (%d chars)", len(raw)))
			}
		}
	}

	// Format output as Markdown code block
	envelope := map[string]any{
		keyMetadata: map[string]any{
			keyServerID:     input.ServerID,
			"severity":      input.Severity,
			"lines_scanned": linesToScan,
			"total_matches": len(filtered),
		},
	}
	envJSON := marshalIndentOrEmpty(envelope)

	responseText := fmt.Sprintf("```json\n%s\n```\n\n```\n%s\n```", string(envJSON), strings.Join(filtered, "\n"))
	if len(filtered) == 0 {
		responseText = fmt.Sprintf("```json\n%s\n```\n\n*No matching log entries found.*", string(envJSON))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: responseText,
			},
		},
	}, nil
}

// UpdateConfig is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) UpdateConfig(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
		return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
	}

	input.Key = strings.TrimSpace(input.Key)
	input.Value = strings.TrimSpace(input.Value)
	if input.Key == "" || input.Value == "" {
		return nil, fmt.Errorf("both 'key' and 'value' are required")
	}

	// Validate and apply to runtime state
	oldValue, err := h.Config.UpdateConfigValue(input.Key, input.Value)
	if err != nil {
		return nil, err
	}

	// Build typed RuntimeConfigPatch for ConfigStore transaction
	var runtimePatch config.RuntimeConfigPatch
	switch input.Key {
	case keyLogLevel:
		runtimePatch.LogLevel = config.Set(h.Config.LogLevel)
	case "mcpLogLevel":
		runtimePatch.MCPLogLevel = config.Set(h.Config.MCPLogLevel)
	case "logFormat":
		runtimePatch.LogFormat = config.Set(h.Config.LogFormat)
	case "squeezeLevel":
		if h.Config.SqueezeLevelState != nil {
			runtimePatch.SqueezeLevel = config.Set(*h.Config.SqueezeLevelState)
		} else {
			runtimePatch.SqueezeLevel = config.Remove[int]()
		}
	case "scoreThreshold":
		runtimePatch.ScoreThreshold = config.Set(h.Config.ScoreThreshold)
	case "confidenceGap":
		runtimePatch.ConfidenceGap = config.Set(h.Config.ConfidenceGap)
	case "validateProxyCalls":
		runtimePatch.ValidateProxyCalls = config.Set(h.Config.ValidateProxyCalls)
	case "pinnedServers":
		runtimePatch.PinnedServers = config.Set(h.Config.PinnedServers)
	case "trustServers":
		runtimePatch.TrustServers = config.Set(h.Config.TrustServers)
	case "squeezeBypass":
		runtimePatch.SqueezeBypass = config.Set(h.Config.SqueezeBypass)
	case "ringBufferTargets":
		runtimePatch.RingBufferTargets = config.Set(h.Config.RingBufferTargets)
	case "tokenSpendThresh":
		runtimePatch.TokenSpendThresh = config.Set(h.Config.TokenSpendThresh)
	case "lruLimit":
		runtimePatch.LRULimit = config.Set(h.Config.LRULimit)
	}

	patch := config.ConfigurationPatch{Runtime: runtimePatch}
	store := config.NewStore(h.Config.Paths)
	if _, applyErr := store.Apply(ctx, patch); applyErr != nil {
		// Rollback in-memory mutation on store failure
		_, _ = h.Config.UpdateConfigValue(input.Key, oldValue) //nolint:errcheck // best-effort rollback
		return nil, fmt.Errorf("failed to persist configuration: %w", applyErr)
	}

	// 🛡️ LIVE LOG LEVEL: Apply immediately to all slog handlers
	if input.Key == keyLogLevel && h.LogLevel != nil {
		newLevel := logging.ParseLogLevel(strings.ToUpper(input.Value))
		h.LogLevel.Set(newLevel)
		slog.Info("update_config: log level changed at runtime", "old", oldValue, "new", strings.ToUpper(input.Value))
	}

	msg := fmt.Sprintf("Configuration updated: %s changed from '%s' to '%s'. Change persisted to config.yaml and applied at runtime.", input.Key, oldValue, input.Value)
	if input.Key == "logFormat" {
		msg += "\n\n[!] NOTE: logFormat requires an orchestrator process restart (`mcp_magictools_reload_servers magictools`) to rebuild the root slog handler tree."
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: msg}}}, nil
}

// SelfCheck is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) SelfCheck(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct{}
	if req.Params != nil && len(req.Params.Arguments) > 0 {
		unmarshalArgsOrWarn(req.Params.Arguments, &input)
	}

	// 1. Host OS Metrics
	osMetrics := telemetry.GetSystemProcessStats()

	// 2. Cache Efficacy
	rHits, rMisses, rItems := h.Store.Cache.GetMetrics()
	resHits, resMisses, resItems := h.Responses.GetMetrics()

	// 3. Database & Sync
	dbStats, err := h.Store.GetExtendedDiagnostics()
	if err != nil {
		slog.Warn("self_check: failed to retrieve extended database diagnostics", keyError, err)
	}

	payload := map[string]any{
		"system": osMetrics,
		"handlers": map[string]any{
			"align_tools_latency":    telemetry.MetaLatencies.AlignTools,
			"call_proxy_latency":     telemetry.MetaLatencies.CallProxy,
			"call_proxy_hot_latency": telemetry.MetaLatencies.CallProxyHot,
			"boot_latency":           telemetry.MetaLatencies.BootLatency,
		},
		"cache": map[string]any{
			"align_cache": map[string]any{
				keyHits:   telemetry.SearchMetrics.AlignCacheHits.Load(),
				keyMisses: telemetry.SearchMetrics.AlignCacheMisses.Load(),
				keyItems:  h.AlignCache.Len(),
			},
			"registry_cache": map[string]any{
				keyHits:   rHits,
				keyMisses: rMisses,
				keyItems:  rItems,
			},
			"response_cache": map[string]any{
				keyHits:   resHits,
				keyMisses: resMisses,
				keyItems:  resItems,
			},
		},
		"database": dbStats,
	}

	if vectorPayload := selfCheckVectorPayload(); vectorPayload != nil {
		payload["vector"] = vectorPayload
	}
	if recallPayload := selfCheckRecallPayload(ctx, h); recallPayload != nil {
		payload["session_memory"] = recallPayload
	}

	// 🛡️ LLM BACKPLANE TELEMETRY: Surface pool metrics and HFSC_LLM_TRACE entries
	if h.LLMPool != nil {
		payload["llm_backplane"] = h.LLMPool.Metrics()
	}
	if traces := h.Registry.GetLLMTraces(10); len(traces) > 0 {
		payload["llm_traces"] = traces
	}

	payload["argument_repairs"] = selfCheckArgumentRepairsPayload()

	jsonData, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to format self_check payload: %w", err)
	}

	markdown := string(jsonData)

	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.TextContent{
				Text: markdown,
			},
		},
	}, nil
}

// ListToolsOptions is undocumented but satisfies standard structural requirements.
type ListToolsOptions struct {
	MaxTools *int `json:"max_tools,omitempty,omitzero"`
}

// ListToolsInfo is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) ListToolsInfo(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var input struct {
		ServerName string `json:"server_name"`

		Options *ListToolsOptions `json:"options"`
	}

	hasArgs := false
	if req.Params != nil && len(req.Params.Arguments) > 0 {
		hasArgs = strings.TrimSpace(string(req.Params.Arguments)) != "{}"
		if hasArgs {
			if err := json.Unmarshal(req.Params.Arguments, &input); err != nil {
				return nil, fmt.Errorf("failed to unmarshal arguments: %w", err)
			}
		}
	}

	if input.Options == nil {
		input.Options = &ListToolsOptions{MaxTools: new(1000)}
	} else if input.Options.MaxTools == nil {
		input.Options.MaxTools = new(1000)
	}

	if !hasArgs {
		return listToolsInfoDefaultInventory()
	}

	serverFilter := strings.TrimSpace(input.ServerName)
	serverTools, count := h.collectListToolsInfoRecords(serverFilter, *input.Options.MaxTools)
	if count == 0 {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: "No sub-server tools found."}}}, nil
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: formatListToolsInfoMarkdown(serverTools)}}}, nil
}
