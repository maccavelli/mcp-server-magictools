// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/lipgloss/table"
)

const loadingText = "⏳ Loading telemetry data..."

var (
	subTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			MarginTop(1).
			MarginBottom(1)

	metricLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("241"))

	metricValueStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Bold(true)

	cardStyle = lipgloss.NewStyle().
			Padding(0, 1).
			MarginRight(2).
			MarginBottom(0)

	successStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	warningStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	errorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("197"))
	cyanStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("81"))
	grayStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	magentaStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("170"))
	boldStyle    = lipgloss.NewStyle().Bold(true)

	tableBorderStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("238"))
)

// renderStyledTable builds a lipgloss table from headers and rows.
func renderStyledTable(headers []string, rows [][]string) string {
	styledHeaders := make([]string, len(headers))
	for i, h := range headers {
		styledHeaders[i] = metricLabelStyle.Render(h)
	}
	t := table.New().
		Border(lipgloss.NormalBorder()).
		BorderStyle(tableBorderStyle).
		Headers(styledHeaders...)
	for _, row := range rows {
		styledRow := make([]string, len(row))
		for i, cell := range row {
			if i == 0 {
				styledRow[i] = magentaStyle.Render(cell)
				continue
			}
			styledRow[i] = metricValueStyle.Render(cell)
		}
		t.Row(styledRow...)
	}
	return t.Render()
}

// colorDelta formats a float delta with +/- sign and green/red/neutral coloring.
func colorDelta(d float64) string {
	s := fmt.Sprintf("%+.3f", d)
	if d > 0.001 {
		return successStyle.Render(s)
	} else if d < -0.001 {
		return errorStyle.Render(s)
	}
	return s
}

// ── Tab 1: Overview ─────────────────────────────────────────────────────────

func renderFleetComms(snapshot map[string]any) string {
	proxyRaw, proxyOk := snapshot["proxy"].(map[string]any)
	commsContent := "Waiting for transport data..."
	if proxyOk && len(proxyRaw) > 0 {
		commsContent = "No transport data yet."
		if serversRaw, ok := proxyRaw["servers"].(map[string]any); ok && len(serversRaw) > 0 {
			var names []string
			for n := range serversRaw {
				names = append(names, n)
			}
			sort.Strings(names)
			var rows [][]string
			for _, name := range names {
				m, ok := serversRaw[name].(map[string]any)
				if !ok {
					continue
				}
				bytesRaw := numI64(m, "bytes_raw")
				bytesMin := numI64(m, "bytes_minified")
				faults := numI64(m, "faults")
				calls := numI64(m, "calls")
				spinup := numI64(m, "total_spinup_ms")

				squeezeStr := dashNA
				if bytesRaw > 0 {
					ratio := float64(bytesMin) / float64(bytesRaw) * 100
					squeezeStr = fmt.Sprintf("%.1f%%", ratio)
					if ratio < 50 {
						squeezeStr = successStyle.Render(squeezeStr)
					} else if ratio < 80 {
						squeezeStr = warningStyle.Render(squeezeStr)
					}
				}
				health := successStyle.Render("● OK")
				if faults > 0 && calls > 0 {
					faultRate := float64(faults) / float64(calls) * 100
					if faultRate > 10 {
						health = errorStyle.Render("● DEGRADED")
					} else if faultRate > 2 {
						health = warningStyle.Render("● WARN")
					}
				}
				rows = append(rows, []string{name, fmt.Sprint(calls), fmt.Sprint(spinup), fmt.Sprint(numI64(m, "bytes_sent")), fmt.Sprint(bytesMin), squeezeStr, fmt.Sprint(faults), health})
			}
			commsContent = renderStyledTable([]string{colServer, colCalls, "Spinup ms", "Bytes Out", "Bytes Min", "Squeeze %", "Faults", "Health"}, rows)
		}
	}
	return cardStyle.Render(subTitleStyle.Render("Fleet Transport & Throughput") + "\n" + commsContent)
}

