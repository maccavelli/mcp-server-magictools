package telemetry

import (
	"testing"
)

func TestCollisionTracker(t *testing.T) {
	tracker := NewCollisionTracker(10)

	event := CollisionEvent{
		Query:     "test query",
		S1URN:     "server1:tool1",
		S1Score:   0.9,
		S2URN:     "server2:tool2",
		S2Score:   0.85,
		Gap:       0.05,
		Collision: true,
	}

	tracker.Record(event)
	tracker.Record(event)

	snapshot := tracker.Snapshot()
	if len(snapshot.TopPairs) == 0 {
		t.Fatal("expected top pairs")
	}

	pair := snapshot.TopPairs[0]
	if pair.Count != 2 {
		t.Errorf("expected count 2, got %d", pair.Count)
	}
}
