package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func listToolsInfoDefaultInventory() (*mcp.CallToolResult, error) {
	var internals []db.ToolRecord
	if err := json.Unmarshal(InternalToolsInventoryJSON, &internals); err != nil {
		return nil, fmt.Errorf("failed to unmarshal internal inventory: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("# Available MCP Sub-Server Tools\n\n## magictools\n\n")
	sort.Slice(internals, func(i, j int) bool { return internals[i].Name < internals[j].Name })
	for _, t := range internals {
		toolName := t.Name
		if !strings.HasPrefix(toolName, "magictools:") {
			toolName = "magictools:" + toolName
		}
		fmt.Fprintf(&sb, "### `%s`\n", toolName)
		if t.Description != "" {
			fmt.Fprintf(&sb, "%s\n\n", t.Description)
		}
		if len(t.InputSchema) > 0 {
			schemaBytes := marshalIndentOrEmpty(t.InputSchema)
			fmt.Fprintf(&sb, "```json\n%s\n```\n\n", string(schemaBytes))
		}
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: sb.String()}}}, nil
}

func (h *OrchestratorHandler) collectListToolsInfoRecords(
	serverFilter string,
	maxTools int,
) (map[string][]*db.ToolRecord, int) {
	serverTools := make(map[string][]*db.ToolRecord)
	count := 0
	var serversToScan []string
	if serverFilter != "" {
		serversToScan = append(serversToScan, serverFilter)
	} else {
		for _, sc := range h.Config.GetManagedServers() {
			serversToScan = append(serversToScan, sc.Name)
		}
	}
	for _, srv := range serversToScan {
		tools, bErr := h.Store.GetServerToolsNatively(srv, maxTools)
		if bErr != nil {
			slog.Warn("diagnostic_handlers: badgerdb server mapping error", keyError, bErr)
			continue
		}
		for _, r := range tools {
			if r.Server != serverMagictools || strings.EqualFold(serverFilter, serverMagictools) {
				serverTools[r.Server] = append(serverTools[r.Server], r)
				count++
			}
			if count >= maxTools {
				break
			}
		}
		if count >= maxTools {
			break
		}
	}
	return serverTools, count
}

func formatListToolsInfoMarkdown(serverTools map[string][]*db.ToolRecord) string {
	var servers []string
	for s := range serverTools {
		servers = append(servers, s)
	}
	sort.Strings(servers)
	var sb strings.Builder
	sb.WriteString("# Available MCP Sub-Server Tools\n\n")
	for _, srv := range servers {
		tools := serverTools[srv]
		sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
		fmt.Fprintf(&sb, "## %s\n\n", srv)
		isSummarized := len(tools) > 50
		for _, t := range tools {
			toolName := t.Name
			if !strings.HasPrefix(toolName, srv+":") {
				toolName = srv + ":" + toolName
			}
			fmt.Fprintf(&sb, "### `%s`\n", toolName)
			if isSummarized {
				summary := t.Description
				if summary == "" {
					summary = "No description available."
				}
				if t.LiteSummary != "" {
					summary = t.LiteSummary
				} else if len(summary) > 200 {
					summary = summary[:197] + "..."
				}
				fmt.Fprintf(&sb, "%s\n\n", summary)
				continue
			}
			if t.Description != "" {
				fmt.Fprintf(&sb, "%s\n\n", t.Description)
			}
			if t.LiteSummary != "" {
				fmt.Fprintf(&sb, "> Usage Hint: %s\n\n", t.LiteSummary)
			}
			if len(t.InputSchema) > 0 {
				schemaBytes := marshalIndentOrEmpty(t.InputSchema)
				fmt.Fprintf(&sb, "```json\n%s\n```\n\n", string(schemaBytes))
			}
		}
	}
	return sb.String()
}
