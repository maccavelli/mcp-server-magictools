// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Tab 5: System Backplane ──────────────────────────────────────────────────

func buildSystemBackplaneTab(snapshot map[string]any) string {
	// CSSA & HFSC Metrics
	var cssaRows [][]string
	if optRaw, ok := snapshot["opt_metrics"].(map[string]any); ok && len(optRaw) > 0 {
		hfscActive := numI64(optRaw, "hfsc_active")
		hfscActiveStr := fmt.Sprint(hfscActive)
		if hfscActive > 10 {
			hfscActiveStr = warningStyle.Render(hfscActiveStr)
		} else if hfscActive > 0 {
			hfscActiveStr = successStyle.Render(hfscActiveStr)
		}
		hfscFails := numI64(optRaw, "hfsc_fail")
		hfscFailStr := fmt.Sprint(hfscFails)
		if hfscFails > 0 {
			hfscFailStr = errorStyle.Render(hfscFailStr)
		}
		cssaRows = append(cssaRows,
			[]string{"CSSA Offload", fmt.Sprintf("%.2f KB", float64(numI64(optRaw, "cssa_offload"))/1024.0)},
			[]string{"CSSA Sync Operations", fmt.Sprint(numI64(optRaw, "cssa_sync"))},
			[]string{"HFSC Reassembly OK", fmt.Sprint(numI64(optRaw, "hfsc_success"))},
			[]string{"HFSC Reassembly Fail", hfscFailStr},
			[]string{"HFSC Swept Stale", fmt.Sprint(numI64(optRaw, "hfsc_swept"))},
			[]string{"HFSC Active Streams", hfscActiveStr},
			[]string{"Squeeze Bypasses", fmt.Sprint(numI64(optRaw, "squeeze_bypass"))},
			[]string{"Squeeze Truncations", fmt.Sprint(numI64(optRaw, "squeeze_trunc"))},
		)

		// Compression efficiency
		rawBytes := numI64(optRaw, "total_raw_bytes")
		squeezedBytes := numI64(optRaw, "total_squeezed_bytes")
		effStr := dashNA
		if rawBytes > 0 {
			eff := 1.0 - (float64(squeezedBytes) / float64(rawBytes))
			effStr = fmt.Sprintf("%.2f%%", eff*100)
			if eff > 0.5 {
				effStr = successStyle.Render(effStr)
			} else if eff > 0.1 {
				effStr = warningStyle.Render(effStr)
			} else {
				effStr = errorStyle.Render(effStr)
			}
		}
		cssaRows = append(cssaRows,
			[]string{"Total Raw Volume", fmt.Sprintf("%.2f KB", float64(rawBytes)/1024.0)},
			[]string{"Squeezed Volume", fmt.Sprintf("%.2f KB", float64(squeezedBytes)/1024.0)},
			[]string{"Squeeze Efficiency", effStr},
			[]string{"Total Proxy Calls", fmt.Sprint(numI64(optRaw, "total_proxy_calls"))},
			[]string{"Orchestrator PID", fmt.Sprint(numI64(optRaw, "orchestrator_pid"))},
		)
	} else {
		cssaRows = append(cssaRows, []string{colStatus, "Waiting for CSSA/HFSC telemetry..."})
	}
	cssaBox := cardStyle.Render(subTitleStyle.Render("CSSA & HFSC Backplane") + "\n" + renderStyledTable([]string{colMetric, colValue}, cssaRows))

	// IPC / Stdio Session Metrics
	var ipcRows [][]string
	if ipcRaw, ok := snapshot["ipc_sessions"].(map[string]any); ok {
		active := numI64(ipcRaw, "active")
		activeStr := fmt.Sprint(active)
		if active > 0 {
			activeStr = successStyle.Render(activeStr)
		}

		totalKB := float64(numI64(ipcRaw, "total_bytes")) / 1024.0
		tpKBps := numF64(ipcRaw, "throughput_kbps")

		ipcRows = append(ipcRows,
			[]string{"IDE Connects", fmt.Sprint(numI64(ipcRaw, "connects"))},
			[]string{"IDE Disconnects", fmt.Sprint(numI64(ipcRaw, "disconnects"))},
			[]string{"Active IDE Streams", activeStr},
			[]string{"IDE Data Volume", fmt.Sprintf("%.2f KB", totalKB)},
			[]string{"IDE Throughput", fmt.Sprintf("%.2f KB/s", tpKBps)},
		)
	} else {
		ipcRows = append(ipcRows, []string{colStatus, "No IPC session data."})
	}

	// DASH-3: IDE Pipe Health card (SSE vs POST breakdown)
	var ideHealthRows [][]string
	if ipcRaw, ok := snapshot["ipc_sessions"].(map[string]any); ok {
		sseKB := float64(numI64(ipcRaw, "sse_bytes_sent")) / 1024.0
		postKB := float64(numI64(ipcRaw, "post_bytes_sent")) / 1024.0
		sseResumed := numI64(ipcRaw, "sse_resumed")
		resumedStr := fmt.Sprint(sseResumed)
		if sseResumed > 0 {
			resumedStr = warningStyle.Render(resumedStr)
		}
		rateLimited := numI64(ipcRaw, "rate_limit_rejects")
		rlStr := fmt.Sprint(rateLimited)
		if rateLimited > 0 {
			rlStr = errorStyle.Render(rlStr)
		}
		readiness503 := numI64(ipcRaw, "readiness_503s")
		rdStr := fmt.Sprint(readiness503)
		if readiness503 > 0 {
			rdStr = warningStyle.Render(rdStr)
		}
		ideHealthRows = append(ideHealthRows,
			[]string{"POST Requests", fmt.Sprint(numI64(ipcRaw, "post_requests"))},
			[]string{"SSE Bytes Sent", fmt.Sprintf("%.2f KB", sseKB)},
			[]string{"POST Bytes Sent", fmt.Sprintf("%.2f KB", postKB)},
			[]string{"SSE Streams Resumed", resumedStr},
			[]string{"Rate Limit Rejects", rlStr},
			[]string{"Readiness 503s", rdStr},
		)
	} else {
		ideHealthRows = append(ideHealthRows, []string{colStatus, "No IDE pipe data."})
	}
	ideHealthBox := cardStyle.Render(subTitleStyle.Render("IDE Pipe Health") + "\n" + renderStyledTable([]string{colMetric, colValue}, ideHealthRows))

	// Network Dynamics
	if netRaw, ok := snapshot["network_dynamics"].(map[string]any); ok {
		vel := numF64(netRaw, "token_velocity_tps")
		sqSat := numF64(netRaw, "squeeze_saturation_pct")
		hfSat := numF64(netRaw, "hfsc_saturation_pct")
		sqStr := fmt.Sprintf("%.1f%%", sqSat)
		hfStr := fmt.Sprintf("%.1f%%", hfSat)
		if sqSat > 80 {
			sqStr = errorStyle.Render(sqStr)
		} else if sqSat > 50 {
			sqStr = warningStyle.Render(sqStr)
		} else {
			sqStr = successStyle.Render(sqStr)
		}
		if hfSat > 80 {
			hfStr = errorStyle.Render(hfStr)
		} else if hfSat > 50 {
			hfStr = warningStyle.Render(hfStr)
		} else {
			hfStr = successStyle.Render(hfStr)
		}
		ipcRows = append(ipcRows,
			[]string{"Token Velocity (TPS)", fmt.Sprintf("%.1f", vel)},
			[]string{"Gateway Squeeze Sat", sqStr},
			[]string{"HFSC Fragmenter Sat", hfStr},
		)
	}
	ipcBox := cardStyle.Render(subTitleStyle.Render("IDE HTTP & Network Dynamics") + "\n" + renderStyledTable([]string{colMetric, colValue}, ipcRows))

	row1 := lipgloss.JoinHorizontal(lipgloss.Top, cssaBox, ipcBox)
	return lipgloss.JoinVertical(lipgloss.Left, row1, ideHealthBox)
}

