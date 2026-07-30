package intelligence

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

type MockRecallMiner struct {
	SessionData map[string]any
	ListRaw     string
}

func (m *MockRecallMiner) RecallEnabled() bool { return true }
func (m *MockRecallMiner) AggregateSessionFromRecall(ctx context.Context, serverID, projectID string) (map[string]any, error) {
	if m.SessionData != nil {
		return m.SessionData, nil
	}
	return map[string]any{
		"entries": []any{
			map[string]any{
				"content": `{"stage": "execute_pipeline", "intent": "test intent"}`,
			},
			map[string]any{
				"tags": []any{"trace:tool_1"},
			},
			map[string]any{
				"tags": []any{"trace:tool_2"},
			},
		},
	}, nil
}
func (m *MockRecallMiner) ListSessionsByFilter(ctx context.Context, projectID, serverID, outcome string, limit int) string {
	if m.ListRaw != "" {
		return m.ListRaw
	}
	return `{"entries": [{"record": {"tags": ["trace:tool_1", "outcome:success"]}}, {"record": {"tags": ["trace:tool_2", "outcome:error"]}}]}`
}

func TestRecallMinersExtra(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "intel-miner-*")
	defer os.RemoveAll(tmp)
	store, _ := db.NewStore(tmp)
	defer store.Close()

	mock := &MockRecallMiner{}

	// Mine patterns
	MineRecallPatterns(context.Background(), mock, store)

	// Calibrate
	// We need to add intelligence records first so they can be calibrated
	store.SaveIntelligence("brainstorm:tool_1", &db.ToolIntelligence{
		Metrics: db.ToolMetrics{ProxyReliability: 1.0},
	})
	store.SaveIntelligence("brainstorm:tool_2", &db.ToolIntelligence{
		Metrics: db.ToolMetrics{ProxyReliability: 1.0},
	})

	CalibrateFromRecall(context.Background(), mock, store)
}