func mergeKeyValuePairsToRows(pairs [][]string, cols int) [][]string {
	var result [][]string
	var currentRow []string
	for _, p := range pairs {
		currentRow = append(currentRow, p[0], p[1])
		if len(currentRow) == cols*2 {
			result = append(result, currentRow)
			currentRow = nil
		}
	}
	if len(currentRow) > 0 {
		for len(currentRow) < cols*2 {
			currentRow = append(currentRow, "", "")
		}
		result = append(result, currentRow)
	}
	return result
}

func buildSummaryTab(snapshot map[string]any, _ []string) string { //nolint:gocritic // dashboard table rows built incrementally for readability
	header := boldStyle.Render("Magictools Orchestrator Overview")

	udpPort := "unknown"
	if udpRaw, ok := snapshot["udp_telemetry"].(map[string]any); ok {
		if portAny, ok := udpRaw["bound_port"]; ok {
			udpPort = fmt.Sprint(portAny)
		}
	}
	udpIndicator := successStyle.Render(fmt.Sprintf("Server Connected (udp:%s)", udpPort))

	titleBox := lipgloss.JoinVertical(lipgloss.Left, header, udpIndicator, "")

	configRaw := mapFrom(snapshot["config"])
	sysRaw := mapFrom(snapshot["system"])
	regRaw := mapFrom(snapshot["registry"])
	proxyRaw := mapFrom(snapshot["proxy"])

	var pairs [][]string

	pairs = append(pairs,
		[]string{"Logging Level", str(configRaw, "log_level")},
		[]string{"Goroutines", fmt.Sprint(numI64(sysRaw, "num_goroutine"))},
		[]string{"Heap Allocated", fmt.Sprintf("%.1f MB", numF64(sysRaw, "heap_alloc_mb"))},
		[]string{"Registered Tools", fmt.Sprint(numI64(regRaw, "total_tools"))},
		[]string{"Configured Sub-Servers", fmt.Sprint(numI64(regRaw, "total_servers"))},
		[]string{"Cache Hits", fmt.Sprint(numI64(regRaw, "cache_hits"))},
	)

	if latRaw, ok := proxyRaw["latencies"].(map[string]any); ok {
		pairs = append(pairs,
			[]string{"EMA align_tools (ms)", fmt.Sprintf("%.1f (n=%d)", numF64(latRaw, "align_tools_ema"), numI64(latRaw, "align_tools_count"))},
			[]string{"EMA call_proxy (ms)", fmt.Sprintf("%.1f (n=%d)", numF64(latRaw, "call_proxy_ema"), numI64(latRaw, "call_proxy_count"))},
			[]string{"EMA call_proxy hot (ms)", fmt.Sprintf("%.1f (n=%d)", numF64(latRaw, "call_proxy_hot_ema"), numI64(latRaw, "call_proxy_hot_cnt"))},
			[]string{"EMA boot cold (ms)", fmt.Sprintf("%.1f (n=%d)", numF64(latRaw, "boot_ema"), numI64(latRaw, "boot_count"))},
		)
	}

	if statsRaw, ok := proxyRaw["session_stats"].(map[string]any); ok {
		if digestRaw, ok := statsRaw["digest"].(map[string]any); ok {
			pairs = append(pairs,
				[]string{"Digest Total Calls", str(digestRaw, "total_calls")},
				[]string{"Digest Total Faults", str(digestRaw, "total_faults")},
				[]string{"Digest Tokens Used", str(digestRaw, "tokens_used")},
				[]string{"Digest Tokens Saved", str(digestRaw, "tokens_saved")},
			)
		}
	}

	if raw, ok := snapshot["collisions"].(map[string]any); ok && len(raw) > 0 {
		avgGap := numF64(raw, "avg_gap")
		trend := str(raw, "trend")
		var trendIcon string
		switch trend {
		case "improving":
			trendIcon = successStyle.Render("▲ Improving")
		case "degrading":
			trendIcon = errorStyle.Render("▼ Degrading")
		case "stable":
			trendIcon = cyanStyle.Render("● Stable")
		default:
			trendIcon = grayStyle.Render("◌ " + trend)
		}
		if trendIcon == "" {
			trendIcon = "➡️"
		}
		avgGapStr := fmt.Sprintf("%.4f", avgGap)
		if avgGap < 0.05 {
			avgGapStr = errorStyle.Render(avgGapStr)
		} else if avgGap < 0.10 {
			avgGapStr = warningStyle.Render(avgGapStr)
		} else {
			avgGapStr = successStyle.Render(avgGapStr)
		}
		pairs = append(pairs,
			[]string{"Total Search Events", str(raw, "total_events")},
			[]string{"Total Collisions", str(raw, "total_collisions")},
			[]string{"Avg Confidence Gap", avgGapStr},
			[]string{"Trend Direction", trendIcon},
		)
	}

	rows := mergeKeyValuePairsToRows(pairs, 3)
	telemetryBox := cardStyle.Render(subTitleStyle.Render("Global Telemetry & Latency Profiling") + "\n" + renderStyledTable([]string{colMetric, colValue, colMetric, colValue, colMetric, colValue}, rows))

	firewallBox := renderFirewallHealth(snapshot)
	pairsBox := renderTopCollisionPairs(snapshot)
	bottomRow := lipgloss.JoinHorizontal(lipgloss.Top, firewallBox, pairsBox)

	return lipgloss.JoinVertical(lipgloss.Left, titleBox, telemetryBox, bottomRow)
}

