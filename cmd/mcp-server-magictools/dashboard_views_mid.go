// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ── Tab 3: Orchestration & DAG ───────────────────────────────────────────────

func renderPipelineOpt(snapshot map[string]any) string {
	optRaw, ok := snapshot["opt_metrics"].(map[string]any)
	if !ok || len(optRaw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("Pipeline Optimization") + "\n" + loadingText)
	}

	sqTbl := renderStyledTable([]string{colMetric, colValue}, [][]string{
		{"Bypass Count", str(optRaw, "squeeze_bypass")},
		{"Truncations", str(optRaw, "squeeze_trunc")},
	})
	sqBox := cardStyle.Render(subTitleStyle.Render("Squeeze Writer") + "\n" + sqTbl)

	hfTbl := renderStyledTable([]string{colMetric, colValue}, [][]string{
		{"Reassembly OK", str(optRaw, "hfsc_success")},
		{"Reassembly Fail", str(optRaw, "hfsc_fail")},
		{"Swept Stale", str(optRaw, "hfsc_swept")},
		{"Active Streams", str(optRaw, "hfsc_active")},
	})
	hfBox := cardStyle.Render(subTitleStyle.Render("HFSC Fragmenter") + "\n" + hfTbl)

	csTbl := renderStyledTable([]string{colMetric, colValue}, [][]string{
		{"Offload Bytes", str(optRaw, "cssa_offload")},
		{"Sync Operations", str(optRaw, "cssa_sync")},
	})
	csBox := cardStyle.Render(subTitleStyle.Render("CSSA Offload") + "\n" + csTbl)

	return lipgloss.JoinHorizontal(lipgloss.Top, sqBox, hfBox, csBox)
}

func renderExecutionCascade(dagRaw map[string]any) string {
	var cascadeContent string
	nodesRaw, nOk := dagRaw["nodes"].([]any)
	if nOk && len(nodesRaw) > 0 {
		var rows [][]string
		for i, n := range nodesRaw {
			node := mapFrom(n)
			state := str(node, "state")
			stateStr := state
			switch state {
			case "DONE":
				stateStr = successStyle.Render(state)
			case "EXECUTING":
				stateStr = warningStyle.Render("▶ " + state)
			case "WAITING", "READY":
				stateStr = grayStyle.Render(state)
			case "FAILED":
				stateStr = errorStyle.Render("✖ " + state)
			}
			rows = append(rows, []string{fmt.Sprintf("%d", i+1), str(node, "name"), stateStr, str(node, "latency")})
		}
		cascadeContent = renderStyledTable([]string{"Step", "Node / Tool URN", "State", "Latency"}, rows)
	} else {
		cascadeContent = "No nodes generated."
	}
	return cardStyle.Render(subTitleStyle.Render("Execution Cascade") + "\n" + cascadeContent)
}

func renderPayloadInspector(activeNode map[string]any) string {
	inspectorContent := "Waiting for node activation..."
	if len(activeNode) > 0 {
		inspectorContent = renderStyledTable([]string{colMetric, colValue}, [][]string{
			{"Active Target", cyanStyle.Render(str(activeNode, "name"))},
			{"Raw Payload (bytes)", fmt.Sprint(numI64(activeNode, "bytes_raw"))},
			{"Minified (bytes)", fmt.Sprint(numI64(activeNode, "bytes_minified"))},
			{"Tokens Processed", fmt.Sprint(numI64(activeNode, "tokens"))},
			{"Cache Action", str(activeNode, "cache_action")},
			{"CSSA Matrix Hash", str(activeNode, "cssa_hash")},
		})
	}
	return cardStyle.Render(subTitleStyle.Render("Payload Inspector") + "\n" + inspectorContent)
}

func renderStructuralEntropy(dagRaw map[string]any) string {
	entropyStr := dashNA
	if eRaw := numF64(dagRaw, "entropy_ratio"); eRaw > 0 {
		eStr := fmt.Sprintf("%.2f", eRaw)
		if eRaw > 1.5 {
			entropyStr = errorStyle.Render(eStr + " (Highly Complex)")
		} else if eRaw > 1.0 {
			entropyStr = warningStyle.Render(eStr + " (Elevated)")
		} else {
			entropyStr = successStyle.Render(eStr + " (Optimized)")
		}
	}
	entropyTbl := renderStyledTable([]string{colMetric, colValue}, [][]string{
		{"Graph Entropy Ratio", entropyStr},
		{"Topological Mutation Depth", fmt.Sprint(numI64(dagRaw, "mutation_depth"))},
		{"Total Edges", fmt.Sprint(numI64(dagRaw, "total_edges"))},
		{"Tree Depth", fmt.Sprint(numI64(dagRaw, "tree_depth"))},
	})
	return cardStyle.Render(subTitleStyle.Render("Structural Entropy") + "\n" + entropyTbl)
}

