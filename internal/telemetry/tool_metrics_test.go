package telemetry

import (
	"testing"
)

func TestToolMetrics(t *testing.T) {
	tracker := &ToolTracker{}
	tracker.Record("server1:tool1", 100, false)
	tracker.Record("server1:tool1", 200, true)
	tracker.Record("server2:tool2", 50, false)

	all := tracker.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 tool metrics, got %d", len(all))
	}

	m1 := all["server1:tool1"]
	if m1.Calls != 2 || m1.Faults != 1 || m1.TotalMs != 300 {
		t.Errorf("unexpected metrics for tool1: %+v", m1)
	}
}