func renderMuxLinks(snapshot map[string]any) string {
	var muxRows [][]string
	if proxyRaw, ok := snapshot["proxy"].(map[string]any); ok {
		if serversRaw, ok := proxyRaw["servers"].(map[string]any); ok {
			for name, mAny := range serversRaw {
				m, ok := mAny.(map[string]any)
				if !ok {
					continue
				}
				throughput := numF64(m, "throughput_kbps")
				tpStr := fmt.Sprintf("%.2f KB/s", throughput)

				stateStr := grayStyle.Render("Idle")

				// Sub-server connection status
				if srvRaw, ok := snapshot["servers"].([]any); ok {
					for _, sAny := range srvRaw {
						if s, ok := sAny.(map[string]any); ok {
							if str(s, "name") == name && s["running"] == true {
								stateStr = successStyle.Render("Connected")
								break
							}
						}
					}
				}

				healthStr := successStyle.Render("Flowing")
				if throughput > 100 {
					healthStr = warningStyle.Render("Saturated")
				}
				if throughput == 0 && stateStr != grayStyle.Render("Idle") {
					healthStr = grayStyle.Render("Standby")
				}

				muxRows = append(muxRows, []string{name, stateStr, tpStr, healthStr})
			}
		}
	}
	if len(muxRows) == 0 {
		muxRows = append(muxRows, []string{colStatus, "No sub-servers active.", "-", "-"})
	}
	return cardStyle.Render(subTitleStyle.Render("Multiplexer IPC Links") + "\n" + renderStyledTable([]string{colServer, "State", "Throughput", "Link Health"}, muxRows))
}

