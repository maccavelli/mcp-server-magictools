package telemetry

import (
	"testing"
)

func TestSpans(t *testing.T) {
	RecordActiveDispatch("server1", "parent1")

	parent := GetActiveCascadeParent()
	if parent != "parent1" {
		t.Errorf("expected parent1, got %s", parent)
	}

	source := GetActiveCascadeSource()
	if source != "server1" {
		t.Errorf("expected server1, got %s", source)
	}

	ClearActiveDispatch("server1")
	parent = GetActiveCascadeParent()
	if parent != "" {
		t.Errorf("expected empty parent, got %s", parent)
	}
}
