package config

import "time"

// Proxy holds hardcoded magic values for proxy operations.
var Proxy = struct {
	MaxResponseBytes       int
	TruncateLimit          int
	SmallResponseThreshold int
	Tier2Timeout           time.Duration
}{
	MaxResponseBytes:       24000,
	TruncateLimit:          2000,
	SmallResponseThreshold: 1024,
	Tier2Timeout:           6 * time.Minute,
}

// Intelligence holds hardcoded magic values for scoring and routing search.
var Intelligence = struct {
	ConfidenceGap       float64
	PRFOverlapThreshold float64
	MaxResultsPerServer int
}{
	ConfidenceGap:       0.4,
	PRFOverlapThreshold: 0.30,
	MaxResultsPerServer: 3,
}
