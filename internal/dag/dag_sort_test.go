package dag

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestTopologicalSort(t *testing.T) {
	tools := []*db.ToolRecord{
		{URN: "A", DependsOn: []string{}},
		{URN: "B", DependsOn: []string{"A"}},
		{URN: "C", DependsOn: []string{"A", "B"}},
	}

	sorted, err := TopologicalSort(tools)
	if err != nil {
		t.Fatalf("sort failed: %v", err)
	}

	if len(sorted) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(sorted))
	}

	if sorted[0].URN != "A" || sorted[1].URN != "B" || sorted[2].URN != "C" {
		t.Errorf("incorrect sort order: %v %v %v", sorted[0].URN, sorted[1].URN, sorted[2].URN)
	}

	// Test cycle
	cycleTools := []*db.ToolRecord{
		{URN: "X", DependsOn: []string{"Y"}},
		{URN: "Y", DependsOn: []string{"X"}},
	}
	_, err = TopologicalSort(cycleTools)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}
