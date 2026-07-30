package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
)

// PipelineSession holds the state of a pipeline execution for telemetry.
type PipelineSession struct {
	SessionID      string         `json:"session_id"`
	Target         string         `json:"target"`
	Intent         string         `json:"intent"`
	PlanHash       string         `json:"plan_hash"`
	Status         string         `json:"status"` // ANALYZING, MUTATING, COMPLETED, FAILED
	Stages         []PipelineStep `json:"stages"`
	CompletedSteps []stepResult   `json:"completed_steps"`
	Warnings       []string       `json:"warnings"`
	ChainedOutput  string         `json:"chained_output"`
	CreatedAt      int64          `json:"created_at"`
	UpdatedAt      int64          `json:"updated_at"`
}

// ── Socratic Verdict Types ──

type socraticResult struct {
	Verdict string         `json:"verdict"`
	Pillars []pillarResult `json:"pillars"`
}

type pillarResult struct {
	Name       string `json:"name"`
	Thesis     int    `json:"thesis"`
	Skeptic    int    `json:"skeptic"`
	Resolution string `json:"resolution"`
}

// ── Plan Cleaning Types ──

// ── Noise Section Prefixes ──
// These heading prefixes identify sections in the PLANNER's markdown_payload
// that are Recall echo noise and should be stripped from the clean plan.
var noisePrefixes = []string{
	"Project History",
	"Requirements",
	"Applicable Standards",
	"Historical Context",
}

// isNoiseSection returns true if a section heading starts with a known noise prefix.
func isNoiseSection(heading string) bool {
	for _, p := range noisePrefixes {
		if strings.HasPrefix(heading, p) {
			return true
		}
	}
	return false
}