func renderSelfHealing(activeNode map[string]any) string {
	healContent := "No faults detected in active node."
	if faultsRaw := numI64(activeNode, "faults"); faultsRaw > 0 {
		healContent = errorStyle.Render(fmt.Sprintf("Soft-Failures Detected: %d", faultsRaw)) + "\n"
		healContent += fmt.Sprintf("Rollback Strategy: %s\n", str(activeNode, "rollback_strategy"))
		healContent += fmt.Sprintf("Retry Limit: %d/%d\n", numI64(activeNode, "retry_count"), numI64(activeNode, "retry_limit"))
		healContent += warningStyle.Render("Alternative URN Route: ") + str(activeNode, "fallback_urn")
	}
	return cardStyle.Render(subTitleStyle.Render("Self-Healing Trajectory") + "\n" + healContent)
}

func buildOrchestrationTab(snapshot map[string]any) string {
	dagRaw, ok := snapshot["dag_status"].(map[string]any)
	if !ok || len(dagRaw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("DAG Pipeline Status") + "\n" + "Waiting for real-time compose_pipeline DAG data...")
	}

	sessionID := str(dagRaw, "session_id")
	status := str(dagRaw, "status")
	totalNodes := numI64(dagRaw, "total_nodes")
	currentNode := numI64(dagRaw, "current_node_index")
	globalLatency := str(dagRaw, "global_latency")

	statusColor := cyanStyle
	switch status {
	case "EXECUTING":
		statusColor = warningStyle
	case "FAILED":
		statusColor = errorStyle
	case "COMPLETED":
		statusColor = successStyle
	}

	header := statusColor.Render(fmt.Sprintf(" [STATUS: %s] | Session: %s | Nodes: %d | Current: %d | Latency: %s ", status, sessionID, totalNodes, currentNode, globalLatency))

	activeNode := mapFrom(dagRaw["active_node"])

	cascadeBox := renderExecutionCascade(dagRaw)
	entropyBox := renderStructuralEntropy(dagRaw)
	inspectorBox := renderPayloadInspector(activeNode)
	healBox := renderSelfHealing(activeNode)
	optBox := renderPipelineOpt(snapshot)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, cascadeBox, entropyBox)
	midRow := lipgloss.JoinHorizontal(lipgloss.Top, inspectorBox, healBox)

	return header + "\n" + lipgloss.JoinVertical(lipgloss.Left, topRow, midRow, optBox)
}

// ── Tab 6: Storage ──────────────────────────────────────────────────────────

func getHist(dbsHist map[string]any, window string, root string, child string) map[string]any {
	if dbsHist == nil {
		return nil
	}
	winObj := mapFrom(dbsHist[window])
	if winObj == nil {
		return nil
	}
	rootObj := mapFrom(winObj[root])
	if rootObj == nil {
		return nil
	}
	if child != "" {
		childObj := mapFrom(rootObj[child])
		return childObj
	}
	return rootObj
}

func dbDeltaStr(live map[string]any, hist map[string]any, key string) string {
	liveVal := numF64(live, key)
	if hist == nil {
		return "-"
	}
	histVal := numF64(hist, key)
	diff := liveVal - histVal
	if diff > 0 {
		return successStyle.Render(fmt.Sprintf("+%v", diff))
	} else if diff < 0 {
		return errorStyle.Render(fmt.Sprintf("%v", diff))
	}
	return "0"
}