func buildFleetTransportTab(snapshot map[string]any) string {
	fleetBox := renderFleet(snapshot)
	commsBox := renderFleetComms(snapshot)
	muxBox := renderMuxLinks(snapshot)
	muxHealthBox := renderMuxHealth(snapshot)

	row1 := lipgloss.JoinHorizontal(lipgloss.Top, fleetBox, commsBox)
	row2 := lipgloss.JoinHorizontal(lipgloss.Top, muxHealthBox, muxBox)
	return lipgloss.JoinVertical(lipgloss.Left, row1, row2)
}

func renderFirewallHealth(snapshot map[string]any) string {
	fwRaw, ok := snapshot["firewall"].(map[string]any)
	if !ok || len(fwRaw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("JSON-RPC Stdio Firewall") + "\n" + grayStyle.Render("No firewall data yet."))
	}
	var rows [][]string
	for name, mAny := range fwRaw {
		m, ok := mAny.(map[string]any)
		if !ok {
			continue
		}
		valid := numI64(m, "valid_frames")
		dropped := numI64(m, "dropped_frames")

		statusStr := successStyle.Render("Clean")
		if dropped > 0 {
			statusStr = warningStyle.Render(fmt.Sprintf("%d dropped", dropped))
		}

		lastDrop := "-"
		if v, ok := m["last_drop_ago"].(string); ok && v != "" {
			lastDrop = v
		}

		rows = append(rows, []string{name, fmt.Sprint(valid), statusStr, lastDrop})
		if len(rows) >= 15 {
			break
		}
	}
	return cardStyle.Render(subTitleStyle.Render("JSON-RPC Stdio Firewall") + "\n" + renderStyledTable([]string{colServer, "Valid", colStatus, "Last Drop"}, rows))
}

func renderFleet(snapshot map[string]any) string {
	serversRaw, ok := snapshot["servers"].([]any)
	if !ok || len(serversRaw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("Fleet Status") + "\n" + loadingText)
	}

	var rows [][]string
	for _, raw := range serversRaw {
		s, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		statusStr := errorStyle.Render("●")
		if s["running"] == true {
			statusStr = successStyle.Render("●")
		}
		rows = append(rows, []string{
			str(s, "name"), statusStr, str(s, "uptime"),
			str(s, "total_calls"), str(s, "last_latency"), str(s, "ping_latency"),
			str(s, "consecutive_errors"),
		})
		if len(rows) >= 15 {
			break
		}
	}

	tbl := renderStyledTable([]string{colServer, colStatus, "Uptime", colCalls, "Latency", "Ping", "Errors"}, rows)
	return cardStyle.Render(subTitleStyle.Render("Fleet Status") + "\n" + tbl)
}

// ── Tab 2: Intelligence & Routing ───────────────────────────────────────────

