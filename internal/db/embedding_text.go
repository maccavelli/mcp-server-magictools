package db

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strings"
)

// BuildEmbeddingText constructs the semantic payload for HNSW vector embedding.
// Structural metadata (server, tool name, category) is omitted to avoid artificial
// semantic convergence between similarly named tools.
func BuildEmbeddingText(rec *ToolRecord) string {
	if rec == nil {
		return ""
	}
	var parts []string

	if len(rec.NegativeTriggers) > 0 {
		parts = append(parts, "NOT: "+strings.Join(rec.NegativeTriggers, ", "))
	}

	parts = append(parts, stripEmbeddingBoilerplate(rec.Description))

	if rec.Intent != "" {
		parts = append(parts, rec.Intent)
	}
	if len(rec.SyntheticIntents) > 0 {
		parts = append(parts, strings.Join(rec.SyntheticIntents, " "))
	}
	if len(rec.LexicalTokens) > 0 {
		parts = append(parts, strings.Join(rec.LexicalTokens, " "))
	}
	if len(rec.ParameterNames) > 0 {
		parts = append(parts, strings.Join(rec.ParameterNames, " "))
	}
	if len(rec.EnumValues) > 0 {
		limit := min(len(rec.EnumValues), 10)
		parts = append(parts, strings.Join(rec.EnumValues[:limit], " "))
	}
	if len(rec.Requires) > 0 {
		parts = append(parts, strings.Join(rec.Requires, " "))
	}
	if len(rec.Triggers) > 0 {
		parts = append(parts, strings.Join(rec.Triggers, " "))
	}

	return strings.Join(parts, " | ")
}

// ComputeEmbeddingHash returns a stable checksum for provider+model+text invalidation.
func ComputeEmbeddingHash(providerModel, embeddingText string) string {
	h := sha256.Sum256([]byte(providerModel + embeddingText))
	return fmt.Sprintf("%x", h)
}

var (
	embeddingDirectiveRE = regexp.MustCompile(`\[DIRECTIVE:[^\]]+\]\s*`)
	embeddingKeywordsRE  = regexp.MustCompile(`(?i)\s*Keywords:\s*[^|]+$`)
)

func stripEmbeddingBoilerplate(text string) string {
	text = embeddingDirectiveRE.ReplaceAllString(text, "")
	text = embeddingKeywordsRE.ReplaceAllString(text, "")
	return strings.TrimSpace(text)
}
