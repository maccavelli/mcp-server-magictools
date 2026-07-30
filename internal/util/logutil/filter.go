package logutil

import (
	"strings"
)

// FilterLogs applies common MCP/Orchestrator log filters to a slice of log lines.
func FilterLogs(lines []string, serverID, severity string) []string {
	if len(lines) == 0 {
		return nil
	}

	severity = strings.ToUpper(severity)
	serverID = strings.ToLower(serverID)

	var filtered []string
	for _, line := range lines {
		if line == "" {
			continue
		}
		match := true
		lineLower := strings.ToLower(line)

		// Server filter
		if serverID != "" {
			if !strings.Contains(lineLower, "server="+serverID) &&
				!strings.Contains(lineLower, "name="+serverID) &&
				!strings.Contains(lineLower, "\"server\":\""+serverID+"\"") &&
				!strings.Contains(lineLower, "\"name\":\""+serverID+"\"") {
				match = false
			}
		}

		// Severity filter
		if match && severity != "" {
			switch severity {
			case "ERROR":
				if !strings.Contains(line, "level=ERROR") && !strings.Contains(line, "\"level\":\"ERROR\"") {
					match = false
				}
			case "WARNING":
				if !strings.Contains(line, "level=WARN") &&
					!strings.Contains(line, "level=WARNING") &&
					!strings.Contains(line, "\"level\":\"WARN\"") &&
					!strings.Contains(line, "\"level\":\"WARNING\"") {
					match = false
				}
			case "CRITICAL":
				if !strings.Contains(line, "CRITICAL") &&
					!strings.Contains(line, "level=DPANIC") &&
					!strings.Contains(line, "level=PANIC") &&
					!strings.Contains(line, "level=FATAL") {
					match = false
				}
			}
		}

		if match {
			filtered = append(filtered, line)
		}
	}

	return filtered
}