func renderToolAnalytics(snapshot map[string]any) string {
	toolsRaw, ok := snapshot["tools"].(map[string]any)
	mainBox := cardStyle.Render(subTitleStyle.Render("Tool Analytics") + "\n" + "No tool telemetry data recorded yet.")
	if ok && len(toolsRaw) > 0 {
		var urns []string
		for u := range toolsRaw {
			urns = append(urns, u)
		}
		sort.Strings(urns)

		var rows [][]string
		for _, urn := range urns {
			raw := toolsRaw[urn]
			t, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			calls := numI64(t, colCalls)
			totalMs := numI64(t, "TotalMs")
			faults := numI64(t, "Faults")
			lastAt := numI64(t, "LastCallAt")

			if calls == 0 {
				continue
			}

			avgMs := totalMs / calls

			latStr := fmt.Sprintf("%d", avgMs)
			if avgMs > 500 {
				latStr = errorStyle.Render(latStr)
			} else if avgMs > 100 {
				latStr = warningStyle.Render(latStr)
			} else {
				latStr = successStyle.Render(latStr)
			}

			faultStr := fmt.Sprint(faults)
			if faults > 0 {
				faultStr = errorStyle.Render(faultStr)
			}

			lastCallStr := "-"
			if lastAt > 0 {
				lastCallStr = time.Since(time.Unix(0, lastAt)).Truncate(time.Second).String() + " ago"
			}

			rows = append(rows, []string{urn, fmt.Sprint(calls), latStr, faultStr, lastCallStr})
		}
		if len(rows) > 15 {
			rows = rows[:15]
		}
		if len(rows) == 0 {
			mainBox = cardStyle.Render(subTitleStyle.Render("Tool Analytics") + "\n" + "No tool telemetry data recorded yet.")
		} else {
			mainBox = cardStyle.Render(subTitleStyle.Render(fmt.Sprintf("Tool Analytics (%d tools)", len(rows))) + "\n" + renderStyledTable([]string{colURN, colCalls, "Avg ms", "Faults", "Last Call"}, rows))
		}
	}
	return mainBox
}

func renderCrossServerRouting(snapshot map[string]any) string {
	routeContent := "Waiting for cross-server routing telemetry..."
	if routesRaw, rOk := snapshot["cross_server_routes"].([]any); rOk && len(routesRaw) > 0 {
		var rRows [][]string
		for _, r := range routesRaw {
			m, ok := r.(map[string]any)
			if !ok {
				continue
			}
			source := str(m, "source")
			target := str(m, "target")
			calls := numI64(m, "calls")
			faults := numI64(m, "faults")

			// Only show degraded/flaky routes
			if faults == 0 {
				continue
			}

			status := warningStyle.Render("● FLAKY")
			if calls > 0 && float64(faults)/float64(calls) > 0.1 {
				status = errorStyle.Render("● DEGRADED")
			}
			rRows = append(rRows, []string{source, cyanStyle.Render("➔ ") + target, fmt.Sprint(calls), status})
		}
		if len(rRows) == 0 {
			routeContent = successStyle.Render("All cross-server routes are healthy.")
		} else {
			routeContent = renderStyledTable([]string{"Source Server", "Target Server", "Proxy Invocations", colStatus}, rRows)
		}
	}
	return cardStyle.Render(subTitleStyle.Render("Cross-Server Routing Matrix (Degraded Only)") + "\n" + routeContent)
}

func renderToolReliability(snapshot map[string]any) string {
	rawScores, okScores := snapshot["scores"].(map[string]ToolScoreCard)
	relBox := cardStyle.Render(subTitleStyle.Render("Tool Reliability Scores") + "\n" + "Waiting for tool calls... scores appear after the first proxy call.")
	if okScores && len(rawScores) > 0 {
		var arr []ToolScoreCard
		for _, card := range rawScores {
			arr = append(arr, card)
		}
		sort.SliceStable(arr, func(i, j int) bool {
			if arr[i].Reliability != arr[j].Reliability {
				return arr[i].Reliability > arr[j].Reliability
			}
			return arr[i].URN < arr[j].URN
		})
		if len(arr) > 20 {
			arr = arr[:20]
		}

		var rows [][]string
		for _, c := range arr {
			relPct := c.Reliability * 100
			relStr := fmt.Sprintf("%.1f%%", relPct)
			if relPct >= 95 {
				relStr = successStyle.Render(relStr)
			} else if relPct >= 80 {
				relStr = warningStyle.Render(relStr)
			} else {
				relStr = errorStyle.Render(relStr)
			}
			rows = append(rows, []string{c.URN, relStr, fmt.Sprintf("%.3f", c.Baseline), colorDelta(c.Deviation), colorDelta(c.Delta30m), colorDelta(c.Delta4h), colorDelta(c.DeltaAll)})
		}

		relBox = cardStyle.Render(subTitleStyle.Render(fmt.Sprintf("Tool Reliability (Top %d) — 10s refresh", len(arr))) + "\n" + renderStyledTable([]string{colURN, "Rel%", "Base", "Dev", "30m Δ", "4h Δ", "All Δ"}, rows))
	}
	return relBox
}

