package intelligence

import (
	"context"
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

type mockRecallMinerRecallTest struct {
	sessionData map[string]any
	listRaw     string
}

func (m *mockRecallMinerRecallTest) RecallEnabled() bool { return true }
func (m *mockRecallMinerRecallTest) AggregateSessionFromRecall(ctx context.Context, serverID, projectID string) (map[string]any, error) {
	return m.sessionData, nil
}
func (m *mockRecallMinerRecallTest) ListSessionsByFilter(ctx context.Context, projectID, serverID, outcome string, limit int) string {
	return m.listRaw
}

func TestRecallHelpers_ParseRecallEntries(t *testing.T) {
	if parseRecallEntries(map[string]any{}) != nil {
		t.Error("expected nil")
	}

	res := parseRecallEntries(map[string]any{
		"entries": []any{"a"},
	})
	if res == nil {
		t.Error("expected entries")
	}

	res = parseRecallEntries(map[string]any{
		"stages": []any{"b"},
	})
	if res == nil {
		t.Error("expected stages")
	}
}

func TestRecallHelpers_ExtractDAGFromEntries(t *testing.T) {
	entries := []any{
		map[string]any{
			"tags": []any{"trace:foo"},
		},
		map[string]any{
			"tags":    []any{"trace:execute_pipeline"},
			"content": `{"stage":"execute_pipeline","intent":"test intent"}`,
		},
		map[string]any{
			"tags": []any{"trace:bar", "outcome:error"},
		},
	}

	// Has failure
	_, _, ok := extractDAGFromEntries(entries)
	if ok {
		t.Error("expected false due to failure outcome")
	}

	// No failure
	entries2 := []any{
		map[string]any{
			"tags": []any{"trace:foo"},
		},
		map[string]any{
			"tags":    []any{"trace:execute_pipeline"},
			"content": `{"stage":"execute_pipeline","intent":"test intent"}`,
		},
	}
	urns, intent, ok := extractDAGFromEntries(entries2)
	if !ok || len(urns) != 1 || intent != "test intent" {
		t.Errorf("extractDAG failed: %v %v %v", ok, urns, intent)
	}
}

func TestRecallHelpers_ParseRecallSessionEnvelope(t *testing.T) {
	env1 := `{"entries":["a"]}`
	res1 := parseRecallSessionEnvelope(env1)
	if len(res1) == 0 {
		t.Error("failed env1")
	}

	env2 := `{"data":{"entries":["b"]}}`
	res2 := parseRecallSessionEnvelope(env2)
	if len(res2) == 0 {
		t.Error("failed env2")
	}

	res3 := parseRecallSessionEnvelope(`invalid`)
	if res3 != nil {
		t.Error("expected nil")
	}
}

func TestRecallHelpers_ApplyRecallCalibration(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "recall-test-*")
	defer os.RemoveAll(tmpDir)
	store, _ := db.NewStore(tmpDir)
	defer store.Close()

	intel := &db.ToolIntelligence{}
	intel.Metrics.ProxyReliability = 1.0
	store.SaveIntelligence("test:urn", intel)

	stats := map[string]*recallToolStats{
		"test:urn":  {total: 10, success: 8}, // 80% success
		"test:skip": {total: 1, success: 1},  // <3 total
	}

	calibrated := applyRecallCalibration(store, stats)
	if calibrated != 1 {
		t.Errorf("expected 1, got %d", calibrated)
	}

	intelAfter, _ := store.GetIntelligence("test:urn")
	// 1.0*0.6 + 0.8*0.4 = 0.6 + 0.32 = 0.92
	if intelAfter.Metrics.ProxyReliability != 0.92 {
		t.Errorf("expected 0.92, got %v", intelAfter.Metrics.ProxyReliability)
	}
}

func TestRecallHelpers_MineServerRecallPatterns(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "mine-test-*")
	defer os.RemoveAll(tmpDir)
	store, _ := db.NewStore(tmpDir)
	defer store.Close()

	miner := &mockRecallMinerRecallTest{
		sessionData: map[string]any{
			"entries": []any{
				map[string]any{"tags": []any{"trace:stage1"}},
				map[string]any{"tags": []any{"trace:stage2"}},
				map[string]any{
					"tags":    []any{"trace:execute_pipeline"},
					"content": `{"intent":"mine intent"}`,
				},
			},
		},
	}

	mined := mineServerRecallPatterns(context.Background(), miner, store, "test_server")
	if mined != 1 {
		t.Errorf("expected 1, got %d", mined)
	}
}
