package client

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type snapshotContext struct {
	managedServers []string
	recallSess     *mcp.ClientSession
	report         []ServerStatus
	scoresPayload  map[string]any
}

func (m *WarmRegistry) collectSnapshotContext(dashboardScores map[string]any) snapshotContext {
	var managedServers []string
	var recallSess *mcp.ClientSession
	m.mu.RLock()
	for name, s := range m.Servers {
		managedServers = append(managedServers, name)
		if name == serverRecall && s.Session != nil {
			recallSess = s.Session
		}
	}
	m.mu.RUnlock()

	scoresPayload := dashboardScores
	if scoresPayload == nil {
		scoresPayload = make(map[string]any)
	}

	return snapshotContext{
		managedServers: managedServers,
		recallSess:     recallSess,
		report:         m.GetStatusReport(managedServers),
		scoresPayload:  scoresPayload,
	}
}

func fetchRecallDBMetrics(recallSess *mcp.ClientSession) any {
	if recallSess == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	res, err := recallSess.CallTool(ctx, &mcp.CallToolParams{Name: "get_metrics"})
	if err != nil || res == nil || res.StructuredContent == nil {
		return nil
	}
	if sc, ok := res.StructuredContent.(map[string]any); ok {
		if data, ok := sc["data"]; ok {
			return data
		}
	}
	return nil
}

func buildBaseSnapshot(ctx snapshotContext, recallDBMetrics any, databasesHistory map[string]any, storeMetrics any) map[string]any {
	return map[string]any{
		"timestamp": time.Now().UnixNano(),
		"servers":   ctx.report,
		"tools":     telemetry.GlobalToolTracker.GetAll(),
		"scores":    ctx.scoresPayload,
		"databases": map[string]any{
			serverMagictools:   storeMetrics,
			serverRecall:       recallDBMetrics,
			"is_healing":       telemetry.IsHealing.Load(),
			"sync_out_of_sync": telemetry.SyncOutOfSync.Load(),
		},
		"databases_history":   databasesHistory,
		"opt_metrics":         buildOptMetricsSnapshot(),
		"errors":              buildErrorTaxonomySnapshot(),
		"lifecycle":           buildLifecycleSnapshot(),
		"recent_errors":       telemetry.RecentErrors.GetAll(),
		"collisions":          telemetry.Collisions.Snapshot(),
		"dag_status":          telemetry.GlobalDAGTracker.Snapshot(),
		"cross_server_routes": telemetry.GlobalRouteTracker.Snapshot(),
	}
}

func buildOptMetricsSnapshot() map[string]any {
	return map[string]any{
		"squeeze_bypass":       telemetry.OptMetrics.SqueezeBypassCount.Load(),
		"squeeze_trunc":        telemetry.OptMetrics.SqueezeTruncations.Load(),
		"total_raw_bytes":      telemetry.OptMetrics.TotalRawBytes.Load(),
		"total_squeezed_bytes": telemetry.OptMetrics.TotalSqueezedBytes.Load(),
		"hfsc_success":         telemetry.OptMetrics.HFSCReassemblySuccesses.Load(),
		"hfsc_fail":            telemetry.OptMetrics.HFSCReassemblyFails.Load(),
		"hfsc_swept":           telemetry.OptMetrics.HFSCSweptStale.Load(),
		"hfsc_active":          telemetry.OptMetrics.HFSCActiveStreams.Load(),
		"cssa_offload":         telemetry.OptMetrics.CSSAOffloadBytes.Load(),
		"cssa_sync":            telemetry.OptMetrics.CSSASyncOperations.Load(),
		"total_proxy_calls":    telemetry.GlobalToolTracker.TotalCalls(),
		"orchestrator_pid":     int64(os.Getpid()),
	}
}

