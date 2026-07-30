package intelligence

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

type mockRecallMiner struct{}

func (m *mockRecallMiner) RecallEnabled() bool { return true }
func (m *mockRecallMiner) AggregateSessionFromRecall(ctx context.Context, serverID, projectID string) (map[string]any, error) {
	return map[string]any{
		"entries": []any{
			map[string]any{
				"record": map[string]any{
					"content": "{\"stage\":\"execute_pipeline\",\"intent\":\"fix things\"}",
					"tags":    []any{"trace:tool1", "outcome:error"},
				},
			},
			map[string]any{
				"record": map[string]any{
					"content": "{\"stage\":\"tool1\"}",
					"tags":    []any{"trace:tool1", "outcome:completed"},
				},
			},
		},
	}, nil
}
func (m *mockRecallMiner) ListSessionsByFilter(ctx context.Context, projectID, serverID, outcome string, limit int) string {
	return "{\"sessions\":[{\"session_id\":\"s1\",\"intent\":\"test\",\"target\":\"test\"}]}"
}

func TestHydratorExtra(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "hydrator-db-*")
	defer os.RemoveAll(tempDir)

	store, _ := db.NewStore(tempDir)
	defer store.Close()

	miner := &mockRecallMiner{}

	CalibrateFromRecall(context.Background(), miner, store)
	MineRecallPatterns(context.Background(), miner, store)
}

func TestRunSweepExtra(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "hydrator-sweep-*")
	defer os.RemoveAll(tempDir)
	store, _ := db.NewStore(tempDir)
	defer store.Close()

	cfg := &config.Config{}
	cfg.Intelligence.Provider = "test"
	cfg.Intelligence.APIKey = "test"
	RunSweep(context.Background(), store, cfg, nil)
}

func TestHydrateVectorGraphExtra(t *testing.T) {
	hydrateVectorGraph(context.Background(), nil, nil, []*db.ToolRecord{})
}

func TestRunSweep_NilStoreOrCfg(t *testing.T) {
	if RunSweep(context.Background(), nil, nil, nil) {
		t.Error("expected false")
	}
	cfg := &config.Config{}
	if RunSweep(context.Background(), nil, cfg, nil) {
		t.Error("expected false")
	}
}

func TestRunSweep_NoLLM(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "hydrator-sweep-*")
	defer os.RemoveAll(tempDir)
	store, _ := db.NewStore(tempDir)
	defer store.Close()

	cfg := &config.Config{} // no provider means no LLM
	if RunSweep(context.Background(), store, cfg, nil) {
		t.Error("expected false")
	}
}
