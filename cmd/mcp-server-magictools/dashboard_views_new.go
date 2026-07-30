// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"fmt"
	"sort"

	"github.com/charmbracelet/lipgloss"
)

// ── Tab 5: LLM Backplane ────────────────────────────────────────────────────

func renderLLMOverview(llmRaw map[string]any) string {
	status := str(llmRaw, "backplane_status")
	statusStyled := successStyle.Render("● " + status)
	if status != "ENABLED" {
		statusStyled = errorStyle.Render("● " + status)
	}

	fastModel := str(llmRaw, "fast_model")
	thinkingModel := str(llmRaw, "thinking_model")
	if thinkingModel == "" {
		thinkingModel = grayStyle.Render("(not configured)")
	}
	totalTokens := numI64(llmRaw, "total_tokens_consumed")

	return cardStyle.Render(subTitleStyle.Render("Backplane Overview") + "\n" +
		renderStyledTable([]string{colProperty, colValue}, [][]string{
			{colStatus, statusStyled},
			{"Fast Model", fastModel},
			{"Thinking Model", thinkingModel},
			{"Total Tokens Consumed", fmt.Sprintf("%d", totalTokens)},
		}))
}

func renderLLMServerClients(llmRaw map[string]any) string {
	serverContent := grayStyle.Render("No per-server data available.")
	if perServerRaw, ok := llmRaw["per_server_tokens"].(map[string]any); ok && len(perServerRaw) > 0 {
		var names []string
		for n := range perServerRaw {
			names = append(names, n)
		}
		sort.Strings(names)

		var rows [][]string
		for _, name := range names {
			tokens := numI64(perServerRaw, name)
			statusStr := grayStyle.Render("○ IDLE")
			if tokens > 0 {
				statusStr = successStyle.Render("● ACTIVE")
			}
			rows = append(rows, []string{
				name,
				statusStr,
			})
		}
		serverContent = renderStyledTable(
			[]string{colServer, colStatus},
			rows,
		)
	}
	return cardStyle.Render(subTitleStyle.Render("Active Sub-Server LLM Clients") + "\n" + serverContent)
}

func renderLLMUsageAndEfficiency(snapshot map[string]any, llmRaw map[string]any) string {
	dbsHist := mapFrom(snapshot["databases_history"])
	totalTokens := numI64(llmRaw, "total_tokens_consumed")
	thresh := numI64(llmRaw, "token_spend_thresh")
	if thresh <= 0 {
		thresh = 500000
	}
	t5 := getHist(dbsHist, "5m", "llm_backplane", "")
	t15 := getHist(dbsHist, "15m", "llm_backplane", "")
	t60 := getHist(dbsHist, "1h", "llm_backplane", "")

	var rows [][]string

	if perServerRaw, ok := llmRaw["per_server_tokens"].(map[string]any); ok {
		var names []string
		for n := range perServerRaw {
			names = append(names, n)
		}
		sort.Strings(names)

		for _, name := range names {
			tokens := numI64(perServerRaw, name)
			pct := float64(0)
			if totalTokens > 0 {
				pct = float64(tokens) / float64(totalTokens) * 100
			}
			threshPct := float64(tokens) / float64(thresh) * 100

			delta5 := "0"
			delta15 := "0"
			delta60 := "0"
			if t5 != nil {
				if sHist, ok := t5["per_server_tokens"].(map[string]any); ok {
					delta5 = dbDeltaStr(perServerRaw, sHist, name)
				}
			}
			if t15 != nil {
				if sHist, ok := t15["per_server_tokens"].(map[string]any); ok {
					delta15 = dbDeltaStr(perServerRaw, sHist, name)
				}
			}
			if t60 != nil {
				if sHist, ok := t60["per_server_tokens"].(map[string]any); ok {
					delta60 = dbDeltaStr(perServerRaw, sHist, name)
				}
			}

			rows = append(rows, []string{
				name,
				fmt.Sprintf("%d", tokens),
				fmt.Sprintf("%.1f%%", pct),
				fmt.Sprintf("%.1f%%", threshPct),
				delta5,
				delta15,
				delta60,
			})
		}
	}

	histContent := "Waiting for historical rollups..."
	if len(rows) > 0 {
		histContent = renderStyledTable(
			[]string{colServer, "Tokens", "Share %", "Threshold %", colDelta5m, colDelta15m, colDelta1h},
			rows,
		)
	}

	delta5Tot := dbDeltaStr(llmRaw, t5, "total_tokens_consumed")
	delta15Tot := dbDeltaStr(llmRaw, t15, "total_tokens_consumed")
	delta60Tot := dbDeltaStr(llmRaw, t60, "total_tokens_consumed")

	// Global threshold percent doesn't map directly per server, but it's the global total against the threshold
	globalThreshPct := float64(totalTokens) / float64(thresh) * 100
	totalRows := [][]string{
		{"TOTAL", fmt.Sprintf("%d", totalTokens), "100.0%", fmt.Sprintf("%.1f%%", globalThreshPct), delta5Tot, delta15Tot, delta60Tot},
	}
	totalContent := renderStyledTable(
		[]string{"Global", "Tokens", "Share %", "Threshold %", colDelta5m, colDelta15m, colDelta1h},
		totalRows,
	)

	return cardStyle.Render(subTitleStyle.Render("Token Usage & Efficiency") + "\n" + histContent + "\n\n" + totalContent)
}

func buildLLMTab(snapshot map[string]any) string {
	llmRaw, ok := snapshot["llm_backplane"].(map[string]any)
	if !ok {
		return cardStyle.Render(subTitleStyle.Render("LLM Backplane") + "\n" +
			warningStyle.Render("LLM backplane not enabled or no telemetry received yet."))
	}

	infoBox := renderLLMOverview(llmRaw)
	usageBox := renderLLMUsageAndEfficiency(snapshot, llmRaw)
	serverBox := renderLLMServerClients(llmRaw)

	topRow := lipgloss.JoinHorizontal(lipgloss.Top, infoBox, usageBox)
	return lipgloss.JoinVertical(lipgloss.Left, topRow, serverBox)
}
