package db

import (
	"errors"
	"math"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

// ErrGatedNoMatch indicates strict per-leg gates rejected every retrieval leg.
var ErrGatedNoMatch = errors.New("no local tool match: search gates rejected all legs")

// SearchGateConfig controls per-leg confidence floors and fallback suppression.
type SearchGateConfig struct {
	StrictGates           bool
	VectorMinCosine       float64
	BM25MinNormalized     float64
	DisableSearchFallback bool
}

// DefaultSearchGates returns conservative defaults (gates off) for backward-compatible tests.
func DefaultSearchGates() SearchGateConfig {
	return SearchGateConfig{
		StrictGates:           false,
		VectorMinCosine:       0.72,
		BM25MinNormalized:     0.15,
		DisableSearchFallback: true,
	}
}

// SearchGatesFromConfig maps orchestrator config into store gate settings.
func SearchGatesFromConfig(strictGates bool, vectorMinCosine, bm25MinNormalized float64, disableFallback bool) SearchGateConfig {
	g := DefaultSearchGates()
	g.StrictGates = strictGates
	if vectorMinCosine > 0 {
		g.VectorMinCosine = vectorMinCosine
	}
	if bm25MinNormalized > 0 {
		g.BM25MinNormalized = bm25MinNormalized
	}
	g.DisableSearchFallback = disableFallback
	return g
}

// LexicalPassesGate compares raw Bleve BM25 against a normalized atan floor.
func LexicalPassesGate(rawBM25, minNormalized float64) bool {
	if rawBM25 <= 0 {
		return false
	}
	return NormalizeBM25(rawBM25) >= minNormalized
}

// VectorPassesGate compares top cosine against the configured floor.
func VectorPassesGate(cosine, minCosine float64) bool {
	if cosine <= 0 {
		return false
	}
	return cosine >= minCosine
}

// topScore returns the highest score from a map, or 0 when empty.
func topScore(scores map[string]float64) float64 {
	top := 0.0
	for _, s := range scores {
		if s > top {
			top = s
		}
	}
	return top
}

// applyStrictGates drops weak retrieval legs. vectorLegEligible is true when the vector
// API returned hits (pre-gate); the vector floor is applied only when filtered vectorScores is non-empty.
func applyStrictGates(gates SearchGateConfig, bm25Scores, vectorScores map[string]float64, vectorLegEligible bool) (lexicalDropped, vectorDropped, fullyRejected bool) {
	if !gates.StrictGates {
		return false, false, false
	}

	topBM25 := topScore(bm25Scores)
	if len(bm25Scores) > 0 && !LexicalPassesGate(topBM25, gates.BM25MinNormalized) {
		for k := range bm25Scores {
			delete(bm25Scores, k)
		}
		lexicalDropped = true
	}

	if vectorLegEligible && len(vectorScores) > 0 {
		topCosine := topScore(vectorScores)
		if !VectorPassesGate(topCosine, gates.VectorMinCosine) {
			for k := range vectorScores {
				delete(vectorScores, k)
			}
			vectorDropped = true
		}
	}

	if len(bm25Scores) == 0 && len(vectorScores) == 0 {
		fullyRejected = true
	}
	return lexicalDropped, vectorDropped, fullyRejected
}

// rebuildRankedPairs sorts score maps into descending pairs for telemetry and shadow RRF.
func rebuildRankedPairs(scores map[string]float64) []rankedScorePair {
	pairs := make([]rankedScorePair, 0, len(scores))
	for urn, score := range scores {
		pairs = append(pairs, rankedScorePair{urn: urn, score: score})
	}
	for i := 1; i < len(pairs); i++ {
		for j := i; j > 0 && pairs[j].score > pairs[j-1].score; j-- {
			pairs[j], pairs[j-1] = pairs[j-1], pairs[j]
		}
	}
	return pairs
}

type rankedScorePair struct {
	urn   string
	score float64
}

func rankMap(pairs []rankedScorePair) map[string]int {
	ranks := make(map[string]int, len(pairs))
	for i, pair := range pairs {
		ranks[pair.urn] = i + 1
	}
	return ranks
}

func topURNByRRF(bRank, hRank map[string]int) string {
	all := make(map[string]struct{})
	for urn := range bRank {
		all[urn] = struct{}{}
	}
	for urn := range hRank {
		all[urn] = struct{}{}
	}
	bestURN := ""
	bestRRF := -1.0
	for urn := range all {
		score := RRFScore(bRank[urn], hRank[urn])
		if score > bestRRF {
			bestRRF = score
			bestURN = urn
		}
	}
	return bestURN
}

func topURNByFused(fused map[string]float64) string {
	bestURN := ""
	best := -1.0
	for urn, score := range fused {
		if score > best {
			best = score
			bestURN = urn
		}
	}
	return bestURN
}

// shadowRRFLeaderMismatch returns true when RRF rank fusion would pick a different top-1 than direct fusion.
func shadowRRFLeaderMismatch(bRank, hRank map[string]int, fused map[string]float64) bool {
	if len(fused) == 0 {
		return false
	}
	rrfTop := topURNByRRF(bRank, hRank)
	fusionTop := topURNByFused(fused)
	if rrfTop == "" || fusionTop == "" {
		return false
	}
	return rrfTop != fusionTop
}

// maxFusedScore returns the highest fused score in the map.
func maxFusedScore(fused map[string]float64) float64 {
	return topScore(fused)
}

// recordFusionWinnerTelemetry increments per-query vector/lexical win counters for the final winner.
func recordFusionWinnerTelemetry(vectorScores, bm25Scores map[string]float64, winURN string, alpha float64, trace *telemetry.SearchQueryTrace) {
	telemetry.RecordFusionWinner(vectorScores, bm25Scores, winURN, alpha, trace, NormalizeBM25)
}

// recordBM25SquashTelemetry stores the top-1 raw→normalized BM25 delta for dashboard traces.
func recordBM25SquashTelemetry(topRawBM25 float64, trace *telemetry.SearchQueryTrace) {
	if topRawBM25 <= 0 {
		return
	}
	norm := NormalizeBM25(topRawBM25)
	delta := topRawBM25 - norm
	telemetry.SearchMetrics.BM25SquashDeltas.Store(math.Float64bits(delta))
	if trace != nil {
		trace.BM25SquashDelta = delta
	}
}

// NormalizeBM25 applies the ADR-0016 atan squash used during score fusion.
func NormalizeBM25(rawBM25 float64) float64 {
	if rawBM25 <= 0 {
		return 0
	}
	const constantBaseFactor = 5.0
	return math.Atan(rawBM25/constantBaseFactor) / (math.Pi / 2)
}