func buildErrorTaxonomySnapshot() map[string]int64 {
	return map[string]int64{
		"timeout":            telemetry.ErrorTaxonomy.Timeout.Load(),
		"connection_refused": telemetry.ErrorTaxonomy.ConnectionRefused.Load(),
		"panic":              telemetry.ErrorTaxonomy.Panic.Load(),
		"validation":         telemetry.ErrorTaxonomy.Validation.Load(),
		"hallucination":      telemetry.ErrorTaxonomy.HallucinationBlocked.Load(),
		"pipe_error":         telemetry.ErrorTaxonomy.PipeError.Load(),
		"context_cancelled":  telemetry.ErrorTaxonomy.ContextCancelled.Load(),
	}
}

func buildLifecycleSnapshot() map[string]int64 {
	return map[string]int64{
		"restarts_health":      telemetry.LifecycleEvents.RestartsHealth.Load(),
		"restarts_oom":         telemetry.LifecycleEvents.RestartsOOM.Load(),
		"evictions":            telemetry.LifecycleEvents.EvictionsEviction.Load(),
		"reconnections":        telemetry.LifecycleEvents.Reconnections.Load(),
		"config_reloads":       telemetry.LifecycleEvents.ConfigReloads.Load(),
		"backpressure_pending": telemetry.LifecycleEvents.BackpressurePending.Load(),
		"backpressure_reject":  telemetry.LifecycleEvents.BackpressureReject.Load(),
	}
}

func attachConfigSnapshot(snapshot map[string]any, m *WarmRegistry, managedServers []string) {
	if m.Config == nil {
		return
	}
	squeezeLevel := 0
	if m.Config.SqueezeLevelState != nil {
		squeezeLevel = *m.Config.SqueezeLevelState
	}
	snapshot["config"] = map[string]any{
		"score_threshold":       m.Config.ScoreThreshold,
		"squeeze_level":         squeezeLevel,
		"max_response_tokens":   m.Config.MaxResponseTokens,
		"log_level":             m.Config.GetLogLevel(),
		"mcp_log_level":         m.Config.GetMCPLogLevel(),
		"log_format":            m.Config.GetLogFormat(),
		"validate_proxy":        m.Config.ValidateProxyCalls,
		"no_optimize":           m.Config.NoOptimize,
		"pinned_servers":        m.Config.GetPinnedServers(),
		"squeeze_bypass":        m.Config.GetSqueezeBypass(),
		"managed_servers":       len(managedServers),
		"token_spend_thresh":    m.Config.TokenSpendThresh,
		"config_path":           m.Config.ConfigPath,
		"db_path":               m.Config.DBPath,
		"intelligence_provider": m.Config.Intelligence.Provider,
		"intelligence_model":    m.Config.Intelligence.Model,
	}
}

func attachProxySnapshot(snapshot map[string]any) {
	snapshot["proxy"] = map[string]any{
		"servers":       telemetry.GlobalTracker.GetAll(),
		"session_stats": telemetry.GlobalTracker.GetSessionStats(),
		"latencies": map[string]any{
			"align_tools_ema":    telemetry.MetaLatencies.AlignTools.EMA,
			"align_tools_count":  telemetry.MetaLatencies.AlignTools.Count,
			"call_proxy_ema":     telemetry.MetaLatencies.CallProxy.EMA,
			"call_proxy_count":   telemetry.MetaLatencies.CallProxy.Count,
			"call_proxy_hot_ema": telemetry.MetaLatencies.CallProxyHot.EMA,
			"call_proxy_hot_cnt": telemetry.MetaLatencies.CallProxyHot.Count,
			"boot_ema":           telemetry.MetaLatencies.BootLatency.EMA,
			"boot_count":         telemetry.MetaLatencies.BootLatency.Count,
		},
	}
}

