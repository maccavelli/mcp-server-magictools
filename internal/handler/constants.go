package handler

// Server identifiers.
const (
	serverMagictools = "magictools"
	serverBrainstorm = "brainstorm"
)

// Tool names (MCP tool registration).
const (
	toolAnalyzeSystemLogs    = "analyze_system_logs"
	toolExecutePipeline      = "execute_pipeline"
	toolValidatePipelineStep = "validate_pipeline_step"
	toolGenerateAuditReport  = "generate_audit_report"
	toolAlignTools           = "align_tools"
	toolCallProxy            = "call_proxy"
	toolSyncServers          = "sync_servers"
	toolReloadServers        = "reload_servers"
)

// Tool URNs used in pipeline orchestration.
const (
	urnGoModernizerGoTestValidation   = "go-modernizer:go_test_validation"
	urnMagictoolsGenerateAuditReport  = "magictools:generate_audit_report"
	urnBrainstormDiscoverProject      = "brainstorm:discover_project"
	urnBrainstormThesisArchitect      = "brainstorm:thesis_architect"
	urnBrainstormAntithesisSkeptic    = "brainstorm:antithesis_skeptic"
	urnBrainstormAporiaEngine         = "brainstorm:aporia_engine"
	urnGoModernizerGoASTSuiteAnalyzer = "go-modernizer:go_ast_suite_analyzer"
	urnGoModernizerGenerateImplPlan   = "go-modernizer:generate_implementation_plan"
	urnBrainstormGenerateFinalReport  = "brainstorm:generate_final_report"
)

// Pipeline roles.
const (
	roleAnalyzer    = "ANALYZER"
	roleCritic      = "CRITIC"
	roleSynthesizer = "SYNTHESIZER"
	rolePlanner     = "PLANNER"
	roleReporting   = "REPORTING"
	roleThreat      = "THREAT"
	roleDiagnostic  = "DIAGNOSTIC"
	roleMutator     = "MUTATOR"
)

// Status and outcome literals.
const (
	statusFailed    = "FAILED"
	statusBlocked   = "BLOCKED"
	statusDone      = "DONE"
	statusCompleted = "COMPLETED"
	statusError     = "error"
	statusFailedAlt = "failed"

	outcomeApproved        = "approved"
	outcomeDAGState        = "dag_state"
	outcomeReportGenerated = "report_generated"
)

// Socratic verdicts.
const (
	verdictAdopt               = "ADOPT"
	verdictApprove             = "APPROVE"
	verdictAdoptWithMitigation = "ADOPT_WITH_MITIGATION"
	verdictReject              = "REJECT"
)

// Envelope and metadata map keys.
const (
	keyMetadata   = "metadata"
	keyQuery      = "query"
	keySource     = "source"
	keyNamespace  = "namespace"
	keyServerID   = "server_id"
	keyLimit      = "limit"
	keyHits       = "hits"
	keyMisses     = "misses"
	keyItems      = "items"
	keyTarget     = "target"
	keyType       = "type"
	keyStatus     = "status"
	keySessionID  = "session_id"
	keyProjectID  = "project_id"
	keyOutcome    = "outcome"
	keyStateData  = "state_data"
	keyURN        = "urn"
	keyArguments  = "arguments"
	keyProperties = "properties"
	keyRequired   = "required"
	keyError      = "error"
	keyMeta       = "_meta"
)

// Recall and diagnostic sources.
const (
	sourceRecallFallbackBM25 = "recall_fallback_bm25"
	sourceRecall             = "recall"
	namespaceServerStatus    = "server_status"
	severityError            = "ERROR"
)

// JSON Schema type literals.
const (
	schemaTypeObject  = "object"
	schemaTypeString  = "string"
	schemaTypeInteger = "integer"
	schemaTypeArray   = "array"
)

// Proxy and repair constants.
const (
	planHashAutoApproved        = "auto-approved-orchestrator-override"
	proxyStatusNoLocalToolMatch = "NO_LOCAL_TOOL_MATCH"
	repairTypeXMLStripped       = "xml_stripped"
	errorMissingRequiredFields  = "MISSING_REQUIRED_FIELDS"
	dryRunType                  = "dry_run"
	planHashApprovalFmt         = `{"plan_hash":%q,"approved_by":"orchestrator_socratic_gate"}`
)

// Cache and limit defaults.
const (
	defaultAlignCacheSize = 500
	defaultRecallLimit    = 50
)

// Mutation verbs shared across safety checks.
const (
	verbDelete = "delete"
	verbUpdate = "update"
)

// Intent signal tokens for pipeline role inference.
const (
	signalFix       = "fix"
	signalRefactor  = "refactor"
	signalAudit     = "audit"
	signalPlan      = "plan"
	signalModernize = "modernize"
)

// Summary envelope keys.
const (
	keyResultCount = "result_count"
	keySuccess     = "success"
)
