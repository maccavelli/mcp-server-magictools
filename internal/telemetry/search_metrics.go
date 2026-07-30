package telemetry

import "sync/atomic"

// SearchQueryTrace captures per-query diagnostics for calibration dashboards.
type SearchQueryTrace struct {
	Query             string
	TopBM25           float64
	TopCosine         float64
	MaxFused          float64
	FallbackUsed      bool
	GatesRejected     bool
	LexicalLegDropped bool
	VectorLegDropped  bool
	ShadowRRFMismatch bool
	VectorAttempted   bool
	VectorSearchError bool
	BM25SquashDelta   float64
	FusionWinner      string // "vector", "lexical", or ""
	FastPath          string // "urn", "internal", or ""
}

// SearchMetricsRegistry holds global atomic counters for the semantic search pipeline.
type SearchMetricsRegistry struct {
	TotalSearches          atomic.Int64
	VectorSearches         atomic.Int64 // HNSW semantic path
	VectorSearchAttempts   atomic.Int64 // Vector leg invoked (including errors)
	VectorSearchErrors     atomic.Int64 // Vector API failures
	AlignSearchInvocations atomic.Int64 // Bleve search invocations from align_tools
	LexicalSearches        atomic.Int64 // Deprecated: mirrors AlignSearchInvocations
	TotalLatencyMs         atomic.Int64
	TotalConfidenceScore   atomic.Uint64 // Float64 stored as Uint64 bits for moving avg
	CacheHits              atomic.Int64  // Vector upsert hash cache hits (skip re-embed)
	CacheMisses            atomic.Int64  // Vector upsert required fresh embed
	EmbedLatencyMs         atomic.Int64  // Cumulative document embedding API latency
	QueryEmbedLatencyMs    atomic.Int64  // Cumulative query embedding API latency
	VectorStaleSkips       atomic.Int64  // UpsertDocument skipped unchanged hash
	GraphCompletenessRatio atomic.Uint64 // Float64 bits: HNSW nodes / Bleve tool docs
	VectorWins             atomic.Int64  // Fusion: vector score dominated the blend
	LexicalWins            atomic.Int64  // Fusion: lexical score dominated the blend
	AlignCacheHits         atomic.Int64  // L1 Intent Cache Hits
	AlignCacheMisses       atomic.Int64  // L1 Intent Cache Misses
	SchemaMutations        atomic.Int64  // Semantic index schema mutations
	LearningWeight         atomic.Uint64 // Float64 stored as Uint64 bits

	// Top 5 Index Decision Matrix Pointers (for dashboard tracking)
	LastBleveTop5  atomic.Pointer[[]string]
	LastHnswTop5   atomic.Pointer[[]string]
	LastQueryTrace atomic.Pointer[SearchQueryTrace]

	GateRejections      atomic.Int64
	FallbackInvocations atomic.Int64
	FallbackRescues     atomic.Int64 // Fallback returned non-empty results used by caller
	LexicalLegDropped   atomic.Int64
	VectorLegDropped    atomic.Int64
	ShadowRRFMismatches atomic.Int64
	IndexDriftHits      atomic.Int64 // Bleve URN present but missing from Badger
	IndexRepairs        atomic.Int64 // BLV-1: tools missing from Bleve, re-indexed from Badger

	ActionVerbBoostsApplied atomic.Int64
	DynamicGapPassesSafe    atomic.Int64
	BM25SquashDeltas        atomic.Uint64 // Float64 stored as Uint64 bits for delta tracking
}

// SearchMetrics is the global instance of search metrics.
var SearchMetrics = &SearchMetricsRegistry{}
