package telemetry

import (
	"testing"
)

func TestDAGTracker(t *testing.T) {
	tracker := &DAGTracker{}
	tracker.InitializePipeline("session-1", []string{"node1", "node2"}, 0.5, 10, 5)

	// Test depth
	tracker.IncrementMutationDepth()

	// Test active node
	tracker.UpdateActiveNode("node1", 100, 1000, 500, "HIT", "hash")

	// Test completion
	tracker.CompleteNode("node1", true)

	// Test fault
	tracker.RecordFault("node1", "RETRY", 1, 3, "fallback")

	// Test splice
	tracker.SpliceNodes("node1", []string{"node3", "node4"})

	// Test snapshot
	snapshot := tracker.Snapshot()
	if snapshot["session_id"] != "session-1" {
		t.Errorf("expected session-1, got %v", snapshot["session_id"])
	}
	nodes := snapshot["nodes"].([]any)
	if len(nodes) != 4 { // node1, node2 + spliced node3, node4
		t.Errorf("expected 4 nodes, got %d", len(nodes))
	}

	// Test close
	tracker.ClosePipeline("COMPLETED")
}
