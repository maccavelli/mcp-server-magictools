package llm

// PoolMetricsAdapter wraps a Pool to satisfy client.LLMMetricsProvider.
// The concrete Pool.Metrics() returns PoolMetrics; this adapter converts to map[string]any.
type PoolMetricsAdapter struct {
	Pool *Pool
}

// Metrics returns the pool metrics as a generic map[string]any suitable for
// JSON telemetry snapshots. This method satisfies the client.LLMMetricsProvider interface.
func (a *PoolMetricsAdapter) Metrics() map[string]any {
	pm := a.Pool.Metrics()

	// Cap audit entries to latest 3 for snapshot budget
	audit := pm.RecentAudit
	if len(audit) > 3 {
		audit = audit[:3]
	}

	// Convert audit entries to map representation for JSON
	auditMaps := make([]map[string]any, 0, len(audit))
	for _, e := range audit {
		auditMaps = append(auditMaps, map[string]any{
			"server":     e.Server,
			"tier":       e.Tier,
			"model":      e.Model,
			"latency_ms": e.LatencyMs,
			"tokens":     e.TokensUsed,
			keyError:     e.Error,
		})
	}

	return map[string]any{
		"backplane_status":      pm.BackplaneStatus,
		"fast_model":            pm.FastModel,
		"thinking_model":        pm.ThinkingModel,
		"total_tokens_consumed": pm.TotalTokens,
		"token_spend_thresh":    pm.TokenSpendThresh,
		"per_server_tokens":     pm.PerServer,
		"recent_audit":          auditMaps,
	}
}