func renderMuxHealth(snapshot map[string]any) string {
	var muxHealthRows [][]string
	if muxRaw, ok := snapshot["proxy_mux"].(map[string]any); ok {
		inbound := numI64(muxRaw, "inbound_messages")
		outbound := numI64(muxRaw, "outbound_messages")
		drops := numI64(muxRaw, "outbound_drops")
		dropsStr := fmt.Sprint(drops)
		if drops > 0 {
			dropsStr = errorStyle.Render(dropsStr)
		}
		sseActive := numI64(muxRaw, "sse_streams_active")
		sseStr := fmt.Sprint(sseActive)
		if sseActive > 0 {
			sseStr = successStyle.Render(sseStr)
		}
		writeErrors := numI64(muxRaw, "write_errors")
		weStr := fmt.Sprint(writeErrors)
		if writeErrors > 0 {
			weStr = errorStyle.Render(weStr)
		}
		marshalErrors := numI64(muxRaw, "json_marshal_errors")
		meStr := fmt.Sprint(marshalErrors)
		if marshalErrors > 0 {
			meStr = errorStyle.Render(meStr)
		}
		unmarshalErrors := numI64(muxRaw, "unmarshal_errors")
		ueStr := fmt.Sprint(unmarshalErrors)
		if unmarshalErrors > 0 {
			ueStr = errorStyle.Render(ueStr)
		}
		relayErrors := numI64(muxRaw, "relay_errors")
		reStr := fmt.Sprint(relayErrors)
		if relayErrors > 0 {
			reStr = errorStyle.Render(reStr)
		}

		// Drop rate calculation
		dropRate := "0.0%"
		if outbound+drops > 0 {
			rate := float64(drops) / float64(outbound+drops) * 100
			dropRate = fmt.Sprintf("%.2f%%", rate)
			if rate > 1 {
				dropRate = errorStyle.Render(dropRate)
			} else if rate > 0 {
				dropRate = warningStyle.Render(dropRate)
			} else {
				dropRate = successStyle.Render(dropRate)
			}
		}

		muxHealthRows = append(muxHealthRows,
			[]string{"Inbound Messages", fmt.Sprint(inbound)},
			[]string{"Outbound Messages", fmt.Sprint(outbound)},
			[]string{"Outbound Drops", dropsStr},
			[]string{"Drop Rate", dropRate},
			[]string{"SSE Streams Active", sseStr},
			[]string{"SSE Events Processed", fmt.Sprint(numI64(muxRaw, "sse_events_processed"))},
			[]string{"Relay Errors", reStr},
			[]string{"401 Auto-Heals", fmt.Sprint(numI64(muxRaw, "relay_401_heals"))},
			[]string{"Write Errors", weStr},
			[]string{"Marshal Errors", meStr},
			[]string{"Unmarshal Errors", ueStr},
			[]string{"Notifications Broadcast", fmt.Sprint(numI64(muxRaw, "notifications_broadcast"))},
			[]string{"Notifications Dropped (QoS)", fmt.Sprint(numI64(muxRaw, "notifications_dropped_qos"))},
			[]string{"Byte Quota Drops", fmt.Sprint(numI64(muxRaw, "byte_quota_drops"))},
		)
	} else {
		muxHealthRows = append(muxHealthRows, []string{colStatus, "Waiting for proxy mux telemetry..."})
	}
	return cardStyle.Render(subTitleStyle.Render("Multiplexer Health") + "\n" + renderStyledTable([]string{colMetric, colValue}, muxHealthRows))
}

