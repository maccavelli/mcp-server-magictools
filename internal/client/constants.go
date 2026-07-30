package client

// Managed server and taxonomy identifiers.
const (
	serverMagictools   = "magictools"
	serverRecall       = "recall"
	serverGoModernizer = "go-modernizer"
	serverBrainstorm   = "brainstorm"
	serverUnknownName  = "unknown"
)

// Tool category labels (deriveCategory and hydrator).
const (
	categoryMemory       = "memory"
	categoryDevops       = "devops"
	categoryFilesystem   = "filesystem"
	categorySearch       = "search"
	categorySystem       = "system"
	categoryAgent        = "agent"
	categoryRefactor     = "refactor"
	categoryCore         = "core"
	categoryPlugin       = "plugin"
	categoryOrchestrator = "orchestrator"
)

// Snapshot and dashboard JSON field keys.
const (
	keyCategory   = "category"
	keyCount      = "count"
	keyImpactType = "impact_type"
	keyURN        = "urn"
	keyActive     = "active"
)

// Synonym map keys (expandSynonyms / crossCategoryExclusions).
const (
	synonymQuery  = "query"
	synonymPath   = "path"
	synonymRecall = "recall"
)

// Cross-category exclusion phrase literals.
const (
	phraseRefactorCode = "refactor code"
	phraseRecallMemory = "recall memory"
	phraseGitCommit    = "git commit"
	phraseSearchWeb    = "search web"
	phraseSearchTheWeb = "search the web"
	phraseReadFile     = "read file"
)

// Scoring telemetry category labels.
const (
	scoringFaultRecovery = "Fault Recovery"
	scoringNominalOps    = "Nominal Operations"
	scoringTrendingAlign = "Trending Alignment"
	impactPenalty        = "Penalty"
	impactNeutral        = "Neutral"
	impactReward         = "Reward"
)
