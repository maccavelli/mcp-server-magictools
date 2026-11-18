package handler

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// TestTopologicalSortCycleWarning builds a two-tool dependency cycle (each
// tool triggers the other) and asserts the exact warning text. The tool that
// closes the cycle is the second one in phase order, so the warning names it.
func TestTopologicalSortCycleWarning(t *testing.T) {
	tests := []struct {
		name string
		last string // the tool name the warning is expected to name
		want string
	}{
		{
			name: "ordinary name keeps its backquote delimiters",
			last: "srv:beta",
			want: "⚠️ Semantic Gatekeeper Warning: Circular Dependency Detected connecting `srv:beta` natively.",
		},
		{
			// A name containing a backquote cannot be shown raw inside backquotes,
			// so %#q falls back to the double-quoted escaped form.
			name: "name containing a backquote is double-quoted",
			last: "srv:be`ta",
			want: "⚠️ Semantic Gatekeeper Warning: Circular Dependency Detected connecting \"srv:be`ta\" natively.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			const first = "srv:alpha"
			stages := []PipelineStep{
				{ToolName: first, Role: "ANALYZER", Phase: 1},
				{ToolName: tc.last, Role: "ANALYZER", Phase: 1},
			}
			tools := []*db.ToolRecord{
				{URN: first, Triggers: []string{tc.last}},
				{URN: tc.last, Triggers: []string{first}},
			}

			sorted, warnings := topologicalSort(stages, tools)

			if len(sorted) != 2 {
				t.Fatalf("expected both stages to be kept, got %d", len(sorted))
			}
			if len(warnings) != 1 {
				t.Fatalf("expected exactly one warning, got %d: %q", len(warnings), warnings)
			}
			if warnings[0] != tc.want {
				t.Errorf("warning text mismatch\n got: %s\nwant: %s", warnings[0], tc.want)
			}
		})
	}
}
