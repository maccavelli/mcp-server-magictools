package db

import (
	"context"
	"log/slog"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

func (s *Store) searchToolsFuseResults(ctx context.Context, query string, category string, serverConstraint string, scoreThreshold float64, alpha float64, domain SearchDomain, skipVector bool) ([]*ToolRecord, error) {
	searchResult, err := s.Index.Search(ctx, query, category, serverConstraint, domain)
	if err != nil {
		return nil, err
	}
	vecResults, vectorAttempted, vectorSearchErr := s.runVectorSearchOverlay(ctx, query, skipVector)
	if res, done, err := s.handleFusionNoHits(query, category, serverConstraint, domain, len(searchResult.Hits), len(vecResults)); done {
		return res, err
	}

	vectorLegEligible := vectorAttempted && vectorSearchErr == nil && len(vecResults) > 0
	bundle := s.buildFusionScoreBundle(searchResult.Hits, vecResults, query, category, serverConstraint, domain)
	if gated, err := s.applyFusionGateTelemetry(query, &bundle, vectorLegEligible); gated {
		return nil, err
	}

	telemetry.SearchMetrics.LastBleveTop5.Store(&bundle.bTop)
	telemetry.SearchMetrics.LastHnswTop5.Store(&bundle.hTop)

	fusedScores := computeFusedScores(bundle.bm25Scores, bundle.vectorScores, alpha, s.Fusion.CorroborationBonus)
	records := s.applyFusionRecordBoosts(fusedScores, bundle.records)

	trace := buildFusionQueryTrace(query, bundle, fusedScores, vectorAttempted, vectorSearchErr)
	telemetry.SearchMetrics.LastQueryTrace.Store(trace)
	slog.Debug("search: query trace",
		"query", query,
		"top_bm25", trace.TopBM25,
		"top_cosine", trace.TopCosine,
		"max_fused", trace.MaxFused,
		"shadow_rrf_mismatch", trace.ShadowRRFMismatch)

	if res, done, err := s.tryFusionThresholdFallback(query, category, serverConstraint, domain, scoreThreshold, trace.MaxFused, trace); done {
		return res, err
	}

	results, uniqueHits := collectFusedToolRecords(fusedScores, records, bundle.descFragments, serverConstraint, scoreThreshold)
	recordSearchCollisionGap(query, uniqueHits)
	results = rerankFusionResults(results, query, serverConstraint)

	if len(results) > 0 {
		recordFusionWinnerTelemetry(bundle.vectorScores, bundle.bm25Scores, results[0].URN, alpha, trace)
		telemetry.SearchMetrics.LastQueryTrace.Store(trace)
	}

	RecordSearchGraphCompleteness(s)

	return results, nil
}
