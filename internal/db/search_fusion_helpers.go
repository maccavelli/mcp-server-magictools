package db

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/blevesearch/bleve/v2/search"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

func (s *Store) runVectorSearchOverlay(ctx context.Context, query string, skipVector bool) (results []vector.ScoredResult, attempted bool, searchErr error) {
	if skipVector {
		return nil, false, nil
	}
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return nil, false, nil
	}
	attempted = true
	telemetry.SearchMetrics.VectorSearchAttempts.Add(1)
	results, searchErr = e.SearchWithScores(ctx, query, searchRecallK)
	if searchErr != nil {
		telemetry.SearchMetrics.VectorSearchErrors.Add(1)
		slog.Log(ctx, util.LevelTrace, "db: vector search unavailable, using pure BM25", "error", searchErr)
	}
	return results, attempted, searchErr
}

func (s *Store) handleFusionNoHits(query, category, serverConstraint string, domain SearchDomain, hitCount, vecCount int) ([]*ToolRecord, bool, error) {
	if hitCount > 0 || vecCount > 0 {
		return nil, false, nil
	}
	if s.SearchGates.StrictGates && s.SearchGates.DisableSearchFallback {
		trace := &telemetry.SearchQueryTrace{Query: query, GatesRejected: true}
		telemetry.SearchMetrics.GateRejections.Add(1)
		telemetry.SearchMetrics.LastQueryTrace.Store(trace)
		slog.Debug("search: no hits and fallback disabled by strict gates", "query", query)
		return nil, true, ErrGatedNoMatch
	}
	telemetry.SearchMetrics.FallbackInvocations.Add(1)
	trace := &telemetry.SearchQueryTrace{Query: query, FallbackUsed: true}
	telemetry.SearchMetrics.LastQueryTrace.Store(trace)
	fallback, err := s.SearchToolsFallback(query, category, serverConstraint, domain)
	if err == nil && len(fallback) > 0 {
		telemetry.SearchMetrics.FallbackRescues.Add(1)
		telemetry.SearchMetrics.LexicalWins.Add(1)
		trace.FusionWinner = "lexical"
		telemetry.SearchMetrics.LastQueryTrace.Store(trace)
	}
	return fallback, true, err
}

func recordSearchCollisionGap(query string, uniqueHits []fusionLocalHit) {
	if len(uniqueHits) < 2 {
		return
	}
	for i := 1; i < len(uniqueHits); i++ {
		for j := i; j > 0 && uniqueHits[j].Score > uniqueHits[j-1].Score; j-- {
			uniqueHits[j], uniqueHits[j-1] = uniqueHits[j-1], uniqueHits[j]
		}
	}
	s1 := uniqueHits[0]
	s2 := uniqueHits[1]
	gap := s1.Score - s2.Score
	telemetry.Collisions.Record(telemetry.CollisionEvent{
		Timestamp: time.Now().UnixNano(),
		Query:     query,
		S1URN:     s1.ID,
		S1Score:   s1.Score,
		S2URN:     s2.ID,
		S2Score:   s2.Score,
		Gap:       gap,
		Collision: gap < 0.05,
	})
}

type fusionLocalHit struct {
	ID    string
	Score float64
}

func collectFusedToolRecords(
	fusedScores map[string]float64,
	records map[string]*ToolRecord,
	descFragments map[string]string,
	serverConstraint string,
	scoreThreshold float64,
) ([]*ToolRecord, []fusionLocalHit) {
	var results []*ToolRecord
	var uniqueHits []fusionLocalHit
	seenURNs := make(map[string]bool)
	for urn, fusedScore := range fusedScores {
		if scoreThreshold > 0 && fusedScore < (scoreThreshold*perResultCullFactor) {
			continue
		}
		record, ok := records[urn]
		if !ok || seenURNs[urn] {
			continue
		}
		if serverConstraint != "" && !strings.EqualFold(record.Server, serverConstraint) {
			continue
		}
		seenURNs[urn] = true
		record.ConfidenceScore = fusedScore
		record.HighlightedDescription = record.Description
		if frag, ok := descFragments[urn]; ok {
			record.HighlightedDescription = frag
		}
		results = append(results, record)
		uniqueHits = append(uniqueHits, fusionLocalHit{ID: urn, Score: fusedScore})
	}
	return results, uniqueHits
}