func renderDatabases(snapshot map[string]any) string {
	dbsRaw, ok := snapshot["databases"].(map[string]any)
	if !ok || len(dbsRaw) == 0 {
		return cardStyle.Render(subTitleStyle.Render("Database Diagnostics") + "\n" + "Waiting for database metrics telemetry...")
	}
	dbsHist := mapFrom(snapshot["databases_history"])
	magicRaw, magicOk := dbsRaw["magictools"].(map[string]any)
	recallRaw, recallOk := dbsRaw["recall"].(map[string]any)

	var magicBox, recallBox string
	if magicOk {
		m5 := getHist(dbsHist, "5m", "databases", "magictools")
		m15 := getHist(dbsHist, "15m", "databases", "magictools")
		m60 := getHist(dbsHist, "1h", "databases", "magictools")
		syncStr := successStyle.Render("● HEALTHY")
		if dbsRaw["is_healing"] != nil {
			isH := boolFrom(dbsRaw["is_healing"])
			outSync := boolFrom(dbsRaw["sync_out_of_sync"])
			if isH {
				syncStr = warningStyle.Render("● HEALING")
			} else if outSync {
				syncStr = errorStyle.Render("✖ DESYNC")
			}
		}

		tbl := renderStyledTable([]string{colMetric, "Live", colDelta5m, colDelta15m, colDelta1h}, [][]string{
			{"DB Sync State", syncStr, "-", "-", "-"},
			{"Registry Cache Hits", fmt.Sprint(numF64(magicRaw, "Hits")), dbDeltaStr(magicRaw, m5, "Hits"), dbDeltaStr(magicRaw, m15, "Hits"), dbDeltaStr(magicRaw, m60, "Hits")},
			{"Registry Cache Misses", fmt.Sprint(numF64(magicRaw, "Misses")), dbDeltaStr(magicRaw, m5, "Misses"), dbDeltaStr(magicRaw, m15, "Misses"), dbDeltaStr(magicRaw, m60, "Misses")},
			{"Registry Cached Items", fmt.Sprint(numF64(magicRaw, "Entries")), dbDeltaStr(magicRaw, m5, "Entries"), dbDeltaStr(magicRaw, m15, "Entries"), dbDeltaStr(magicRaw, m60, "Entries")},
			{"BadgerDB Tools Count", fmt.Sprint(numF64(magicRaw, "Tools")), dbDeltaStr(magicRaw, m5, "Tools"), dbDeltaStr(magicRaw, m15, "Tools"), dbDeltaStr(magicRaw, m60, "Tools")},
			{"BadgerDB Intel Count", fmt.Sprint(numF64(magicRaw, "Intel")), dbDeltaStr(magicRaw, m5, "Intel"), dbDeltaStr(magicRaw, m15, "Intel"), dbDeltaStr(magicRaw, m60, "Intel")},
			{"Bleve Search Docs", fmt.Sprint(numF64(magicRaw, "BleveDocs")), dbDeltaStr(magicRaw, m5, "BleveDocs"), dbDeltaStr(magicRaw, m15, "BleveDocs"), dbDeltaStr(magicRaw, m60, "BleveDocs")},
		})
		magicBox = cardStyle.Render(subTitleStyle.Render("Orchestrator DB (MagicTools) 10s refresh") + "\n" + tbl)
	} else {
		magicBox = cardStyle.Render(subTitleStyle.Render("Orchestrator DB (MagicTools)") + "\n" + "Offline")
	}

	if recallOk {
		r5 := getHist(dbsHist, "5m", "databases", "recall")
		r15 := getHist(dbsHist, "15m", "databases", "recall")
		r60 := getHist(dbsHist, "1h", "databases", "recall")

		// Dynamic namespace rows from the nested namespaces map
		var nsRows [][]string
		if nsRaw, ok := recallRaw["namespaces"].(map[string]any); ok && len(nsRaw) > 0 {
			// Extract history namespace sub-maps for deltas
			var ns5, ns15, ns60 map[string]any
			if r5 != nil {
				ns5, _ = mapFromAny(r5["namespaces"])
			}
			if r15 != nil {
				ns15, _ = mapFromAny(r15["namespaces"])
			}
			if r60 != nil {
				ns60, _ = mapFromAny(r60["namespaces"])
			}

			var domains []string
			for d := range nsRaw {
				domains = append(domains, d)
			}
			sort.Strings(domains)

			for _, domain := range domains {
				label := strings.ReplaceAll(domain, "_", " ")
				label = strings.Title(label) //nolint:staticcheck // Title is deprecated but matches legacy dashboard labels
				nsRows = append(nsRows, []string{
					label,
					fmt.Sprint(numF64(nsRaw, domain)),
					dbDeltaStr(nsRaw, ns5, domain),
					dbDeltaStr(nsRaw, ns15, domain),
					dbDeltaStr(nsRaw, ns60, domain),
				})
			}
		}

		// Append DB-level aggregate metrics
		nsRows = append(nsRows,
			[]string{"DB Hits", fmt.Sprint(numF64(recallRaw, "db_hits")), dbDeltaStr(recallRaw, r5, "db_hits"), dbDeltaStr(recallRaw, r15, "db_hits"), dbDeltaStr(recallRaw, r60, "db_hits")},
			[]string{"DB Misses", fmt.Sprint(numF64(recallRaw, "db_misses")), dbDeltaStr(recallRaw, r5, "db_misses"), dbDeltaStr(recallRaw, r15, "db_misses"), dbDeltaStr(recallRaw, r60, "db_misses")},
			[]string{"Bleve Search Docs", fmt.Sprint(numF64(recallRaw, "bleve_docs")), dbDeltaStr(recallRaw, r5, "bleve_docs"), dbDeltaStr(recallRaw, r15, "bleve_docs"), dbDeltaStr(recallRaw, r60, "bleve_docs")},
		)

		tbl := renderStyledTable([]string{colMetric, "Live", colDelta5m, colDelta15m, colDelta1h}, nsRows)
		recallBox = cardStyle.Render(subTitleStyle.Render("Memory DB (Recall) 10s refresh") + "\n" + tbl)
	} else {
		recallBox = cardStyle.Render(subTitleStyle.Render("Memory DB (Recall)") + "\n" + "Offline or Disconnected")
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, recallBox, magicBox)
}
