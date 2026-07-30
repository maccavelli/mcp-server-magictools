package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCleanPlannerOutput_JSONEnvelope(t *testing.T) {
	// Simulate the real PLANNER output: a JSON envelope wrapping markdown_payload
	// where the payload contains noise sections (Recall history) and signal sections.
	payload := strings.Join([]string{
		"# Implementation Plan: mcp-server-test",
		"",
		"## Requirements",
		"",
		"## Project History (from Recall)",
		`{"count":5,"entries":[{"key":"massive-recall-noise","record":{"content":"diff --git a/file.go..."}}]}`,
		"",
		"## Applicable Standards (from Recall)",
		"Standards search for 'Go context': 5 results.",
		"",
		"## Step 1: brainstorm:discover_project [ANALYZER]",
		`{"data":{"gaps":[]},"summary":"Discovery complete."}`,
		"",
		"## Thesis: Codebase Modernization Analysis",
		"### Type Safety (Score: 8/10)",
		"interface{}/any usage found.",
		"",
		"## Proposed Changes",
		"Based on diagnostics, the following changes are recommended.",
		"",
		"### File: internal/handler/main.go",
		"- Replace interface{} with any",
	}, "\n")

	raw, _ := json.Marshal(map[string]string{"markdown_payload": payload})

	result := cleanPlannerOutput(string(raw), "ep-test-123", "/project/test")

	// Verify noise sections are stripped.
	if strings.Contains(result, "Project History") {
		t.Error("expected Project History to be stripped")
	}
	if strings.Contains(result, "Applicable Standards") {
		t.Error("expected Applicable Standards to be stripped")
	}
	if strings.Contains(result, "Requirements") && !strings.Contains(result, "Proposed") {
		t.Error("expected Requirements section to be stripped")
	}
	if strings.Contains(result, "massive-recall-noise") {
		t.Error("expected Recall noise content to be stripped")
	}

	// Verify raw JSON step echoes are stripped.
	if strings.Contains(result, `"data":{"gaps":[]}`) {
		t.Error("expected raw JSON step output to be stripped")
	}

	// Verify signal sections are kept.
	if !strings.Contains(result, "Thesis: Codebase Modernization Analysis") {
		t.Error("expected Thesis section to be kept")
	}
	if !strings.Contains(result, "Proposed Changes") {
		t.Error("expected Proposed Changes to be kept")
	}
	if !strings.Contains(result, "Replace interface{} with any") {
		t.Error("expected file-level changes to be kept")
	}

	// Verify header is correct.
	if !strings.Contains(result, "# Implementation Plan: test") {
		t.Error("expected header with project basename")
	}
	if !strings.Contains(result, "ep-test-123") {
		t.Error("expected session ID in header")
	}
}

func TestCleanPlannerOutput_RawMarkdown(t *testing.T) {
	// Test with raw markdown (no JSON envelope).
	raw := strings.Join([]string{
		"# Implementation Plan: project",
		"",
		"## Historical Context",
		"Some old session data.",
		"",
		"## Proposed Changes",
		"Fix the thing.",
	}, "\n")

	result := cleanPlannerOutput(raw, "ep-raw-456", "/project/raw")

	if strings.Contains(result, "Historical Context") {
		t.Error("expected Historical Context to be stripped")
	}
	if !strings.Contains(result, "Proposed Changes") {
		t.Error("expected Proposed Changes to be kept")
	}
}