// ── Tab 6: Storage & Databases ───────────────────────────────────────────────

func buildStorageTab(snapshot map[string]any) string { //nolint:gocritic // dashboard table rows built incrementally for readability
	var pairs [][]string

	// Runtime metrics
	if rtRaw, ok := snapshot["runtime"].(map[string]any); ok && len(rtRaw) > 0 {
		sysRaw, hasSys := snapshot["system"].(map[string]any)
		cpuPercent := 0.0
		memVms := 0.0
		if hasSys {
			cpuPercent = numF64(sysRaw, "cpu_percent")
			memVms = numF64(sysRaw, "memory_vms_mb")
		}

		pairs = append(pairs,
			[]string{"Heap Alloc", fmt.Sprintf("%.1f MB", numF64(rtRaw, "heap_alloc_mb"))},
			[]string{"Heap Sys", fmt.Sprintf("%.1f MB", numF64(rtRaw, "heap_sys_mb"))},
			[]string{"OS VMS Mem", fmt.Sprintf("%.1f MB", memVms)},
			[]string{"OS CPU %", fmt.Sprintf("%.2f%%", cpuPercent)},
			[]string{"GC Cycles", fmt.Sprint(numI64(rtRaw, "num_gc"))},
			[]string{"GC Pause Total", fmt.Sprintf("%.1f ms", numF64(rtRaw, "pause_total_ms"))},
			[]string{"Goroutines", fmt.Sprint(numI64(rtRaw, "num_goroutine"))},
			[]string{"GOMAXPROCS", fmt.Sprint(numI64(rtRaw, "go_max_procs"))},
		)
		goMemLimitMB := numF64(rtRaw, "go_mem_limit_mb")
		if goMemLimitMB > 0 {
			pairs = append(pairs, []string{"GOMEMLIMIT", fmt.Sprintf("%.0f MB", goMemLimitMB)})
			headroom := numF64(rtRaw, "headroom_pct")
			hStr := fmt.Sprintf("%.1f%%", headroom)
			if headroom < 20 {
				hStr = errorStyle.Render(hStr)
			} else if headroom < 50 {
				hStr = warningStyle.Render(hStr)
			} else {
				hStr = successStyle.Render(hStr)
			}
			pairs = append(pairs, []string{"GOMEMLIMIT Headroom", hStr})
		}
	}

	// Error Taxonomy
	if errRaw, ok := snapshot["errors"].(map[string]any); ok {
		pairs = append(pairs,
			[]string{"Timeout Errors", str(errRaw, "timeout")},
			[]string{"Connection Refused", str(errRaw, "connection_refused")},
			[]string{"Panic Errors", str(errRaw, "panic")},
			[]string{"Validation Errors", str(errRaw, "validation")},
			[]string{"Hallucination Errors", str(errRaw, "hallucination")},
			[]string{"Pipe/Context Errors", fmt.Sprintf("%s / %s", str(errRaw, "pipe_error"), str(errRaw, "context_cancelled"))},
		)
	}

	// Lifecycle Events
	if lcRaw, ok := snapshot["lifecycle"].(map[string]any); ok {
		pairs = append(pairs,
			[]string{"Health Restarts", str(lcRaw, "restarts_health")},
			[]string{"OOM Restarts", str(lcRaw, "restarts_oom")},
			[]string{"Evictions/Reconnects", fmt.Sprintf("%s / %s", str(lcRaw, "evictions"), str(lcRaw, "reconnections"))},
			[]string{"Config Reloads", str(lcRaw, "config_reloads")},
			[]string{"Backpressure P/R", fmt.Sprintf("%s / %s", str(lcRaw, "backpressure_pending"), str(lcRaw, "backpressure_reject"))},
		)
	}

	// Network Dynamics
	if netRaw, ok := snapshot["network_dynamics"].(map[string]any); ok {
		vel := numF64(netRaw, "token_velocity_tps")
		sqSat := numF64(netRaw, "squeeze_saturation_pct")
		hfSat := numF64(netRaw, "hfsc_saturation_pct")
		sqStr := fmt.Sprintf("%.1f%%", sqSat)
		hfStr := fmt.Sprintf("%.1f%%", hfSat)
		if sqSat > 80 {
			sqStr = errorStyle.Render(sqStr)
		} else if sqSat > 50 {
			sqStr = warningStyle.Render(sqStr)
		} else {
			sqStr = successStyle.Render(sqStr)
		}
		if hfSat > 80 {
			hfStr = errorStyle.Render(hfStr)
		} else if hfSat > 50 {
			hfStr = warningStyle.Render(hfStr)
		} else {
			hfStr = successStyle.Render(hfStr)
		}
		pairs = append(pairs,
			[]string{"Token Velocity (TPS)", fmt.Sprintf("%.1f", vel)},
			[]string{"Gateway Squeeze Sat", sqStr},
			[]string{"HFSC Fragmenter Sat", hfStr},
		)
	}

	// Proxy Compression Efficiency
	if optRaw, ok := snapshot["opt_metrics"].(map[string]any); ok && len(optRaw) > 0 {
		rawBytes := numI64(optRaw, "total_raw_bytes")
		squeezedBytes := numI64(optRaw, "total_squeezed_bytes")
		efficiencyStr := "0.0%"
		if rawBytes > 0 {
			efficiency := 1.0 - (float64(squeezedBytes) / float64(rawBytes))
			efficiencyStr = fmt.Sprintf("%.2f%%", efficiency*100)
			if efficiency > 0.5 {
				efficiencyStr = successStyle.Render(efficiencyStr)
			} else if efficiency > 0.1 {
				efficiencyStr = warningStyle.Render(efficiencyStr)
			} else {
				efficiencyStr = errorStyle.Render(efficiencyStr)
			}
		}
		pairs = append(pairs,
			[]string{"Total Raw Volume", fmt.Sprintf("%.2f KB", float64(rawBytes)/1024.0)},
			[]string{"Total Minified Volume", fmt.Sprintf("%.2f KB", float64(squeezedBytes)/1024.0)},
			[]string{"Squeeze Efficiency", efficiencyStr},
		)
	}

	// IDE Client Sessions
	if ipcRaw, ok := snapshot["ipc_sessions"].(map[string]any); ok {
		connects := numI64(ipcRaw, "connects")
		disconnects := numI64(ipcRaw, "disconnects")
		active := numI64(ipcRaw, "active")
		activeStr := fmt.Sprintf("%d", active)
		if active > 0 {
			activeStr = successStyle.Render(activeStr)
		}
		pairs = append(pairs,
			[]string{"IDE Connects/Disconnects", fmt.Sprintf("%d / %d", connects, disconnects)},
			[]string{"Active IPC Sessions", activeStr},
		)
	}

	rows := mergeKeyValuePairsToRows(pairs, 2)
	telemetryBox := cardStyle.Render(subTitleStyle.Render("System Telemetry Matrix") + "\n" + renderStyledTable([]string{colMetric, colValue, colMetric, colValue}, rows))

	dbBox := renderDatabases(snapshot)

	return lipgloss.JoinVertical(lipgloss.Left, telemetryBox, dbBox)
}

