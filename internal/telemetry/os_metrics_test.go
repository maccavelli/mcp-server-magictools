package telemetry

import (
	"testing"
)

func TestGetSystemProcessStats(t *testing.T) {
	stats := GetSystemProcessStats()
	if stats == nil {
		t.Fatal("expected stats to not be nil")
	}
	if stats.PID <= 0 {
		t.Errorf("expected valid PID, got %d", stats.PID)
	}
	if stats.UptimeString == "" {
		t.Errorf("expected uptime string, got empty")
	}
	if stats.Goroutines <= 0 {
		t.Errorf("expected goroutines count > 0, got %d", stats.Goroutines)
	}
}