func TestExtractSocraticVerdict(t *testing.T) {
	stages := []PipelineStep{
		{ToolName: "brainstorm:thesis_architect", Role: "CRITIC"},
		{ToolName: "brainstorm:aporia_engine", Role: "SYNTHESIZER"},
		{ToolName: "go-modernizer:generate_implementation_plan", Role: "PLANNER"},
	}

	apoOutput := `{"safe_path_verdict":"REJECT","refusal_to_proceed":true,"resolutions":[{"pillar":"Type Safety","thesis_score":8,"skeptic_score":3,"resolution":"APORIA"},{"pillar":"Modernization","thesis_score":8,"skeptic_score":7,"resolution":"ADOPT"}]}`

	results := []stepResult{
		{URN: "brainstorm:thesis_architect", Status: "DONE", Output: "thesis output"},
		{URN: "brainstorm:aporia_engine", Status: "DONE", Output: apoOutput},
		{URN: "go-modernizer:generate_implementation_plan", Status: "DONE", Output: "plan output"},
	}

	sr := extractSocraticVerdict(results, stages)
	if sr == nil {
		t.Fatal("expected non-nil socratic result")
	}
	if sr.Verdict != "REJECT" {
		t.Errorf("expected verdict REJECT, got %s", sr.Verdict)
	}
	if len(sr.Pillars) != 2 {
		t.Fatalf("expected 2 pillars, got %d", len(sr.Pillars))
	}
	if sr.Pillars[0].Name != "Type Safety" {
		t.Errorf("expected first pillar 'Type Safety', got %q", sr.Pillars[0].Name)
	}
	if sr.Pillars[0].Thesis != 8 || sr.Pillars[0].Skeptic != 3 {
		t.Errorf("unexpected scores: thesis=%d skeptic=%d", sr.Pillars[0].Thesis, sr.Pillars[0].Skeptic)
	}
	if sr.Pillars[0].Resolution != "APORIA" {
		t.Errorf("expected resolution APORIA, got %s", sr.Pillars[0].Resolution)
	}
}

func TestShouldInjectMutators(t *testing.T) {
	tests := []struct {
		name     string
		verdict  string
		output   string
		pillars  []pillarResult
		expected bool
	}{
		{"ADOPT with fixes", "ADOPT", "suggest_fixes found 3 issues that should be changed", nil, true},
		{"ADOPT without fixes", "ADOPT", "no issues found in the codebase", nil, false},
		{"APPROVE with fixes", "APPROVE", "suggest_fixes found issues that should be changed", nil, true},
		{"APPROVE without fixes", "APPROVE", "no issues found in the codebase", nil, false},
		{"ADOPT_WITH_MITIGATION with fixes", "ADOPT_WITH_MITIGATION", "recommend refactor for clarity", nil, true},
		{"REJECT with fixes but no pillars", "REJECT", "suggest_fixes found issues that should be changed", nil, false},
		{"REVIEW with fixes", "REVIEW", "suggest_fixes found issues that should be changed", nil, false},
		{"APORIA with fixes", "APORIA", "should be changed and modernized", nil, false},
		{"empty verdict", "", "suggest_fixes found issues", nil, false},
		{"ADOPT with recommendation", "ADOPT", "recommendation: replace deprecated API", nil, true},
		{"ADOPT with refactor signal", "ADOPT", "this code should be refactored for clarity", nil, true},
		// Majority-vote override: REJECT but majority of pillars voted ADOPT
		{"REJECT majority ADOPT pillars", "REJECT", "suggest_fixes found issues that should be changed", []pillarResult{
			{Name: "Type Safety", Resolution: "APORIA"},
			{Name: "Modernization", Resolution: "ADOPT"},
			{Name: "Modularization", Resolution: "ADOPT"},
			{Name: "Error Handling", Resolution: "ADOPT"},
			{Name: "Testing", Resolution: "ADOPT"},
			{Name: "Documentation", Resolution: "ADOPT"},
		}, true},
		// REJECT with majority non-ADOPT pillars — should stay blocked
		{"REJECT majority APORIA pillars", "REJECT", "suggest_fixes found issues that should be changed", []pillarResult{
			{Name: "Type Safety", Resolution: "APORIA"},
			{Name: "Modernization", Resolution: "APORIA"},
			{Name: "Modularization", Resolution: "APORIA"},
			{Name: "Error Handling", Resolution: "ADOPT"},
		}, false},
		// REJECT with even split — should stay blocked (need strict majority)
		{"REJECT even split pillars", "REJECT", "suggest_fixes found issues that should be changed", []pillarResult{
			{Name: "Type Safety", Resolution: "APORIA"},
			{Name: "Modernization", Resolution: "ADOPT"},
		}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldInjectMutators(tt.verdict, tt.output, tt.pillars)
			if got != tt.expected {
				t.Errorf("shouldInjectMutators(%q, %q, pillars=%d) = %v, want %v",
					tt.verdict, tt.output, len(tt.pillars), got, tt.expected)
			}
		})
	}
}