// ── Analytics Components ──────────────────────────────────────────────────────

func renderFusionAnalytics(snapshot map[string]any) string {
	waiting := "Waiting for search telemetry..."
	if reason := searchTelemetryEmptyReason(snapshot); reason != "" {
		waiting = reason
	}
	searchRaw, ok := snapshot["search"].(map[string]any)
	if !ok || len(searchRaw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("Fusion Analytics") + "\n" + waiting)
	}
	fusionMode := str(searchRaw, "fusion_mode")
	if fusionMode == "" {
		fusionMode = str(searchRaw, "mode")
	}
	if fusionMode == "" {
		fusionMode = "Unknown"
	}
	fusionModeDisplay := warningStyle.Render("● " + fusionMode)
	if strings.Contains(fusionMode, "Hybrid") {
		fusionModeDisplay = successStyle.Render("● " + fusionMode)
	}
	vectorWins := numI64(searchRaw, "vector_wins")
	lexicalWins := numI64(searchRaw, "lexical_wins")
	graphSize := numI64(searchRaw, "hnsw_graph_size")
	avgConfidence := numF64(searchRaw, "total_confidence_score")
	cacheHits := numI64(searchRaw, "cache_hits")
	cacheMisses := numI64(searchRaw, "cache_misses")
	l1Hits := numI64(searchRaw, "l1_cache_hits")
	l1Misses := numI64(searchRaw, "l1_cache_misses")
	graphCompleteness := numF64(searchRaw, "graph_completeness") * 100
	gateRejections := numI64(searchRaw, "gate_rejections")
	vectorAttempts := numI64(searchRaw, "vector_attempts")
	vectorErrors := numI64(searchRaw, "vector_errors")
	staleSkips := numI64(searchRaw, "vector_stale_skips")
	embedLatency := numI64(searchRaw, "embed_latency_ms")

	dominanceStr := dashNA
	totalDecisions := vectorWins + lexicalWins
	if totalDecisions > 0 {
		ratio := float64(vectorWins) / float64(totalDecisions) * 100
		dominanceStr = fmt.Sprintf("%.1f%% Vector / %.1f%% Lexical", ratio, 100-ratio)
		if ratio >= 60 {
			dominanceStr = successStyle.Render(dominanceStr)
		} else if ratio >= 40 {
			dominanceStr = warningStyle.Render(dominanceStr)
		}
	}
	l1RateStr := dashNA
	if t := l1Hits + l1Misses; t > 0 {
		rate := float64(l1Hits) / float64(t) * 100
		l1RateStr = fmt.Sprintf("%.1f%%", rate)
		if rate >= 70 {
			l1RateStr = successStyle.Render(l1RateStr)
		} else if rate >= 40 {
			l1RateStr = warningStyle.Render(l1RateStr)
		}
	}
	l2RateStr := dashNA
	if t := cacheHits + cacheMisses; t > 0 {
		rate := float64(cacheHits) / float64(t) * 100
		l2RateStr = fmt.Sprintf("%.1f%%", rate)
		if rate >= 70 {
			l2RateStr = successStyle.Render(l2RateStr)
		} else if rate >= 40 {
			l2RateStr = warningStyle.Render(l2RateStr)
		}
	}
	graphSizeStr := fmt.Sprint(graphSize)
	if graphSize > 100 {
		graphSizeStr = successStyle.Render(graphSizeStr + " tools")
	} else if graphSize > 0 {
		graphSizeStr = warningStyle.Render(graphSizeStr + " tools")
	} else {
		graphSizeStr = errorStyle.Render("0 (disabled)")
	}
	graphCompleteStr := dashNA
	if graphCompleteness > 0 {
		graphCompleteStr = fmt.Sprintf("%.1f%%", graphCompleteness)
		if graphCompleteness >= 95 {
			graphCompleteStr = successStyle.Render(graphCompleteStr)
		} else if graphCompleteness >= 80 {
			graphCompleteStr = warningStyle.Render(graphCompleteStr)
		} else {
			graphCompleteStr = errorStyle.Render(graphCompleteStr)
		}
	}
	tbl := renderStyledTable([]string{colMetric, colValue}, [][]string{
		{"Fusion Mode", fusionModeDisplay},
		{"Vector Wins", fmt.Sprint(vectorWins)},
		{"Lexical Wins", fmt.Sprint(lexicalWins)},
		{"Engine Dominance", dominanceStr},
		{"HNSW Graph Size", graphSizeStr},
		{"Graph Completeness", graphCompleteStr},
		{"Avg Confidence (EMA)", fmt.Sprintf("%.4f", avgConfidence)},
		{"L1 Absorption Rate", l1RateStr},
		{"L2 Cache Hit Rate", l2RateStr},
		{"Gate Rejections", fmt.Sprint(gateRejections)},
		{"Vector Attempts", fmt.Sprint(vectorAttempts)},
		{"Vector Errors", fmt.Sprint(vectorErrors)},
		{"Vector Stale Skips", fmt.Sprint(staleSkips)},
		{"Embed Latency (cum.)", fmt.Sprintf("%d ms", embedLatency)},
	})
	return cardStyle.Render(subTitleStyle.Render("Fusion Analytics") + "\n" + tbl)
}

