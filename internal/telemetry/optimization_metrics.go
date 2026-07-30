package telemetry

import "sync/atomic"

// OptMetricsRegistry holds global atomic counters for the three primary
// content optimization pipelines.
type OptMetricsRegistry struct {
	SqueezeBypassCount      atomic.Int64
	SqueezeTruncations      atomic.Int64
	TotalRawBytes           atomic.Int64 // Token-Value: pre-squeeze payload size
	TotalSqueezedBytes      atomic.Int64 // Token-Value: post-squeeze payload size
	HFSCReassemblySuccesses atomic.Int64
	HFSCReassemblyFails     atomic.Int64
	HFSCSweptStale          atomic.Int64
	HFSCActiveStreams       atomic.Int64 // gauge: len(r.sessions)
	CSSAOffloadBytes        atomic.Int64
	CSSASyncOperations      atomic.Int64
}

// OptMetrics is the global instance of optimization metrics.
var OptMetrics = &OptMetricsRegistry{}

// ArgumentRepairsRegistry tracks proxy argument deserialization repair frequency
// by repair type. Exposed via self_check under "argument_repairs".
type ArgumentRepairsRegistry struct {
	DoubleEncoded atomic.Uint64
	XMLStripped   atomic.Uint64
	FlatStructure atomic.Uint64
	TrailingComma atomic.Uint64
	NestedUnwrap  atomic.Uint64
	Heuristic     atomic.Uint64
	TotalAttempts atomic.Uint64
	TotalFailures atomic.Uint64
}

// ArgumentRepairs is the global instance of argument repair metrics.
var ArgumentRepairs = &ArgumentRepairsRegistry{}

// ProxyOptimizationRegistry tracks the effectiveness of the Tier 1/Tier 2
// proxy resilience improvements across all execution paths.
type ProxyOptimizationRegistry struct {
	TemplatesServed     atomic.Uint64 // Call templates delivered via align_tools discovery mode
	Tier1AutoExecutions atomic.Uint64 // Safe coercion → auto-execute succeeded (no retry needed)
	Tier2StructuredErrs atomic.Uint64 // Missing required → structured correction template returned
	InlineExecutions    atomic.Uint64 // align_tools inline executions (arguments provided, auto-selected, executed)
	InlineSkipped       atomic.Uint64 // align_tools had arguments but fell back to discovery mode
	ExtraFieldsStripped atomic.Uint64 // Hallucinated extra fields removed by Tier 1 CoerceAndStrip
}

// ProxyOptimization is the global instance of proxy resilience metrics.
var ProxyOptimization = &ProxyOptimizationRegistry{}
