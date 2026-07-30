package db

import (
	"testing"
)

func TestSearchQueryTokens(t *testing.T) {
	t.Parallel()
	tokens := searchQueryTokens("read file contents")
	if len(tokens) != 3 {
		t.Fatalf("got %v", tokens)
	}
}

func TestBuildTokenizedKeywordQuery(t *testing.T) {
	t.Parallel()
	q := buildTokenizedKeywordQuery([]string{"include", "history"}, "lexical_tokens", 1.0)
	if q == nil {
		t.Fatal("expected query")
	}
}

func TestFallbackTextMatchIntent(t *testing.T) {
	t.Parallel()
	rec := &ToolRecord{
		Name:   "other",
		Intent: "metrics health telemetry",
	}
	if !fallbackTextMatch(rec, []string{"telemetry"}, "telemetry") {
		t.Fatal("expected intent field match in fallback")
	}
}

func TestFallbackTextMatchRequires(t *testing.T) {
	t.Parallel()
	rec := &ToolRecord{
		Name:     "compose",
		Requires: []string{"discover_project"},
	}
	if !fallbackTextMatch(rec, []string{"discover"}, "discover") {
		t.Fatal("expected requires field match in fallback")
	}
}