// isRawJSONSection returns true if a section body is predominantly raw JSON
// (a step output echo), not human-readable markdown.
func isRawJSONSection(heading, body string) bool {
	// Step outputs look like "Step N: urn [ROLE]\n\n{\"data\":..."
	if !strings.HasPrefix(heading, "Step ") {
		return false
	}
	trimmed := strings.TrimSpace(body)
	return strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[")
}

// cleanPlannerOutput extracts the clean markdown plan from the PLANNER step
// output, stripping the JSON envelope and filtering out Recall noise sections.
//
// The PLANNER tool typically returns:
//
//	{"markdown_payload": "# Implementation Plan...\n## Project History...\n## Proposed Changes..."}
//
// This function:
//  1. Unwraps the JSON envelope if present (extracts markdown_payload)
//  2. Splits on "\n## " section boundaries
//  3. Filters out noise sections (Recall history, empty Requirements, Standards dumps)
//  4. Filters out raw JSON step output echoes
//  5. Keeps only signal sections (Diagnostics, Proposed Changes, Execution Traces)
//  6. Reassembles clean markdown
func cleanPlannerOutput(raw string, sessionID, target string) string {
	// Step 1: Unwrap JSON envelope if present.
	payload := raw
	var envelope struct {
		MarkdownPayload string `json:"markdown_payload"`
	}
	if err := json.Unmarshal([]byte(raw), &envelope); err == nil && envelope.MarkdownPayload != "" {
		payload = envelope.MarkdownPayload
	}

	// Step 2: Split on section boundaries. We split on "\n## " to get each
	// "## Heading\nBody" block. The first element is the pre-header content.
	parts := strings.Split(payload, "\n## ")

	// Step 3+4: Filter sections.
	var kept []string
	for i, part := range parts {
		if i == 0 {
			// The first part is content before the first "## " — typically a
			// top-level "# Implementation Plan: ..." header. Keep the title
			// line but strip any noise body.
			continue
		}

		// Extract heading (first line of the section).
		heading := strings.SplitN(part, "\n", 2)[0]
		body := ""
		if _, after, ok := strings.Cut(part, "\n"); ok {
			body = after
		}

		// Filter noise.
		if isNoiseSection(heading) {
			continue
		}

		// Filter raw JSON step echoes.
		if isRawJSONSection(heading, body) {
			continue
		}

		kept = append(kept, part)
	}

	// Step 5: Reassemble with structured header.
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Implementation Plan: %s\n\n", filepath.Base(target))
	fmt.Fprintf(&sb, "> **Session**: `%s`\n", sessionID)
	fmt.Fprintf(&sb, "> **Target**: `%s`\n\n", target)

	for _, section := range kept {
		sb.WriteString("\n## ")
		sb.WriteString(section)
	}

	return sb.String()
}

// extractSocraticVerdict scans step results for the SYNTHESIZER (aporia_engine)
// output and extracts the structured verdict with per-pillar resolutions.
func extractSocraticVerdict(results []stepResult, stages []PipelineStep) *socraticResult {
	// Find SYNTHESIZER steps.
	synthURNs := make(map[string]bool)
	for _, s := range stages {
		if s.Role == roleSynthesizer {
			synthURNs[s.ToolName] = true
		}
	}

	// Scan for the aporia output (last SYNTHESIZER step with output).
	var apoOutput string
	for i := len(results) - 1; i >= 0; i-- {
		if synthURNs[results[i].URN] && results[i].Output != "" {
			apoOutput = results[i].Output
			break
		}
	}
	if apoOutput == "" {
		return nil
	}

	// The aporia output is JSON like:
	// {"safe_path_verdict":verdictReject,"resolutions":[{"pillar":"...","thesis_score":8,"skeptic_score":3,"resolution":"APORIA"},...]}
	// Try to parse it. It may be wrapped in markdown or plain JSON.
	jsonStr := apoOutput
	// If it starts with non-JSON content, try to find the JSON block.
	if !strings.HasPrefix(strings.TrimSpace(jsonStr), "{") {
		if idx := strings.Index(jsonStr, "{"); idx >= 0 {
			jsonStr = jsonStr[idx:]
			// Find the matching closing brace
			depth := 0
			for j, ch := range jsonStr {
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
					if depth == 0 {
						jsonStr = jsonStr[:j+1]
						break
					}
				}
			}
		}
	}

	var apoData struct {
		Verdict     string `json:"safe_path_verdict"`
		Resolutions []struct {
			Pillar     string `json:"pillar"`
			Thesis     int    `json:"thesis_score"`
			Skeptic    int    `json:"skeptic_score"`
			Resolution string `json:"resolution"`
		} `json:"resolutions"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &apoData); err != nil {
		slog.Warn("extractSocraticVerdict: failed to parse aporia output",
			keyError, err, "raw_length", len(apoOutput),
			"json_prefix", jsonStr[:min(200, len(jsonStr))])
		return nil
	}

	slog.Info("extractSocraticVerdict: parsed verdict",
		"verdict", apoData.Verdict, "pillar_count", len(apoData.Resolutions))

	sr := &socraticResult{Verdict: apoData.Verdict}
	for _, r := range apoData.Resolutions {
		sr.Pillars = append(sr.Pillars, pillarResult{
			Name:       r.Pillar,
			Thesis:     r.Thesis,
			Skeptic:    r.Skeptic,
			Resolution: r.Resolution,
		})
	}
	return sr
}

// acceptedVerdicts defines the Socratic verdicts that permit MUTATOR injection.
// Both vocabulary sets are accepted for cross-server compatibility:
//   - ADOPT: used by per-pillar resolutions (resolveConflict)
//   - APPROVE: used by aggregate verdict (ComputeSafePathVerdict)
//   - ADOPT_WITH_MITIGATION: partial approval with caveats
var acceptedVerdicts = map[string]bool{
	verdictAdopt:               true,
	verdictApprove:             true,
	verdictAdoptWithMitigation: true,
}

// shouldInjectMutators determines whether MUTATOR stages should be dynamically
// injected into the pipeline based on the Socratic Trifecta verdict, the
// accumulated analysis output, and per-pillar resolutions.
//
// MUTATOR injection requires BOTH conditions:
//  1. The Socratic verdict is in the accepted set, OR the aggregate verdict is
//     REJECT but a strict majority of individual pillars resolved to ADOPT
//     (majority-vote override — prevents one contested pillar from vetoing the
//     entire pipeline).
//  2. The analysis output contains actionable fix proposals.
func shouldInjectMutators(socraticVerdict string, analysisOutput string, pillars []pillarResult) bool {
	if !analysisContainsFixes(analysisOutput) {
		return false
	}

	// Fast path: aggregate verdict is accepted.
	if acceptedVerdicts[socraticVerdict] {
		return true
	}

	// Majority-vote override: when the aggregate verdict is REJECT but individual
	// pillars show a majority voted ADOPT, permit mutations. This prevents a single
	// contested pillar (APORIA) from vetoing an otherwise approved pipeline.
	if socraticVerdict == verdictReject && len(pillars) > 0 {
		adoptCount := 0
		for _, p := range pillars {
			slog.Debug("shouldInjectMutators: pillar check",
				"name", p.Name, "resolution", p.Resolution)
			if p.Resolution == verdictAdopt || p.Resolution == verdictAdoptWithMitigation {
				adoptCount++
			}
		}
		slog.Info("shouldInjectMutators: majority-vote evaluation",
			"verdict", socraticVerdict,
			"adopt_count", adoptCount,
			"total_pillars", len(pillars),
			"threshold", len(pillars)/2)
		if adoptCount > len(pillars)/2 {
			slog.Info("shouldInjectMutators: majority-vote override APPROVED",
				"adopt_count", adoptCount,
				"total_pillars", len(pillars))
			return true
		}
	}

	return false
}

// intentRequiresMutation checks if the user's intent signals a desire for code changes.
func intentRequiresMutation(intent string) bool {
	signals := []string{
		signalFix, signalRefactor, signalModernize, "optimize", "clean",
		"apply", "edit", verbUpdate, "migrate", "upgrade",
		"replace", "remove", verbDelete, "add", "inject",
	}
	lower := strings.ToLower(intent)
	for _, s := range signals {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// analysisContainsFixes checks if the chained analysis output contains actionable fix proposals.
func analysisContainsFixes(output string) bool {
	indicators := []string{
		"suggest_fixes", verdictAdopt, "recommendation",
		"should be changed", "should be replaced",
		signalRefactor, signalModernize, signalFix,
	}
	lower := strings.ToLower(output)
	for _, ind := range indicators {
		if strings.Contains(lower, strings.ToLower(ind)) {
			return true
		}
	}
	return false
}

// filterOutRole removes a specific role from the target_roles slice.
func filterOutRole(roles []string, exclude string) []string {
	var result []string
	for _, r := range roles {
		if !strings.EqualFold(r, exclude) {
			result = append(result, r)
		}
	}
	return result
}

// classifyScope determines DAG breadth from the intent.
// Returns "narrow" (2-3 tools), "standard" (4-6 tools), or "broad" (8+ tools).
func classifyScope(intent string) string {
	lower := strings.ToLower(intent)

	narrowSignals := []string{"find missing", "check for", "only", "just", "specific", "single"}
	for _, s := range narrowSignals {
		if strings.Contains(lower, s) {
			return "narrow"
		}
	}

	// Broad detection: count signals rather than just first-match.
	broadSignals := []string{
		"full", "comprehensive", "end-to-end", "all", "everything", "complete", "entire",
		"best practices", "standards", "idioms", "adherence", "modernization", "modularization",
		"evaluate", "assess", "ensure",
	}
	broadCount := 0
	for _, s := range broadSignals {
		if strings.Contains(lower, s) {
			broadCount++
		}
	}

	// Multiple broad signals or long intent (20+ words) indicates a broad, multi-faceted request.
	wordCount := len(strings.Fields(lower))
	if broadCount >= 3 || (broadCount >= 1 && wordCount >= 20) {
		return "broad"
	}

	return "standard"
}

// composeMutatorStages builds the dynamic MUTATOR stage sequence for continuation.
func composeMutatorStages() []PipelineStep {
	return []PipelineStep{
		{
			ToolName: "go-modernizer:apply_vetted_edit",
			Role:     roleMutator,
			Phase:    8,
			Purpose:  "Apply approved code mutations from Socratic-vetted analysis.",
		},
		{
			ToolName: urnGoModernizerGoTestValidation,
			Role:     "VALIDATOR",
			Phase:    9,
			Purpose:  "Validate structural test constraints post-mutation.",
		},
		{
			ToolName: urnMagictoolsGenerateAuditReport,
			Role:     roleSynthesizer,
			Phase:    10,
			Purpose:  "Generate final audit report with git diff.",
		},
	}
}