func renderScoringDrift(snapshot map[string]any) string {
	driftContent := "Waiting for drift telemetry..."
	if driftRaw, dOk := snapshot["scoring_factors"].([]any); dOk && len(driftRaw) > 0 {
		var dRows [][]string
		for _, d := range driftRaw {
			if m, ok := d.(map[string]any); ok {
				dRows = append(dRows, []string{str(m, "category"), fmt.Sprint(numI64(m, "count")), str(m, "impact_type")})
			}
		}
		driftContent = renderStyledTable([]string{"Factor Category", "Impact Count", "Penalty/Reward"}, dRows)
	}
	return cardStyle.Render(subTitleStyle.Render("Scoring Drift Factors") + "\n" + driftContent)
}

func renderToolVolatility(snapshot map[string]any) string {
	volatilityContent := "Waiting for volatility index..."
	if volRaw, vOk := snapshot["volatility_index"].([]any); vOk && len(volRaw) > 0 {
		var vRows [][]string
		for _, v := range volRaw {
			m, ok := v.(map[string]any)
			if !ok {
				continue
			}
			score := numF64(m, "score")
			scoreStr := fmt.Sprintf("%.2f", score)
			status := successStyle.Render("STABLE")
			if score > 5.0 {
				status = errorStyle.Render("HIGH VOLATILITY")
				scoreStr = errorStyle.Render(scoreStr)
			} else if score > 2.0 {
				status = warningStyle.Render("ELEVATED")
				scoreStr = warningStyle.Render(scoreStr)
			}
			vRows = append(vRows, []string{str(m, "urn"), scoreStr, status})
		}
		volatilityContent = renderStyledTable([]string{colURN, "Volatility Score", "Stability Status"}, vRows)
	}
	return cardStyle.Render(subTitleStyle.Render("Tool Volatility Tracking") + "\n" + volatilityContent)
}

func renderRAGConfidence(snapshot map[string]any) string {
	waiting := "Waiting for RAG vector telemetry..."
	if reason := searchTelemetryEmptyReason(snapshot); reason != "" {
		waiting = reason
	}
	ragBox := cardStyle.Render(subTitleStyle.Render("RAG Confidence Map") + "\n" + waiting)
	searchRaw, ok := snapshot["search"].(map[string]any)
	if ok && len(searchRaw) > 0 {
		totalSearches := numI64(searchRaw, "vector_searches")
		totalConfidence := numF64(searchRaw, "total_confidence_score")

		avgConf := 0.0
		avgConfStr := dashNA
		alertStr := successStyle.Render("● Healthy")

		if totalSearches > 0 {
			// BUG FIX: totalConfidence is an EMA provided by the backend, not a cumulative sum.
			// We should not divide it by totalSearches.
			avgConf = totalConfidence
			avgConfStr = fmt.Sprintf("%.4f", avgConf)
			if totalSearches < 5 {
				alertStr = cyanStyle.Render("● CALIBRATING")
				avgConfStr = cyanStyle.Render(avgConfStr)
			} else if avgConf < 0.60 {
				alertStr = errorStyle.Render("● DANGER (Hallucination Risk)")
				avgConfStr = errorStyle.Render(avgConfStr)
			} else if avgConf < 0.80 {
				alertStr = warningStyle.Render("● CAUTION (Sub-optimal Alignment)")
				avgConfStr = warningStyle.Render(avgConfStr)
			} else {
				avgConfStr = successStyle.Render(avgConfStr)
			}
		}

		tbl := renderStyledTable([]string{colMetric, colValue}, [][]string{
			{"Vector Operations", fmt.Sprintf("%d", totalSearches)},
			{"EMA Vector Confidence", fmt.Sprintf("%.4f", totalConfidence)},
			{"Avg Comp. Confidence", avgConfStr},
			{"RAG Safety Status", alertStr},
		})
		ragBox = cardStyle.Render(subTitleStyle.Render("Vector Search Confidence Map (RAG)") + "\n" + tbl)
	}
	return ragBox
}

