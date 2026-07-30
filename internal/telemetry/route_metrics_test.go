package telemetry

import (
	"testing"
)

func TestRouteMetrics(t *testing.T) {
	tracker := &RouteTracker{}
	tracker.RecordRoute("client", "align_tools", false)
	tracker.RecordRoute("client", "align_tools", false)
	tracker.RecordRoute("brainstorm", "call_proxy", true)

	snapshot := tracker.Snapshot()
	if len(snapshot) != 2 {
		t.Errorf("expected 2 routes, got %d", len(snapshot))
	}

	found := false
	for _, r := range snapshot {
		if r["source"] == "client" && r["target"] == "align_tools" {
			if r["calls"] != int64(2) {
				t.Errorf("expected count 2 for client->align_tools, got %v", r["calls"])
			}
			found = true
		}
	}
	if !found {
		t.Errorf("client->align_tools not found in snapshot")
	}
}