func attachRuntimeSnapshots(snapshot map[string]any) telemetry.RuntimeSnapshot {
	rtSnap := telemetry.CaptureRuntime()
	snapshot["runtime"] = map[string]any{
		"heap_alloc_mb":   rtSnap.HeapAllocMB,
		"heap_sys_mb":     rtSnap.HeapSysMB,
		"num_gc":          rtSnap.NumGC,
		"pause_total_ms":  rtSnap.PauseTotalMs,
		"num_goroutine":   rtSnap.NumGoroutine,
		"go_max_procs":    rtSnap.GoMaxProcs,
		"go_mem_limit_mb": rtSnap.GoMemLimitMB,
		"headroom_pct":    rtSnap.HeadroomPct,
	}
	osStats := telemetry.GetSystemProcessStats()
	snapshot["system"] = map[string]any{
		"os":            runtime.GOOS,
		"arch":          runtime.GOARCH,
		"num_goroutine": rtSnap.NumGoroutine,
		"heap_alloc_mb": rtSnap.HeapAllocMB,
		"cpu_percent":   osStats.CPUPercent,
		"memory_vms_mb": osStats.MemoryVMS_MB,
	}
	return rtSnap
}

func attachRegistrySnapshot(snapshot map[string]any, m *WarmRegistry, managedServers []string) {
	storeMetrics := m.Store.GetMetrics()
	snapshot["registry"] = map[string]any{
		"total_tools":   storeMetrics.Tools,
		"total_servers": len(managedServers),
		"cache_hits":    storeMetrics.Hits,
	}
}