func TestBuildStepSummaryJSON(t *testing.T) {
	results := []stepResult{
		{URN: "brainstorm:discover_project", Status: "DONE", Output: "short output"},
		{URN: "go-modernizer:go_ast_suite_analyzer", Status: "DONE", Output: strings.Repeat("x", 600)},
		{URN: "brainstorm:aporia_engine", Status: "DONE", Output: "verdict output"},
		{URN: "go-modernizer:generate_implementation_plan", Status: "FAILED", Error: "timeout"},
	}

	data := buildStepSummaryJSON(results)
	if len(data) == 0 {
		t.Fatal("expected non-empty summary JSON")
	}

	var summaries []struct {
		Step    int    `json:"step"`
		URN     string `json:"urn"`
		Status  string `json:"status"`
		Error   string `json:"error,omitempty"`
		Excerpt string `json:"excerpt,omitempty"`
	}
	if err := json.Unmarshal(data, &summaries); err != nil {
		t.Fatalf("failed to parse summary JSON: %v", err)
	}
	if len(summaries) != 4 {
		t.Fatalf("expected 4 summaries, got %d", len(summaries))
	}

	// Verify step numbering.
	if summaries[0].Step != 1 || summaries[3].Step != 4 {
		t.Errorf("step numbering wrong: first=%d last=%d", summaries[0].Step, summaries[3].Step)
	}

	// Verify excerpt truncation for long output.
	if len(summaries[1].Excerpt) > 510 {
		t.Errorf("expected excerpt truncated to ~503 chars, got %d", len(summaries[1].Excerpt))
	}
	if !strings.HasSuffix(summaries[1].Excerpt, "...") {
		t.Error("expected truncated excerpt to end with '...'")
	}

	// Verify short output preserved.
	if summaries[0].Excerpt != "short output" {
		t.Errorf("expected short output preserved, got %q", summaries[0].Excerpt)
	}

	// Verify error captured.
	if summaries[3].Error != "timeout" {
		t.Errorf("expected error 'timeout', got %q", summaries[3].Error)
	}
}

func TestBuildPipelineResultWithArtifacts(t *testing.T) {
	results := []stepResult{
		{URN: "brainstorm:discover_project", Status: "DONE"},
	}
	artifactURIs := map[string]string{
		"implementation_plan": "mcp://magictools/raw/pipeline_ep-test_plan",
		"audit_report":        "mcp://magictools/raw/pipeline_ep-test_audit",
		"step_summary":        "mcp://magictools/raw/pipeline_ep-test_summary",
	}

	result := buildPipelineResultWithArtifacts(results, "COMPLETED", nil, "ep-test", artifactURIs)
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	text := ""
	for _, c := range result.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text = tc.Text
		}
	}
	if text == "" {
		t.Fatal("expected non-empty text content")
	}

	// Verify metadata contains session_id.
	if !strings.Contains(text, `"session_id": "ep-test"`) {
		t.Error("expected session_id in metadata")
	}

	// Verify artifact URIs appear in metadata.
	if !strings.Contains(text, "mcp://magictools/raw/pipeline_ep-test_plan") {
		t.Error("expected implementation_plan artifact URI in metadata")
	}
	if !strings.Contains(text, "mcp://magictools/raw/pipeline_ep-test_audit") {
		t.Error("expected audit_report artifact URI in metadata")
	}
	if !strings.Contains(text, "mcp://magictools/raw/pipeline_ep-test_summary") {
		t.Error("expected step_summary artifact URI in metadata")
	}

	// Verify basic wrapper still works without artifacts.
	basic := buildPipelineResult(results, "FAILED", []string{"test warning"})
	basicText := ""
	for _, c := range basic.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			basicText = tc.Text
		}
	}
	if strings.Contains(basicText, "artifacts") {
		t.Error("basic buildPipelineResult should not contain artifacts")
	}
	if !strings.Contains(basicText, "test warning") {
		t.Error("expected warning in basic result")
	}
}