func renderSearchIntelligence(snapshot map[string]any) (string, string) { //nolint:gocritic // dashboard table rows built incrementally for readability
	emptyReason := searchTelemetryEmptyReason(snapshot)
	mainWaiting := "Waiting for search telemetry..."
	if emptyReason != "" {
		mainWaiting = emptyReason
	}
	mainBox := cardStyle.Render(subTitleStyle.Render("Search Intelligence") + "\n" + mainWaiting)

	matrixReason := indexMatrixEmptyReason(snapshot)
	compWaiting := "Waiting for routing comparison telemetry..."
	if matrixReason != "" {
		compWaiting = matrixReason
	}
	compBox := cardStyle.Render(subTitleStyle.Render("Index Decision Matrix") + "\n" + compWaiting)

	searchRaw, ok := snapshot["search"].(map[string]any)
	if !ok || len(searchRaw) == 0 {
		return mainBox, compBox
	}

	mode := str(searchRaw, "mode")
	modeDisplay := warningStyle.Render("● " + mode)
	if strings.Contains(mode, "Hybrid") || strings.Contains(mode, "Vector") {
		modeDisplay = successStyle.Render("● " + mode)
	}

	totalSearches := numI64(searchRaw, "total_searches")
	vectorSearches := numI64(searchRaw, "vector_searches")
	lexicalSearches := numI64(searchRaw, "lexical_searches")

	vectorRatioStr := dashNA
	if totalSearches > 0 {
		ratio := float64(vectorSearches) / float64(totalSearches) * 100
		vectorRatioStr = fmt.Sprintf("%.1f%%", ratio)
		if ratio >= 80 {
			vectorRatioStr = successStyle.Render(vectorRatioStr)
		} else if ratio >= 50 {
			vectorRatioStr = warningStyle.Render(vectorRatioStr)
		}
	}

	semRaw := mapFrom(snapshot["semantic_recall"])
	intentRaw := mapFrom(snapshot["intent_routing"])
	schemaRaw := mapFrom(snapshot["schema_health"])
	orchRaw := mapFrom(snapshot["orchestrator"])

	avgLatency := numF64(searchRaw, "avg_latency_ms")
	if avgLatency == 0 {
		avgLatency = numF64(intentRaw, "avg_latency_ms")
	}

	bleveDocs := numI64(semRaw, "bleve_doc_count")
	if bleveDocs == 0 {
		bleveDocs = numI64(semRaw, "doc_count")
	}
	hnswNodes := numI64(semRaw, "hnsw_node_count")
	if hnswNodes == 0 {
		hnswNodes = numI64(searchRaw, "hnsw_graph_size")
	}

	managedServers := numI64(schemaRaw, "managed_subservers")
	if managedServers == 0 {
		managedServers = numI64(schemaRaw, "valid_routes")
	}
	vectorRoutingWins := numI64(orchRaw, "vector_routing_wins")
	if vectorRoutingWins == 0 {
		vectorRoutingWins = numI64(orchRaw, "dynamic_intercepts")
	}

	var pairs [][]string
	pairs = append(pairs,
		[]string{"Active Mode", modeDisplay},
		[]string{"Total Searches", fmt.Sprint(totalSearches)},
		[]string{"Vector (HNSW)", fmt.Sprint(vectorSearches)},
		[]string{"Lexical (Bleve)", fmt.Sprint(lexicalSearches)},
		[]string{"Vector Ratio", vectorRatioStr},
		[]string{"Search Latency (avg)", fmt.Sprintf("%.1f ms", avgLatency)},
		[]string{"Cumulative Latency", fmt.Sprintf("%d ms", numI64(searchRaw, "total_latency_ms"))},
		[]string{"Learning Weight", fmt.Sprintf("%.4f", numF64(searchRaw, "learning_weight"))},
		[]string{"Index Path", str(semRaw, "index_path")},
		[]string{"Index Size", fmt.Sprintf("%.2f MB", numF64(semRaw, "index_size_mb"))},
		[]string{"Bleve Documents", fmt.Sprint(bleveDocs)},
		[]string{"HNSW Nodes", fmt.Sprint(hnswNodes)},
		[]string{"Schema Mutations", fmt.Sprint(numI64(semRaw, "schema_mutations"))},
		[]string{"Intent Queries", fmt.Sprint(numI64(intentRaw, "total_queries"))},
		[]string{"Cache Hit Ratio", fmt.Sprintf("%.1f%%", numF64(intentRaw, "cache_hit_ratio")*100)},
		[]string{"Avg Intent Latency", fmt.Sprintf("%.1f ms", numF64(intentRaw, "avg_latency_ms"))},
		[]string{"Managed Sub-Servers", fmt.Sprint(managedServers)},
		[]string{"Invalid Matrix Routes", fmt.Sprint(numI64(schemaRaw, "invalid_routes"))},
		[]string{"Vector Routing Wins", fmt.Sprint(vectorRoutingWins)},
		[]string{"Fallback Invocs", fmt.Sprint(numI64(orchRaw, "fallback_invocations"))},
	)

	rows := mergeKeyValuePairsToRows(pairs, 3)
	mainBox = cardStyle.Render(subTitleStyle.Render("Search Intelligence Matrix") + "\n" + renderStyledTable([]string{colMetric, colValue, colMetric, colValue, colMetric, colValue}, rows))

	compContent := compWaiting
	bleveTop, bOk := searchRaw["bleve_top_5"].([]any)
	hnswTop, hOk := searchRaw["hnsw_top_5"].([]any)
	if bOk || hOk {
		var compRows [][]string
		for i := range 5 {
			bStr, hStr := "-", "-"
			if i < len(bleveTop) {
				if s, ok := bleveTop[i].(string); ok {
					bStr = s
				}
			}
			if i < len(hnswTop) {
				if s, ok := hnswTop[i].(string); ok {
					hStr = s
					if bStr != hStr {
						hStr = successStyle.Render(hStr)
					}
				}
			}
			compRows = append(compRows, []string{fmt.Sprintf("#%d", i+1), bStr, hStr})
		}
		compContent = renderStyledTable([]string{"Rank", "Bleve Baseline", "Vector Routing"}, compRows)
		if l1Hits := numI64(searchRaw, "l1_cache_hits"); l1Hits > 0 {
			compContent += "\n" + cyanStyle.Render("Note: matrix may reflect last L1 miss or cached fusion snapshot")
		}
	}
	compBox = cardStyle.Render(subTitleStyle.Render("Index Decision Matrix") + "\n" + compContent)
	return mainBox, compBox
}

