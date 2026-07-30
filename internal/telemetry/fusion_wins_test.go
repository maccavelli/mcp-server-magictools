package telemetry

import (
	"testing"
)

func TestAlignCacheHitRatio(t *testing.T) {
	if got := AlignCacheHitRatio(8, 2); got != 0.8 {
		t.Fatalf("expected 0.8, got %v", got)
	}
	if got := AlignCacheHitRatio(0, 0); got != 0 {
		t.Fatalf("expected 0 for empty, got %v", got)
	}
}

func TestAvgSearchLatencyMs(t *testing.T) {
	if got := AvgSearchLatencyMs(250, 10); got != 25 {
		t.Fatalf("expected 25, got %d", got)
	}
	if got := AvgSearchLatencyMs(100, 0); got != 0 {
		t.Fatalf("expected 0, got %d", got)
	}
}

func TestRecordFusionWinnerVectorOnly(t *testing.T) {
	SearchMetrics.VectorWins.Store(0)
	SearchMetrics.LexicalWins.Store(0)

	RecordFusionWinner(
		map[string]float64{"a:tool1": 0.9},
		map[string]float64{"a:tool1": 0},
		"a:tool1",
		0.55,
		nil,
		func(raw float64) float64 {
			if raw <= 0 {
				return 0
			}
			return raw / 10
		},
	)
	if SearchMetrics.VectorWins.Load() != 1 {
		t.Fatalf("expected vector win, got %d", SearchMetrics.VectorWins.Load())
	}
}

func TestRecordFusionWinnerLexicalOnly(t *testing.T) {
	SearchMetrics.VectorWins.Store(0)
	SearchMetrics.LexicalWins.Store(0)

	RecordFusionWinner(
		map[string]float64{"a:tool1": 0},
		map[string]float64{"a:tool1": 8.0},
		"a:tool1",
		0.55,
		nil,
		func(raw float64) float64 {
			if raw <= 0 {
				return 0
			}
			return raw / 10
		},
	)
	if SearchMetrics.LexicalWins.Load() != 1 {
		t.Fatalf("expected lexical win, got %d", SearchMetrics.LexicalWins.Load())
	}
}

func TestRecordFusionWinnerHybridVector(t *testing.T) {
	SearchMetrics.VectorWins.Store(0)
	SearchMetrics.LexicalWins.Store(0)

	RecordFusionWinner(
		map[string]float64{"a:tool1": 0.95},
		map[string]float64{"a:tool1": 1.0},
		"a:tool1",
		0.55,
		nil,
		func(raw float64) float64 {
			if raw <= 0 {
				return 0
			}
			return raw / 10
		},
	)
	if SearchMetrics.VectorWins.Load() != 1 {
		t.Fatalf("expected vector win, got %d", SearchMetrics.VectorWins.Load())
	}
}

func TestRecordFusionWinnerHybridLexicalTie(t *testing.T) {
	SearchMetrics.VectorWins.Store(0)
	SearchMetrics.LexicalWins.Store(0)

	norm := func(raw float64) float64 {
		if raw <= 0 {
			return 0
		}
		return raw / 10
	}
	// Equal contributions at alpha=0.5 → lexical wins on tie
	RecordFusionWinner(
		map[string]float64{"a:tool1": 0.5},
		map[string]float64{"a:tool1": 5.0},
		"a:tool1",
		0.5,
		nil,
		norm,
	)
	if SearchMetrics.LexicalWins.Load() != 1 {
		t.Fatalf("expected lexical win on tie, got vector=%d lexical=%d",
			SearchMetrics.VectorWins.Load(), SearchMetrics.LexicalWins.Load())
	}
}
