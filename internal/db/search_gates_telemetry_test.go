package db

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

func TestRecordFusionWinnerTelemetryPerQuery(t *testing.T) {
	telemetry.SearchMetrics.VectorWins.Store(0)
	telemetry.SearchMetrics.LexicalWins.Store(0)

	vectorScores := map[string]float64{
		"a:tool1": 0.9,
		"a:tool2": 0.3,
	}
	bm25Scores := map[string]float64{
		"a:tool1": 1.0,
		"a:tool2": 10.0,
	}

	trace := &telemetry.SearchQueryTrace{}
	recordFusionWinnerTelemetry(vectorScores, bm25Scores, "a:tool1", 0.55, trace)

	if telemetry.SearchMetrics.VectorWins.Load() != 1 {
		t.Fatalf("expected 1 vector win, got %d", telemetry.SearchMetrics.VectorWins.Load())
	}
	if telemetry.SearchMetrics.LexicalWins.Load() != 0 {
		t.Fatalf("expected 0 lexical wins, got %d", telemetry.SearchMetrics.LexicalWins.Load())
	}
	if trace.FusionWinner != "vector" {
		t.Fatalf("expected fusion winner vector, got %q", trace.FusionWinner)
	}
}

func TestRecordBM25SquashTelemetry(t *testing.T) {
	trace := &telemetry.SearchQueryTrace{}
	recordBM25SquashTelemetry(8.0, trace)
	if trace.BM25SquashDelta <= 0 {
		t.Fatalf("expected positive squash delta, got %f", trace.BM25SquashDelta)
	}
}