func renderLastQueryDiagnostics(snapshot map[string]any) string {
	searchRaw, ok := snapshot["search"].(map[string]any)
	if !ok {
		return cardStyle.Render(subTitleStyle.Render("Last Query Diagnostics") + "\n" + "No query trace available")
	}
	lastRaw, ok := searchRaw["last_query"].(map[string]any)
	if !ok || len(lastRaw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("Last Query Diagnostics") + "\n" + "No align_tools query trace recorded yet")
	}
	query := str(lastRaw, "query")
	if len(query) > 80 {
		query = query[:77] + "..."
	}
	winner := str(lastRaw, "fusion_winner")
	if winner == "" {
		winner = "-"
	}
	fastPath := str(lastRaw, "fast_path")
	if fastPath == "" {
		fastPath = "-"
	}
	gatesRejected := "no"
	if v, ok := lastRaw["gates_rejected"].(bool); ok && v {
		gatesRejected = errorStyle.Render("yes")
	}
	tbl := renderStyledTable([]string{"Field", colValue}, [][]string{
		{"Query", query},
		{"Fusion Winner", winner},
		{"Fast Path", fastPath},
		{"Gates Rejected", gatesRejected},
		{"Top BM25", fmt.Sprintf("%.4f", numF64(lastRaw, "top_bm25"))},
		{"Top Cosine", fmt.Sprintf("%.4f", numF64(lastRaw, "top_cosine"))},
		{"BM25 Squash Δ", fmt.Sprintf("%.4f", numF64(lastRaw, "bm25_squash_delta"))},
		{"Vector Attempted", fmt.Sprint(lastQueryBool(lastRaw, "vector_attempted"))},
	})
	return cardStyle.Render(subTitleStyle.Render("Last Query Diagnostics") + "\n" + tbl)
}

