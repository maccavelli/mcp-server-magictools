package db

import (
	"strings"
	"testing"
)

func TestBuildEmbeddingText(t *testing.T) {
	t.Parallel()
	rec := &ToolRecord{
		Description:      "[DIRECTIVE: Test] Do something useful. Keywords: foo, bar",
		Intent:           "utility action",
		SyntheticIntents: []string{"perform task"},
		LexicalTokens:    []string{"alpha"},
		NegativeTriggers: []string{"never delete"},
	}
	text := BuildEmbeddingText(rec)
	if strings.Contains(text, "[DIRECTIVE:") {
		t.Fatal("directive boilerplate should be stripped")
	}
	if strings.Contains(text, "Keywords:") {
		t.Fatal("keywords boilerplate should be stripped")
	}
	if !strings.Contains(text, "NOT: never delete") {
		t.Fatal("negative triggers should be included")
	}
	if !strings.Contains(text, "perform task") {
		t.Fatal("synthetic intents should be included")
	}
}

func TestBuildEmbeddingTextGetMetricsLike(t *testing.T) {
	t.Parallel()
	rec := &ToolRecord{
		Description:      "Returns recall health and telemetry metrics",
		Intent:           "fetch operational metrics",
		SyntheticIntents: []string{"telemetry dashboard"},
		ParameterNames:   []string{"server_name", "window"},
		EnumValues:       []string{"1h", "24h", "7d", "30d", "90d", "180d", "365d", "all"},
		Requires:         []string{"recall:connect"},
		Triggers:         []string{"metrics", "health"},
	}
	text := BuildEmbeddingText(rec)
	for _, want := range []string{"server_name", "window", "1h", "recall:connect", "metrics", "telemetry dashboard"} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in embedding text, got %q", want, text)
		}
	}
}

func TestComputeEmbeddingHashDeterministic(t *testing.T) {
	t.Parallel()
	a := ComputeEmbeddingHash("openai:text-embedding-3-small", "hello")
	b := ComputeEmbeddingHash("openai:text-embedding-3-small", "hello")
	if a != b {
		t.Fatal("hash should be deterministic")
	}
	if ComputeEmbeddingHash("openai:text-embedding-3-small", "world") == a {
		t.Fatal("hash should change with text")
	}
}
