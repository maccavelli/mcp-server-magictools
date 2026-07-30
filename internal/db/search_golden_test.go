package db

import (
	"context"
	"errors"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

func TestSearchGoldenQueries_LexicalOnly(t *testing.T) {
	path := t.TempDir()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	tools := []*ToolRecord{
		{
			URN:            "recall:get_metrics",
			Name:           "get_metrics",
			Server:         "recall",
			Description:    "Returns recall health and search telemetry metrics",
			Intent:         "metrics health telemetry diagnostics",
			Category:       "diagnostic",
			ParameterNames: []string{"include_history", "window"},
			LexicalTokens:  []string{"telemetry"},
		},
		{
			URN:            "filesystem:read_file",
			Name:           "read_file",
			Server:         "filesystem",
			Description:    "Read file contents from disk",
			Intent:         "read open file contents",
			Category:       "filesystem",
			ParameterNames: []string{"path"},
			EnumValues:     []string{"utf-8", "binary"},
		},
	}
	for _, tool := range tools {
		if err := store.SaveTool(tool); err != nil {
			t.Fatal(err)
		}
	}

	golden := []struct {
		name     string
		query    string
		wantURN  string
		category string
	}{
		{name: "urn fast path", query: "recall:get_metrics", wantURN: "recall:get_metrics"},
		{name: "parameter name", query: "include_history", wantURN: "recall:get_metrics"},
		{name: "enum value", query: "utf-8", wantURN: "filesystem:read_file"},
		{name: "intent phrase", query: "read file contents", wantURN: "filesystem:read_file"},
		{name: "lexical token", query: "telemetry", wantURN: "recall:get_metrics", category: ""},
	}
	for _, tc := range golden {
		t.Run(tc.name, func(t *testing.T) {
			results, err := store.SearchTools(context.Background(), tc.query, tc.category, "", 0.0, 0.6, DomainSystem, true)
			if err != nil {
				t.Fatalf("search failed: %v", err)
			}
			if len(results) == 0 {
				t.Fatalf("expected results for %q", tc.query)
			}
			if results[0].URN != tc.wantURN {
				t.Fatalf("query %q: got %s want %s", tc.query, results[0].URN, tc.wantURN)
			}
		})
	}
}

func TestSearchGoldenCategoryFilter(t *testing.T) {
	path := t.TempDir()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.SaveTool(&ToolRecord{
		URN: "a:x", Name: "x", Server: "a", Category: "filesystem", Description: "file ops",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveTool(&ToolRecord{
		URN: "b:y", Name: "y", Server: "b", Category: "diagnostic", Description: "file ops",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.SearchTools(context.Background(), "file ops", "filesystem", "", 0.0, 0.6, DomainSystem, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].URN != "a:x" {
		t.Fatalf("category filter failed: %+v", results)
	}
}

func TestSearchToolsFallbackIntent(t *testing.T) {
	path := t.TempDir()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.SaveTool(&ToolRecord{
		URN: "demo:ping", Name: "ping", Server: "demo", Category: "diagnostic",
		Intent: "health latency probe",
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.SearchToolsFallback("latency", "", "", DomainSystem)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 || results[0].URN != "demo:ping" {
		t.Fatalf("fallback intent match failed: %+v", results)
	}
}

func TestStrictGatesRejectWeakQuery(t *testing.T) {
	path := t.TempDir()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	store.SearchGates = SearchGatesFromConfig(true, 0.72, 0.15, true)
	if err := store.SaveTool(&ToolRecord{
		URN:         "demo:tool",
		Name:        "demo_tool",
		Server:      "demo",
		Description: "A narrowly scoped demo tool",
		Category:    "demo",
	}); err != nil {
		t.Fatal(err)
	}

	_, err = store.SearchTools(context.Background(), "xyzzy nonsense query", "", "", 0.3, 0.6, DomainSystem, true)
	if !errors.Is(err, ErrGatedNoMatch) {
		t.Fatalf("expected ErrGatedNoMatch, got %v", err)
	}
}

func TestSearchTelemetryTraceStored(t *testing.T) {
	path := t.TempDir()
	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	if err := store.SaveTool(&ToolRecord{
		URN:         "demo:ping",
		Name:        "ping",
		Server:      "demo",
		Description: "Health ping",
		Category:    "diagnostic",
	}); err != nil {
		t.Fatal(err)
	}

	_, err = store.SearchTools(context.Background(), "health ping", "", "", 0.0, 0.6, DomainSystem, true)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	trace := telemetry.SearchMetrics.LastQueryTrace.Load()
	if trace == nil || trace.Query != "health ping" {
		t.Fatalf("expected query trace, got %+v", trace)
	}
	if trace.MaxFused <= 0 {
		t.Fatalf("expected positive fused score in trace, got %+v", trace)
	}
}