func lastQueryBool(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch b := v.(type) {
	case bool:
		return b
	default:
		return false
	}
}

func renderBiddingTable(snapshot map[string]any) string {
	raw, ok := snapshot["collisions"].(map[string]any)
	if !ok || len(raw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("Semantic Collisions") + "\n" + "No search events captured yet. Collisions appear after align_tools queries.")
	}

	eventsRaw := sliceFrom(raw["events"])
	var biddingRows [][]string
	for _, e := range eventsRaw {
		evt, ok := e.(map[string]any)
		if !ok {
			continue
		}
		query := str(evt, "query")
		if len(query) > 30 {
			query = query[:30] + "..."
		}
		gap := numF64(evt, "gap")
		gapStr := fmt.Sprintf("%.4f", gap)
		status := successStyle.Render("● Healthy")
		if gap < 0.05 {
			status = errorStyle.Render("● Collision")
			gapStr = errorStyle.Render(gapStr)
		} else if gap < 0.10 {
			status = warningStyle.Render("● Narrow")
			gapStr = warningStyle.Render(gapStr)
		} else {
			gapStr = successStyle.Render(gapStr)
		}
		biddingRows = append(biddingRows, []string{query, str(evt, "s1_urn"), str(evt, "s2_urn"), gapStr, status})
	}
	biddingContent := "No events."
	if len(biddingRows) > 0 {
		biddingContent = renderStyledTable([]string{"Query", "S1", "S2", "Gap", colStatus}, biddingRows)
	}
	return cardStyle.Render(subTitleStyle.Render("Bidding Table (Recent Searches)") + "\n" + biddingContent)
}

