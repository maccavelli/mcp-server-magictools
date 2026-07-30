package telemetry

// RecordFusionWinner increments vector/lexical win counters for the given winner URN
// using alpha-weighted leg contributions (ADR-0016). Tie goes to lexical.
func RecordFusionWinner(vectorScores, bm25Scores map[string]float64, winURN string, alpha float64, trace *SearchQueryTrace, normalizeBM25 func(float64) float64) {
	if winURN == "" || normalizeBM25 == nil {
		return
	}
	vecScore := vectorScores[winURN]
	norm := normalizeBM25(bm25Scores[winURN])

	switch {
	case vecScore > 0 && norm <= 0:
		SearchMetrics.VectorWins.Add(1)
		if trace != nil {
			trace.FusionWinner = "vector"
		}
	case norm > 0 && vecScore <= 0:
		SearchMetrics.LexicalWins.Add(1)
		if trace != nil {
			trace.FusionWinner = "lexical"
		}
	case vecScore > 0 && norm > 0:
		vecContrib := alpha * vecScore
		lexContrib := (1 - alpha) * norm
		if vecContrib > lexContrib {
			SearchMetrics.VectorWins.Add(1)
			if trace != nil {
				trace.FusionWinner = "vector"
			}
		} else {
			SearchMetrics.LexicalWins.Add(1)
			if trace != nil {
				trace.FusionWinner = "lexical"
			}
		}
	}
}

// AlignCacheHitRatio returns L1 align cache hit ratio in [0,1].
func AlignCacheHitRatio(hits, misses int64) float64 {
	denom := hits + misses
	if denom <= 0 {
		return 0
	}
	return float64(hits) / float64(denom)
}

// AvgSearchLatencyMs returns mean search latency per align_tools search.
func AvgSearchLatencyMs(totalLatencyMs, totalSearches int64) int64 {
	if totalSearches <= 0 {
		return 0
	}
	return totalLatencyMs / totalSearches
}
