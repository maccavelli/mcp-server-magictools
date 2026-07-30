package db

import "math"

// FusionConfig holds the tunable weights applied during score fusion and the
// post-fusion ranking boost (INT-7). These were previously hardcoded literals
// scattered through the search hot path; centralizing them here lets them be
// calibrated via orchestrator config without a recompile. Defaults reproduce
// the original ADR-0016 calibration exactly.
type FusionConfig struct {
	CorroborationBonus     float64 // dual-leg match bonus added in Score
	ReliabilityBoostWeight float64 // multiplier on (ProxyReliability-1.0) post-fusion
	UsageBoostWeight       float64 // multiplier on log1p(UsageCount) post-fusion
	NativeBoost            float64 // additive boost for native tools post-fusion
}

// DefaultFusionConfig returns the calibrated defaults (the former hardcoded values).
func DefaultFusionConfig() FusionConfig {
	return FusionConfig{
		CorroborationBonus:     0.05,
		ReliabilityBoostWeight: 0.15,
		UsageBoostWeight:       0.02,
		NativeBoost:            0.10,
	}
}

// FusionFromConfig maps orchestrator config into store fusion weights, falling
// back to the calibrated default for any non-positive (unset) value — matching
// the SearchGatesFromConfig convention. A weight cannot be tuned to exactly 0
// this way (use the default's intent instead); the guard exists so an absent
// config field never zeroes a boost.
func FusionFromConfig(corroborationBonus, reliabilityBoostWeight, usageBoostWeight, nativeBoost float64) FusionConfig {
	f := DefaultFusionConfig()
	if corroborationBonus > 0 {
		f.CorroborationBonus = corroborationBonus
	}
	if reliabilityBoostWeight > 0 {
		f.ReliabilityBoostWeight = reliabilityBoostWeight
	}
	if usageBoostWeight > 0 {
		f.UsageBoostWeight = usageBoostWeight
	}
	if nativeBoost > 0 {
		f.NativeBoost = nativeBoost
	}
	return f
}

// Score performs Direct Score Fusion (ADR-0016) between a vector cosine similarity score
// and a lexical BM25 score. The BM25 score is normalized using a soft-sign function (Atan).
//
// IDX-3: when a tool matches both legs, its fused score must never fall below the
// best of its individual leg scores (monotonicity — corroboration cannot demote),
// plus corroborationBonus for dual-leg support (INT-7: now configurable). Negative
// cosine is clamped (IDX-11).
func Score(vecScore, bm25Score, alpha, corroborationBonus float64) float64 {
	if vecScore < 0 {
		vecScore = 0
	}
	normalizedBM25 := NormalizeBM25(bm25Score)

	switch {
	case vecScore > 0 && normalizedBM25 == 0:
		return vecScore
	case vecScore == 0 && normalizedBM25 > 0:
		return normalizedBM25
	case vecScore == 0 && normalizedBM25 == 0:
		return 0
	default: // both legs contributed
		blend := alpha*vecScore + (1-alpha)*normalizedBM25
		best := math.Max(vecScore, normalizedBM25)
		return math.Min(1.0, math.Max(blend, best)+corroborationBonus)
	}
}

const rrfK = 60.0

// RRFScore computes reciprocal-rank fusion for shadow telemetry only (never used for routing).
func RRFScore(bleveRank, vectorRank int) float64 {
	score := 0.0
	if bleveRank > 0 {
		score += 1.0 / (rrfK + float64(bleveRank))
	}
	if vectorRank > 0 {
		score += 1.0 / (rrfK + float64(vectorRank))
	}
	return score
}