func renderTopCollisionPairs(snapshot map[string]any) string {
	raw, ok := snapshot["collisions"].(map[string]any)
	if !ok || len(raw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("Top Collision Pairs") + "\n" + "No search events captured yet.")
	}

	pairsRaw := sliceFrom(raw["top_pairs"])
	pairsContent := "No collision pairs detected."
	if len(pairsRaw) > 0 {
		var pairRows [][]string
		for _, p := range pairsRaw {
			pair, ok := p.(map[string]any)
			if !ok {
				continue
			}
			pairRows = append(pairRows, []string{str(pair, "urn_a"), str(pair, "urn_b"), errorStyle.Render(str(pair, "count"))})
		}
		if len(pairRows) > 0 {
			pairsContent = renderStyledTable([]string{"Tool A", "Tool B", "Count"}, pairRows)
		}
	}
	return cardStyle.Render(subTitleStyle.Render("Top Collision Pairs") + "\n" + pairsContent)
}

// ── Helpers ─────────────────────────────────────────────────────────────────

func str(m map[string]any, key string) string {
	if s := stringFrom(m[key]); s != "" {
		return s
	}
	return "-"
}

func numF64(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	default:
		return 0
	}
}

func numI64(m map[string]any, key string) int64 {
	return int64From(m[key])
}

func wordWrap(text string, maxWidth int) string {
	if maxWidth <= 0 {
		maxWidth = 80
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}
	var lines []string
	current := words[0]
	for _, w := range words[1:] {
		if len(current)+1+len(w) > maxWidth {
			lines = append(lines, current)
			current = w
		} else {
			current += " " + w
		}
	}
	lines = append(lines, current)
	return strings.Join(lines, "\n")
}

// ── Tab 9: Logging ───────────────────────────────────────────────────────────

func buildLoggingTab(snapshot map[string]any) string {
	var s strings.Builder
	s.WriteString(dashTitleStyle.Render("Recent Errors Log"))
	s.WriteByte('\n')

	var errRows [][]string
	if recentErrors, ok := snapshot["recent_errors"].([]any); ok && len(recentErrors) > 0 {
		for _, errItem := range recentErrors {
			if e, ok := errItem.(map[string]any); ok {
				errRows = append(errRows, []string{
					str(e, "timestamp"),
					str(e, "category"),
					str(e, "server"),
					wordWrap(str(e, "message"), 60),
				})
			}
		}
	} else {
		errRows = append(errRows, []string{"-", "No recent errors", "-", "-"})
	}
	errorBox := cardStyle.Render(subTitleStyle.Render("Recent Errors Log") + "\n" + renderStyledTable([]string{"Time", "Category", "Origin", "Message"}, errRows))

	s.WriteString(errorBox)
	return s.String()
}

// ── Tab 8: Distributed Tracing ───────────────────────────────────────────────

func buildTracingTab(snapshot map[string]any) string {
	var s strings.Builder
	s.WriteString(dashTitleStyle.Render("Distributed Tracing Backplane"))
	s.WriteByte('\n')

	traceRaw, ok := snapshot["distributed_tracing"].(map[string]any)
	if !ok || len(traceRaw) == 0 {
		s.WriteString(cardStyle.Render("Waiting for distributed tracing telemetry..."))
		return s.String()
	}

	parent := str(traceRaw, "cascade_parent")
	source := str(traceRaw, "cascade_source")

	s.WriteString(cardStyle.Render(fmt.Sprintf("%s\nCurrent Cascade Source Node: %s\nCurrent Cascade Parent Correlation UUID: %s",
		subTitleStyle.Render("Execution Lock Status"),
		cyanStyle.Render(source),
		cyanStyle.Render(parent),
	)))
	s.WriteByte('\n')

	var rows [][]string
	if spans, ok := traceRaw["active_spans"].(map[string]any); ok && len(spans) > 0 {
		for server, uuid := range spans {
			rows = append(rows, []string{server, fmt.Sprint(uuid)})
		}
	} else {
		rows = append(rows, []string{"No active execution spans.", "-"})
	}

	spanTable := renderStyledTable([]string{"Node (Server)", "Correlation UUID"}, rows)
	s.WriteString(cardStyle.Render(subTitleStyle.Render("Active Cross-Node Spans") + "\n" + spanTable))

	return s.String()
}
