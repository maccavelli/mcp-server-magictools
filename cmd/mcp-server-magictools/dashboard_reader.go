// Package cmd provides functionality for the cmd subsystem.
package main

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

// ToolScoreCard holds the pre-computed score data for a single tool URN.
type ToolScoreCard struct {
	URN         string
	Reliability float64
	Baseline    float64
	Deviation   float64
	Delta30m    float64
	Delta4h     float64
	DeltaAll    float64
}

// ReadDashboardSnapshot reads recent system dashboard state.
func ReadDashboardSnapshot() (map[string]any, []string, error) {
	ringPath := filepath.Join(config.DefaultCacheDir(), "telemetry.ring")
	gaugeBytes, logBytes, err := telemetry.ReadState(ringPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read state failed: %w", err)
	}

	var snapshot map[string]any
	if len(gaugeBytes) > 0 {
		if err := json.Unmarshal(gaugeBytes, &snapshot); err != nil {
			slog.Debug("dashboard: failed to unmarshal gauge snapshot", "error", err, "bytes", len(gaugeBytes))
		}
	}
	if snapshot == nil {
		snapshot = make(map[string]any)
	}

	// Parse raw JSON "scores" into ToolScoreCard structs
	if scoresRaw, ok := snapshot["scores"].(map[string]any); ok {
		parsedScores := make(map[string]ToolScoreCard)
		for urn, rawCard := range scoresRaw {
			if c, ok := rawCard.(map[string]any); ok {
				parsedScores[urn] = ToolScoreCard{
					URN:         urn,
					Reliability: jsonFloat64(c, "Reliability"),
					Baseline:    jsonFloat64(c, "Baseline"),
					Deviation:   jsonFloat64(c, "Deviation"),
					Delta30m:    jsonFloat64(c, "Delta30m"),
					Delta4h:     jsonFloat64(c, "Delta4h"),
					DeltaAll:    jsonFloat64(c, "DeltaAll"),
				}
			}
		}
		snapshot["scores"] = parsedScores
	}

	var logs []string
	lines := strings.SplitSeq(string(logBytes), "\n")
	for l := range lines {
		// Strip null bytes natively
		l = strings.ReplaceAll(l, "\x00", "")
		l = strings.TrimSpace(l)
		if l != "" {
			logs = append(logs, l)
		}
	}
	if len(logs) > 500 {
		logs = logs[len(logs)-500:]
	}

	return snapshot, logs, nil
}

// jsonFloat64 extracts a float64 from a JSON-decoded map (handles float64 and int from json.Unmarshal).
func jsonFloat64(m map[string]any, key string) float64 {
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
