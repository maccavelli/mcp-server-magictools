package db

// JSON Schema property type literals.
const (
	schemaTypeObject = "object"
)

// Scoreboard trending delta field names.
const (
	deltaKey30m   = "Delta30m"
	deltaKey4h    = "Delta4h"
	deltaKeyAll   = "DeltaAll"
	scoreBoardCap = 20
)

// Default trigger keyword → server mappings (PopulateDefaultTriggers).
const (
	serverGlab            = "glab"
	serverDdgSearch       = "ddg-search"
	serverSeqThinking     = "seq-thinking"
	serverMagicskills     = "magicskills"
	serverRecall          = "recall"
	serverFilesystem      = "filesystem"
	serverGit             = "git"
	serverEvolvePlan      = "evolve-plan"
	serverSocraticThinker = "socratic-thinker"
	serverGithub          = "github"
)

// Common action verbs used for lexical score boosts.
const actionVerbSearch = "search"

// Default trigger map keys (PopulateDefaultTriggers).
const (
	triggerKeywordPipeline = "pipeline"
	triggerKeywordTest     = "test"
	triggerKeywordMemory   = "memory"
	triggerKeywordPath     = "path"
)