func buildToolIntelligenceTab(snapshot map[string]any) string {
	searchMatrixBox, compBox := renderSearchIntelligence(snapshot)
	ragBox := renderRAGConfidence(snapshot)
	fusionBox := renderFusionAnalytics(snapshot)
	lastQueryBox := renderLastQueryDiagnostics(snapshot)

	row2 := lipgloss.JoinHorizontal(lipgloss.Top, ragBox, compBox)
	row3 := lipgloss.JoinHorizontal(lipgloss.Top, fusionBox, lastQueryBox)

	return lipgloss.JoinVertical(lipgloss.Left, searchMatrixBox, row2, row3)
}

func buildToolAnalyticsTab(snapshot map[string]any) string {
	routeBox := renderCrossServerRouting(snapshot)
	toolAnalyticsBox := renderToolAnalytics(snapshot)
	relBox := renderToolReliability(snapshot)
	volBox := renderToolVolatility(snapshot)
	biddingBox := renderBiddingTable(snapshot)
	driftBox := renderScoringDrift(snapshot)

	col1 := lipgloss.JoinVertical(lipgloss.Left, toolAnalyticsBox, relBox, biddingBox)
	col2 := lipgloss.JoinVertical(lipgloss.Left, routeBox, volBox, driftBox)

	return lipgloss.JoinHorizontal(lipgloss.Top, col1, col2)
}

// Note: The above functions replace the old tool routing and scoring tabs.