func rerankFusionResults(results []*ToolRecord, query, serverConstraint string) []*ToolRecord {
	if len(results) <= 1 {
		if len(results) == 1 {
			r := results[0]
			rel := r.Metrics.ProxyReliability
			if rel == 0 {
				rel = 1.0
			}
			blended := applyLexicalBoosts(r.ConfidenceScore*rel, r, strings.ToLower(query))
			r.ConfidenceScore = blended
		}
		return results
	}
	var maxUsage int64
	for _, r := range results {
		if r.UsageCount > maxUsage {
			maxUsage = r.UsageCount
		}
	}
	if maxUsage == 0 {
		for i := 1; i < len(results); i++ {
			for j := i; j > 0 && results[j].ConfidenceScore > results[j-1].ConfidenceScore; j-- {
				results[j], results[j-1] = results[j-1], results[j]
			}
		}
		return results
	}
	type scored struct {
		record *ToolRecord
		score  float64
	}
	scoredResults := make([]scored, len(results))
	queryLower := strings.ToLower(query)
	for i, r := range results {
		usageScore := math.Log10(float64(r.UsageCount)+1.0) / math.Log10(float64(maxUsage)+1.0)
		baseScore := r.ConfidenceScore*0.7 + usageScore*0.3
		rel := r.Metrics.ProxyReliability
		if rel == 0 {
			rel = 1.0
		}
		blended := applyLexicalBoosts(baseScore*rel, r, queryLower)
		r.ConfidenceScore = blended
		scoredResults[i] = scored{record: r, score: blended}
	}
	for i := 1; i < len(scoredResults); i++ {
		for j := i; j > 0 && scoredResults[j].score > scoredResults[j-1].score; j-- {
			scoredResults[j], scoredResults[j-1] = scoredResults[j-1], scoredResults[j]
		}
	}
	out := make([]*ToolRecord, 0, len(scoredResults))
	serverCounts := make(map[string]int)
	for _, sr := range scoredResults {
		if serverConstraint != "" && !strings.EqualFold(sr.record.Server, serverConstraint) {
			continue
		}
		if serverConstraint != "" || serverCounts[sr.record.Server] < config.Intelligence.MaxResultsPerServer {
			out = append(out, sr.record)
			serverCounts[sr.record.Server]++
		}
	}
	return out
}

type fusionScoreBundle struct {
	bm25Scores    map[string]float64
	descFragments map[string]string
	records       map[string]*ToolRecord
	vectorScores  map[string]float64
	bTop          []string
	hTop          []string
	bPairs        []rankedScorePair
	hPairs        []rankedScorePair
}

func (s *Store) buildFusionScoreBundle(
	hits search.DocumentMatchCollection,
	vecResults []vector.ScoredResult,
	query, category, serverConstraint string,
	domain SearchDomain,
) fusionScoreBundle {
	bundle := fusionScoreBundle{
		bm25Scores:    make(map[string]float64),
		descFragments: make(map[string]string),
		records:       make(map[string]*ToolRecord),
		vectorScores:  make(map[string]float64),
	}
	for _, hit := range hits {
		bundle.bm25Scores[hit.ID] = hit.Score
		if frags, ok := hit.Fragments["description"]; ok && len(frags) > 0 {
			d := strings.ReplaceAll(frags[0], "<mark>", "**")
			d = strings.ReplaceAll(d, "</mark>", "**")
			bundle.descFragments[hit.ID] = d
		}
	}
	for _, vr := range vecResults {
		rec, err := s.GetTool(vr.Key)
		if err != nil {
			continue
		}
		if !recordAllowedInSearch(rec, query, category, serverConstraint, domain) {
			continue
		}
		bundle.records[vr.Key] = rec
		bundle.vectorScores[vr.Key] = vr.Score
	}
	bundle.bPairs = sortRankedPairs(bundle.bm25Scores)
	bundle.hPairs = sortRankedPairs(bundle.vectorScores)
	bundle.bTop = topURNsFromPairs(bundle.bPairs)
	bundle.hTop = topURNsFromPairs(bundle.hPairs)
	return bundle
}

