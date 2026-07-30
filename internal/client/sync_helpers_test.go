package client

import (
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestDeriveCategory(t *testing.T) {
	m := &WarmRegistry{}
	tests := []struct {
		server   string
		expected string
	}{
		{"mcp-server-recall", "memory"},
		{"memory-store", "memory"},
		{"git-analyzer", "devops"},
		{"github-proxy", "devops"},
		{"glab-ci", "devops"},
		{"filesystem-tool", "filesystem"},
		{"fs-explorer", "filesystem"},
		{"ddg-search", "search"},
		{"duckduckgo", "search"},
		{"bash-executor", "system"},
		{"shell-runner", "system"},
		{"magictools", "core"},
		{"go-modernizer", "refactor"},
		{"go-modernizer", "refactor"},
		{"random-plugin", "plugin"},
	}

	for _, tt := range tests {
		got := m.deriveCategory(tt.server)
		if got != tt.expected {
			t.Errorf("deriveCategory(%q) = %q; want %q", tt.server, got, tt.expected)
		}
	}
}

func TestMinifyDescription(t *testing.T) {
	m := &WarmRegistry{Config: &config.Config{}}

	// Test short description
	short := "Brief description."
	if got := m.minifyDescription(short); got != short {
		t.Errorf("minifyDescription(%q) = %q; want %q", short, got, short)
	}

	// Test long description
	long := "This is a very long description that definitely exceeds two hundred characters to ensure the truncation logic is triggered correctly. It has multiple sentences. We should truncate it for efficiency. And it should keep only first two sentences plus an ellipsis."
	expected := "This is a very long description that definitely exceeds two hundred characters to ensure the truncation logic is triggered correctly. It has multiple sentences..."
	if got := m.minifyDescription(long); got != expected {
		t.Errorf("minifyDescription(long) = %q; want %q", got, expected)
	}

	// Test very long single sentence
	veryLong := "This is a very long sentence without any periods that goes on and on and on and on and on and on and on and on and on and on and on and on and on and on and on and on and on and on and on and on and on and on."
	if got := m.minifyDescription(veryLong); len(got) != 200 {
		t.Errorf("minifyDescription(veryLong) length = %d; want 200", len(got))
	}
}

func TestToSchemaMap(t *testing.T) {
	m := &WarmRegistry{}

	// Nil case
	if got := m.toSchemaMap(nil); got != nil {
		t.Errorf("toSchemaMap(nil) = %v; want nil", got)
	}

	// Map case
	inputMap := map[string]any{"type": "string"}
	if got := m.toSchemaMap(inputMap); got["type"] != "string" {
		t.Errorf("toSchemaMap(map) = %v; want %v", got, inputMap)
	}

	// Struct case
	type schema struct {
		Type string `json:"type"`
	}
	inputStruct := schema{Type: "integer"}
	got := m.toSchemaMap(inputStruct)
	if got["type"] != "integer" {
		t.Errorf("toSchemaMap(struct) = %v; want map with type:integer", got)
	}
}

func TestExtractIntent(t *testing.T) {
	m := &WarmRegistry{}
	name := "search_files"
	desc := "Search for files in the directory.\nALIASES: find, locate\nUSE_WHEN: you need to find a file."

	intent := m.extractIntent(name, desc)

	// Check for presence of key terms
	expectedTerms := []string{"find", "locate", "files", "directory", "search"}
	for _, term := range expectedTerms {
		if !strings.Contains(intent, term) {
			t.Errorf("intent %q does not contain %q", intent, term)
		}
	}

	// Check for synonym expansion (search -> lookup)
	if !strings.Contains(intent, "lookup") {
		t.Errorf("intent %q does not contain expanded synonym 'lookup'", intent)
	}

	// Check stop words exclusion
	if strings.Contains(intent, " the ") {
		t.Errorf("intent %q should not contain stop word 'the'", intent)
	}
}

func TestHashSchema(t *testing.T) {
	m := &WarmRegistry{}
	t1 := &mcp.Tool{
		Name:        "tool1",
		Description: "desc1",
		InputSchema: map[string]any{"type": "object"},
	}
	t2 := &mcp.Tool{
		Name:        "tool1",
		Description: "desc1",
		InputSchema: map[string]any{"type": "object"},
	}
	t3 := &mcp.Tool{
		Name:        "tool2",
		Description: "desc1",
		InputSchema: map[string]any{"type": "object"},
	}

	h1 := m.hashSchema(t1)
	h2 := m.hashSchema(t2)
	h3 := m.hashSchema(t3)

	if h1 != h2 {
		t.Errorf("hashSchema identical tools: %q != %q", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("hashSchema different tools: %q == %q", h1, h3)
	}
}

func TestParseAnnotations(t *testing.T) {
	desc := `Search for logs.
ALIASES: grep_logs, find_logs
USE_WHEN: debugging errors
CASCADES: report_error`

	aliases, useWhen, cascades := parseAnnotations(desc)

	if len(aliases) != 2 || aliases[0] != "grep_logs" || aliases[1] != "find_logs" {
		t.Errorf("parseAnnotations aliases = %v; want [grep_logs find_logs]", aliases)
	}
	if len(useWhen) != 1 || useWhen[0] != "debugging errors" {
		t.Errorf("parseAnnotations useWhen = %v; want [debugging errors]", useWhen)
	}
	if len(cascades) != 1 || cascades[0] != "report_error" {
		t.Errorf("parseAnnotations cascades = %v; want [report_error]", cascades)
	}
}

func TestJsonCoercionInHelpers(t *testing.T) {
	m := &WarmRegistry{}

	// Test toSchemaMap with invalid JSON struct (though rare in Go)
	type badStruct struct {
		Func func() `json:"-"`
	}
	// This should not panic but return empty/nil safely based on current implementation
	_ = m.toSchemaMap(badStruct{})
}

// ─── Semantic Hydrator Tests ───────────────────────────────────────────────

func TestComputeWBase(t *testing.T) {
	// Schema with mixed required/optional params
	richSchema := map[string]any{
		"properties": map[string]any{
			"query":    map[string]any{"type": "string"},
			"category": map[string]any{"type": "string"},
			"limit":    map[string]any{"type": "integer"},
		},
		"required": []any{"query"},
	}
	// Schema with all required
	strictSchema := map[string]any{
		"properties": map[string]any{
			"urn":       map[string]any{"type": "string"},
			"arguments": map[string]any{"type": "object"},
		},
		"required": []any{"urn", "arguments"},
	}
	sixTriggers := []string{"a", "b", "c", "d", "e", "f"}
	twoTriggers := []string{"a", "b"}

	tests := []struct {
		name     string
		desc     string
		category string
		schema   map[string]any
		triggers []string
		minW     float64
		maxW     float64
	}{
		{
			name:     "core tool with strict schema and full triggers",
			desc:     "MANDATORY: System critical tool.",
			category: "core",
			schema:   strictSchema,
			triggers: sixTriggers,
			minW:     1.0, // high specificity (1.0) + exclusivity (1.0) + complexity (0.5) + core (0.05)
			maxW:     1.3,
		},
		{
			name:     "plugin with no schema and no triggers",
			desc:     "A simple tool.",
			category: "plugin",
			schema:   nil,
			triggers: nil,
			minW:     0.5,
			maxW:     0.85,
		},
		{
			name:     "agent with rich schema and some triggers",
			desc:     "Sequential reasoning chain.",
			category: "agent",
			schema:   richSchema,
			triggers: twoTriggers,
			minW:     0.8, // low specificity (0.33) + medium exclusivity + medium complexity + agent offset
			maxW:     1.1,
		},
		{
			name:     "memory with strict schema and full triggers",
			desc:     "MANDATORY: Search memory with BM25 scoring.",
			category: "memory",
			schema:   strictSchema,
			triggers: sixTriggers,
			minW:     0.95,
			maxW:     1.3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeWBase(tt.desc, tt.category, tt.schema, tt.triggers)
			if got < tt.minW || got > tt.maxW {
				t.Errorf("computeWBase() = %f; want between %f and %f", got, tt.minW, tt.maxW)
			}
		})
	}

	// Verify clamping bounds
	got := computeWBase("x", "plugin", nil, nil)
	if got < 0.5 {
		t.Errorf("computeWBase should never go below 0.5, got %f", got)
	}
}

func TestGenerateSyntheticIntents(t *testing.T) {
	intents := generateSyntheticIntents("search_files", "Search for files in the directory using pattern matching.", "filesystem")

	if len(intents) != 14 {
		t.Fatalf("generateSyntheticIntents() produced %d intents; want 14", len(intents))
	}

	// Verify no empty strings
	for i, intent := range intents {
		if intent == "" {
			t.Errorf("intent[%d] is empty", i)
		}
	}

	// Verify uniqueness
	seen := make(map[string]bool)
	for _, intent := range intents {
		if seen[intent] {
			t.Errorf("duplicate synthetic intent: %q", intent)
		}
		seen[intent] = true
	}
}

func TestGenerateLexicalTokens(t *testing.T) {
	schema := map[string]any{
		"properties": map[string]any{
			"query":          map[string]any{"type": "string"},
			"max_results":    map[string]any{"type": "integer"},
			"case_sensitive": map[string]any{"type": "boolean"},
		},
	}

	tokens := generateLexicalTokens("search_files", schema)

	// Should contain name parts
	found := make(map[string]bool)
	for _, t := range tokens {
		found[t] = true
	}

	expected := []string{"search", "files", "query", "max", "results", "case", "sensitive"}
	for _, e := range expected {
		if !found[e] {
			t.Errorf("generateLexicalTokens missing expected token %q; got %v", e, tokens)
		}
	}

	// Test with nil schema
	nilTokens := generateLexicalTokens("simple_tool", nil)
	if len(nilTokens) < 2 {
		t.Errorf("generateLexicalTokens(nil schema) should still extract name tokens; got %v", nilTokens)
	}
}

func TestGenerateNegativeTriggers(t *testing.T) {
	tests := []struct {
		name     string
		category string
		count    int
	}{
		{"git_commit", "devops", 6},
		{"go_complexity_analyzer", "plugin", 6},
		{"sequential_thinking", "agent", 6},
		{"search_memory", "memory", 6},
		{"web_search", "search", 6},
		{"read_file", "filesystem", 6},
		{"sync_ecosystem", "orchestrator", 6},
		{"unknown_tool", "alien", 6}, // fallback
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			triggers := generateNegativeTriggers(tt.name, tt.category)
			if len(triggers) != tt.count {
				t.Errorf("generateNegativeTriggers(%q, %q) = %d triggers; want %d", tt.name, tt.category, len(triggers), tt.count)
			}

			// Verify no empty strings
			for i, trig := range triggers {
				if trig == "" {
					t.Errorf("trigger[%d] is empty for %q", i, tt.name)
				}
			}
		})
	}
}

func TestDeriveCategoryAgent(t *testing.T) {
	m := &WarmRegistry{}

	// Verify the new agent category
	tests := []struct {
		server   string
		expected string
	}{
		{"seq-thinking", "agent"},
		{"sequential-thinking", "agent"},
		{"mcp-server-sequential-thinking", "agent"},
	}

	for _, tt := range tests {
		got := m.deriveCategory(tt.server)
		if got != tt.expected {
			t.Errorf("deriveCategory(%q) = %q; want %q", tt.server, got, tt.expected)
		}
	}
}
