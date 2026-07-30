package main

import (
	"testing"
)

func TestDashboardViews(t *testing.T) {
	snapshot := make(map[string]any)
	logs := []string{"test log"}

	// Test all tabs with empty snapshot
	content := buildSummaryTab(snapshot, logs)
	if len(content) == 0 {
		t.Error("buildSummaryTab failed")
	}

	content = buildToolIntelligenceTab(snapshot)
	if len(content) == 0 {
		t.Error("buildToolIntelligenceTab failed")
	}

	content = buildToolAnalyticsTab(snapshot)
	if len(content) == 0 {
		t.Error("buildToolAnalyticsTab failed")
	}

	res := buildSystemBackplaneTab(snapshot)
	if len(res) == 0 {
		t.Error("buildSystemBackplaneTab failed")
	}

	res = buildStorageTab(snapshot)
	if len(res) == 0 {
		t.Error("buildStorageTab failed")
	}

	res = buildLoggingTab(snapshot)
	if len(res) == 0 {
		t.Error("buildLoggingTab failed")
	}

	res = buildTracingTab(snapshot)
	if len(res) == 0 {
		t.Error("buildTracingTab failed")
	}

	res = buildOrchestrationTab(snapshot)
	if len(res) == 0 {
		t.Error("buildOrchestrationTab failed")
	}

	res = buildLLMTab(snapshot)
	if len(res) == 0 {
		t.Error("buildLLMTab failed")
	}
}