func sortRankedPairs(scores map[string]float64) []rankedScorePair {
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

const fusionTopTelemetryN = 5

func topURNsFromPairs(pairs []rankedScorePair) []string {
	var top []string
	for i := 0; i < len(pairs) && i < fusionTopTelemetryN; i++ {
		top = append(top, pairs[i].urn)
	}
	return top
}

func (s *Store) applyFusionGateTelemetry(
	query string,
	bundle *fusionScoreBundle,
	vectorLegEligible bool,
) (bool, error) {
	lexicalDropped, vectorDropped, gatedReject := applyStrictGates(
		s.SearchGates, bundle.bm25Scores, bundle.vectorScores, vectorLegEligible,
	)
	if lexicalDropped {
		telemetry.SearchMetrics.LexicalLegDropped.Add(1)
		bundle.bPairs = rebuildRankedPairs(bundle.bm25Scores)
		bundle.bTop = topURNsFromPairs(bundle.bPairs)
	}
	if vectorDropped {
		telemetry.SearchMetrics.VectorLegDropped.Add(1)
		bundle.hPairs = rebuildRankedPairs(bundle.vectorScores)
		bundle.hTop = topURNsFromPairs(bundle.hPairs)
	}
	if !gatedReject {
		return false, nil
	}
	trace := &telemetry.SearchQueryTrace{
		Query:             query,
		GatesRejected:     true,
		LexicalLegDropped: lexicalDropped,
		VectorLegDropped:  vectorDropped,
	}
	if len(bundle.bPairs) > 0 {
		trace.TopBM25 = bundle.bPairs[0].score
	}
	if len(bundle.hPairs) > 0 {
		trace.TopCosine = bundle.hPairs[0].score
	}
	telemetry.SearchMetrics.GateRejections.Add(1)
	telemetry.SearchMetrics.LastQueryTrace.Store(trace)
	slog.Debug("search: strict gates rejected all legs",
		"query", query,
		"top_bm25", trace.TopBM25,
		"top_cosine", trace.TopCosine,
		"vector_min", s.SearchGates.VectorMinCosine,
		"bm25_min_norm", s.SearchGates.BM25MinNormalized)
	return true, ErrGatedNoMatch
}

func computeFusedScores(
	bm25Scores, vectorScores map[string]float64,
	alpha, corroborationBonus float64,
) map[string]float64 {
	allURNs := make(map[string]struct{})
	for urn := range bm25Scores {
		allURNs[urn] = struct{}{}
	}
	for urn := range vectorScores {
		allURNs[urn] = struct{}{}
	}
	fusedScores := make(map[string]float64)
	if len(vectorScores) > 0 {
		for urn := range allURNs {
			fusedScores[urn] = Score(vectorScores[urn], bm25Scores[urn], alpha, corroborationBonus)
		}
		return fusedScores
	}
	for urn := range allURNs {
		if score, ok := bm25Scores[urn]; ok {
			fusedScores[urn] = NormalizeBM25(score)
		}
	}
	return fusedScores
}

func (s *Store) applyFusionRecordBoosts(
	fusedScores map[string]float64,
	records map[string]*ToolRecord,
) map[string]*ToolRecord {
	for urn := range fusedScores {
		record, ok := records[urn]
		if !ok {
			var err error
			record, err = s.GetTool(urn)
			if err != nil {
				slog.Warn("search: bleve hit missing from badger (index drift)", "urn", urn, "error", err)
				telemetry.SearchMetrics.IndexDriftHits.Add(1)
				delete(fusedScores, urn)
				continue
			}
			records[urn] = record
		}
		boost := 1.0
		if record.Metrics.ProxyReliability > 1.0 {
			boost += (record.Metrics.ProxyReliability - 1.0) * s.Fusion.ReliabilityBoostWeight
		}
		if record.UsageCount > 0 {
			boost += math.Log1p(float64(record.UsageCount)) * s.Fusion.UsageBoostWeight
		}
		if record.IsNative {
			boost += s.Fusion.NativeBoost
		}
		fusedScores[urn] *= boost
	}
	return records
}

func buildFusionQueryTrace(
	query string,
	bundle fusionScoreBundle,
	fusedScores map[string]float64,
	vectorAttempted bool,
	vectorSearchErr error,
) *telemetry.SearchQueryTrace {
	trace := &telemetry.SearchQueryTrace{
		Query:             query,
		MaxFused:          maxFusedScore(fusedScores),
		VectorAttempted:   vectorAttempted,
		VectorSearchError: vectorSearchErr != nil,
	}
	if len(bundle.bPairs) > 0 {
		trace.TopBM25 = bundle.bPairs[0].score
		recordBM25SquashTelemetry(bundle.bPairs[0].score, trace)
	}
	if len(bundle.hPairs) > 0 {
		trace.TopCosine = bundle.hPairs[0].score
	}
	bRank := rankMap(bundle.bPairs)
	hRank := rankMap(bundle.hPairs)
	if shadowRRFLeaderMismatch(bRank, hRank, fusedScores) {
		trace.ShadowRRFMismatch = true
		telemetry.SearchMetrics.ShadowRRFMismatches.Add(1)
	}
	return trace
}

func (s *Store) tryFusionThresholdFallback(
	query, category, serverConstraint string,
	domain SearchDomain,
	scoreThreshold, topFused float64,
	trace *telemetry.SearchQueryTrace,
) ([]*ToolRecord, bool, error) {
	if scoreThreshold <= 0 || topFused >= scoreThreshold {
		return nil, false, nil
	}
	if s.SearchGates.StrictGates && s.SearchGates.DisableSearchFallback {
		slog.Debug("search: fused threshold miss with fallback suppressed", "query", query, "max_fused", topFused, "threshold", scoreThreshold)
		return nil, true, ErrGatedNoMatch
	}
	telemetry.SearchMetrics.FallbackInvocations.Add(1)
	trace.FallbackUsed = true
	telemetry.SearchMetrics.LastQueryTrace.Store(trace)
	fallback, err := s.SearchToolsFallback(query, category, serverConstraint, domain)
	if err == nil && len(fallback) > 0 {
		telemetry.SearchMetrics.FallbackRescues.Add(1)
		telemetry.SearchMetrics.LexicalWins.Add(1)
		trace.FusionWinner = "lexical"
		telemetry.SearchMetrics.LastQueryTrace.Store(trace)
		return fallback, true, nil
	}
	slog.Debug("High confidence threshold missed and fallback failed", "query", query)
	return nil, false, err
}
