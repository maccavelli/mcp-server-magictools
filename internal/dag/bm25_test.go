package dag

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestBM25Scorer(t *testing.T) {
	tools := []*db.ToolRecord{
		{
			URN:         "server1:tool1",
			Name:        "search web",
			Description: "[ROLE: ANALYZER] Search the internet for information.",
		},
		{
			URN:         "server2:tool2",
			Name:        "write file",
			Description: "Write content to a local file system.",
		},
	}

	scorer := NewBM25Scorer(tools)
	scores := scorer.Score("search internet")

	if scores["server1:tool1"] <= scores["server2:tool2"] {
		t.Errorf("expected server1:tool1 to rank higher, got scores: %+v", scores)
	}

	// Test normalization
	for _, s := range scores {
		if s < 0 || s > 1 {
			t.Errorf("score not normalized: %f", s)
		}
	}
}