func cardFloat(card map[string]any, key string) float64 {
	v, ok := card[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

func buildScoringTelemetry(scoresPayload map[string]any) ([]map[string]any, []map[string]any) {
	scoringFactors := []map[string]any{}
	volatilityIndex := []map[string]any{}

	for _, cardAny := range scoresPayload {
		card, ok := cardAny.(map[string]any)
		if !ok {
			continue
		}
		urn, _ := card["URN"].(string) //nolint:errcheck // optional dashboard field; key written as "URN" by db.scoreboard_helpers
		faults := cardFloat(card, "Faults")
		deltaAll := cardFloat(card, "DeltaAll")
		calls := cardFloat(card, "Calls")
		reliability := cardFloat(card, "Reliability")
		if calls == 0 {
			continue
		}
		if faults > 0 {
			scoringFactors = append(scoringFactors, map[string]any{
				keyCategory:   scoringFaultRecovery,
				keyCount:      int64(faults),
				keyImpactType: impactPenalty,
			})
		} else {
			scoringFactors = append(scoringFactors, map[string]any{
				keyCategory:   scoringNominalOps,
				keyCount:      int64(calls),
				keyImpactType: impactNeutral,
			})
		}
		if deltaAll > 0 {
			scoringFactors = append(scoringFactors, map[string]any{
				keyCategory:   scoringTrendingAlign,
				keyCount:      int64(1),
				keyImpactType: impactReward,
			})
		}
		volScore := faults*0.5 + (1.0-reliability)*10.0
		volatilityIndex = append(volatilityIndex, map[string]any{
			"score": volScore,
			keyURN:  urn,
		})
	}
	return scoringFactors, volatilityIndex
}

func attachNetworkDynamics(snapshot map[string]any) {
	rawBytes := telemetry.OptMetrics.TotalRawBytes.Load()
	sqBytes := telemetry.OptMetrics.TotalSqueezedBytes.Load()
	var sqSat float64
	if rawBytes > 0 {
		sqSat = float64(rawBytes-sqBytes) / float64(rawBytes) * 100.0
	}
	activeStreams := telemetry.OptMetrics.HFSCActiveStreams.Load()
	var hfSat float64
	if activeStreams > 0 {
		hfSat = float64(activeStreams) / 2048.0 * 100.0
	}
	snapshot["network_dynamics"] = map[string]any{
		"token_velocity_tps":     telemetry.ComputeTokenVelocity(telemetry.GlobalTokenSpend.Load()),
		"squeeze_saturation_pct": sqSat,
		"hfsc_saturation_pct":    hfSat,
	}
}

func attachSearchSnapshots(snapshot map[string]any, m *WarmRegistry) int64 {
	modeLabel := getFusionModeLabel(m.Config)
	var bTop, hTop []string
	if bp := telemetry.SearchMetrics.LastBleveTop5.Load(); bp != nil {
		bTop = *bp
	}
	if hp := telemetry.SearchMetrics.LastHnswTop5.Load(); hp != nil {
		hTop = *hp
	}
	totalSearches := telemetry.SearchMetrics.TotalSearches.Load()
	snapshot["search"] = searchSnapshotFromMetrics(modeLabel, modeLabel, bTop, hTop)

	var bleveDocs int64
	dbPath := ""
	dbSize := float64(0)
	if m.Store != nil {
		bleveDocs = safeInt64FromUint64(m.Store.GetMetrics().BleveDocs)
	}
	if m.Config != nil && m.Config.DBPath != "" {
		dbPath = m.Config.DBPath
		dbSize = calculateDirSizeMB(m.Config.DBPath)
	}
	snapshot["semantic_recall"] = semanticRecallSnapshot(dbPath, dbSize, bleveDocs, getHNSWGraphSize())
	snapshot["intent_routing"] = intentRoutingSnapshot(totalSearches)
	snapshot["schema_health"] = schemaHealthSnapshot(int64(len(m.Servers)))
	snapshot["orchestrator"] = orchestratorSearchSnapshot()
	return totalSearches
}

func attachDistributedTracing(snapshot map[string]any, m *WarmRegistry) {
	snapshot["distributed_tracing"] = map[string]any{
		"active_spans":   telemetry.GetAllActiveSpans(),
		"cascade_parent": telemetry.GetActiveCascadeParent(),
		"cascade_source": telemetry.GetActiveCascadeSource(),
	}
	if m.LLMMetrics != nil {
		snapshot["llm_backplane"] = m.LLMMetrics.Metrics()
	}
}

func attachIPCSessions(snapshot map[string]any) {
	totalIDEBytes := telemetry.IPCSessionCounters.TotalBytes.Load()
	snapshot["ipc_sessions"] = map[string]any{
		"connects":           telemetry.IPCSessionCounters.Connects.Load(),
		"disconnects":        telemetry.IPCSessionCounters.Disconnects.Load(),
		keyActive:            telemetry.IPCSessionCounters.Active.Load(),
		"total_bytes":        totalIDEBytes,
		"throughput_kbps":    telemetry.ComputeIDEThroughput(totalIDEBytes),
		"post_requests":      telemetry.IPCSessionCounters.PostRequests.Load(),
		"sse_bytes_sent":     telemetry.IPCSessionCounters.SSEBytesSent.Load(),
		"post_bytes_sent":    telemetry.IPCSessionCounters.PostBytesSent.Load(),
		"sse_resumed":        telemetry.IPCSessionCounters.SSEResumed.Load(),
		"rate_limit_rejects": telemetry.IPCSessionCounters.RateLimitRejects.Load(),
		"readiness_503s":     telemetry.IPCSessionCounters.Readiness503s.Load(),
	}
}

func (m *WarmRegistry) attachFirewallSnapshot(snapshot map[string]any) {
	firewallMetrics := make(map[string]any)
	m.mu.RLock()
	for name, s := range m.Servers {
		if s.Firewall != nil {
			firewallMetrics[name] = s.Firewall.Snapshot()
		}
	}
	m.mu.RUnlock()
	snapshot["firewall"] = firewallMetrics
}

func attachUDPTelemetry(snapshot map[string]any) {
	if udpSrv := telemetry.GlobalUDPServer; udpSrv != nil {
		snapshot["udp_telemetry"] = map[string]any{
			"bound_port":      udpSrv.BoundPort(),
			"active_clients":  udpSrv.ActiveClients(),
			"packets_sent":    udpSrv.PacketsSent.Load(),
			"packets_dropped": udpSrv.PacketsDropped.Load(),
			"reconnect_count": udpSrv.ReconnectCount.Load(),
		}
	}
}

func persistSnapshot(snapshot map[string]any, flush bool, m *WarmRegistry) {
	telemetry.LatestSnapshot.Store(snapshot)
	if telemetry.GlobalRingBuffer != nil {
		if err := telemetry.GlobalRingBuffer.WriteGauges(snapshot); err != nil {
			slog.Debug("telemetry: failed to write gauge snapshot to ring buffer", "error", err)
		}
	}
	if flush {
		db.FlushMetricBucket(m.Store, snapshot)
	}
}
