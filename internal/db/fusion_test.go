package db

import (
	"math"
	"testing"
)

// cb is the default corroboration bonus, threaded into Score (INT-7).
var cb = DefaultFusionConfig().CorroborationBonus

func TestScoreFusion(t *testing.T) {
	t.Parallel()
	if got := Score(0.9, 0, 0.6, cb); got != 0.9 {
		t.Fatalf("vector-only: got %v want 0.9", got)
	}
	if got := Score(0, 10, 0.6, cb); got <= 0 {
		t.Fatalf("lexical-only: got %v want > 0", got)
	}
	// IDX-3: a both-leg match must NOT be demoted below its stronger leg (the old
	// "between the legs" average was the corroboration-penalty bug).
	blended := Score(0.8, 10, 0.6, cb)
	if blended < 0.8 || blended > 1 {
		t.Fatalf("blended (both legs) should be >= stronger leg and <= 1: got %v", blended)
	}
}

// TestScore_Monotonicity is the IDX-3 regression: when both legs contribute, the
// fused score must never fall below the best single leg, and must stay in [0,1].
func TestScore_Monotonicity(t *testing.T) {
	for _, a := range []float64{0.3, 0.5, 0.7} {
		for _, vec := range []float64{0.1, 0.5, 0.85, 0.99} {
			for _, bm := range []float64{0.5, 2, 5, 20} {
				n := NormalizeBM25(bm)
				s := Score(vec, bm, a, cb)
				if best := math.Max(vec, n); s < best-1e-9 {
					t.Errorf("Score(%v,%v,a=%v)=%v < best leg %v (corroboration demoted)", vec, bm, a, s, best)
				}
				if s > 1.0+1e-9 {
					t.Errorf("Score(%v,%v,a=%v)=%v exceeds 1.0", vec, bm, a, s)
				}
			}
		}
	}
}

// TestScore_CorroborationBeatsWeakerSingleLeg: a both-leg match should outrank a
// slightly-stronger vector-only match (the original bug had it lose).
func TestScore_CorroborationBeatsWeakerSingleLeg(t *testing.T) {
	singleLeg := Score(0.9, 0, 0.5, cb)  // vector-only 0.9
	bothLeg := Score(0.85, 5.0, 0.5, cb) // vec 0.85 + a real BM25 signal
	if bothLeg < singleLeg {
		t.Errorf("both-leg (%v) should not rank below weaker single-leg (%v)", bothLeg, singleLeg)
	}
}

func TestScore_NegativeClampAndZero(t *testing.T) {
	if got := Score(0, 0, 0.5, cb); got != 0 {
		t.Errorf("both-zero: got %v want 0", got)
	}
	// Negative cosine is clamped, so it doesn't pollute fusion.
	if got, want := Score(-0.5, 5.0, 0.5, cb), NormalizeBM25(5.0); math.Abs(got-want) > 1e-9 {
		t.Errorf("negative vec clamp: got %v want %v (bm25-only)", got, want)
	}
}

// TestFusionFromConfig_DefaultsAndOverrides is the INT-7 regression: non-positive
// (unset) weights fall back to the calibrated defaults; positive values override.
func TestFusionFromConfig_DefaultsAndOverrides(t *testing.T) {
	t.Parallel()
	def := DefaultFusionConfig()

	// All unset → identical to defaults (no boost is silently zeroed).
	got := FusionFromConfig(0, 0, -1, 0)
	if got != def {
		t.Fatalf("unset weights should equal defaults: got %+v want %+v", got, def)
	}

	// Positive values override; the rest stay at default.
	got = FusionFromConfig(0.2, 0, 0, 0)
	if got.CorroborationBonus != 0.2 {
		t.Errorf("CorroborationBonus override: got %v want 0.2", got.CorroborationBonus)
	}
	if got.ReliabilityBoostWeight != def.ReliabilityBoostWeight ||
		got.UsageBoostWeight != def.UsageBoostWeight ||
		got.NativeBoost != def.NativeBoost {
		t.Errorf("unset weights should retain defaults: got %+v", got)
	}
}

// TestScore_CorroborationBonusConfigurable is the INT-7 regression: a larger
// corroboration bonus raises a dual-leg fused score, still capped at 1.0.
func TestScore_CorroborationBonusConfigurable(t *testing.T) {
	t.Parallel()
	low := Score(0.5, 5.0, 0.5, 0.01)
	high := Score(0.5, 5.0, 0.5, 0.20)
	if high <= low {
		t.Errorf("larger corroboration bonus should raise dual-leg score: low=%v high=%v", low, high)
	}
	if capped := Score(0.99, 50.0, 0.5, 0.9); capped > 1.0+1e-9 {
		t.Errorf("fused score must stay <= 1.0 even with a large bonus: got %v", capped)
	}
}

func TestRRFScore(t *testing.T) {
	t.Parallel()
	if RRFScore(0, 0) != 0 {
		t.Fatal("empty ranks should score 0")
	}
	s1 := RRFScore(1, 1)
	s2 := RRFScore(5, 5)
	if s1 <= s2 {
		t.Fatalf("rank 1 should beat rank 5: s1=%v s2=%v", s1, s2)
	}
}

func TestNormalizeBM25(t *testing.T) {
	t.Parallel()
	if NormalizeBM25(0) != 0 {
		t.Fatal("zero raw should normalize to 0")
	}
	if NormalizeBM25(5) <= NormalizeBM25(1) {
		t.Fatal("higher raw BM25 should normalize higher")
	}
}

func TestNormalizeBM25PureBranch(t *testing.T) {
	t.Parallel()
	raw := 15.0
	norm := NormalizeBM25(raw)
	if norm <= 0 || norm > 1 {
		t.Fatalf("normalized BM25 should be in (0,1], got %v", norm)
	}
	if norm == raw {
		t.Fatal("pure BM25 branch must not use raw score as fused score")
	}
}

func TestApplyStrictGates(t *testing.T) {
	t.Parallel()
	gates := SearchGatesFromConfig(true, 0.72, 0.15, true)

	bm25 := map[string]float64{"a": 0.01}
	vector := map[string]float64{"b": 0.5}
	lexDrop, vecDrop, rejected := applyStrictGates(gates, bm25, vector, true)
	if !lexDrop || !vecDrop || !rejected {
		t.Fatalf("expected both legs dropped and rejection, got lex=%v vec=%v rej=%v", lexDrop, vecDrop, rejected)
	}

	bm25 = map[string]float64{"a": 20}
	vector = map[string]float64{"b": 0.95}
	_, _, rejected = applyStrictGates(gates, bm25, vector, true)
	if rejected {
		t.Fatal("strong legs should pass gates")
	}
}
