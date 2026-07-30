package dag

import (
	"regexp"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

var bm25StopWords = map[string]bool{
	"the": true, "and": true, "a": true, "to": true, "of": true,
	"in": true, "i": true, "is": true, "that": true, "it": true,
	"on": true, "you": true, "this": true, "for": true, "or": true, "with": true,
	"as": true, "by": true, "an": true, "be": true,
}

func bm25AppendFilteredTokens(raw []string, tokens []string, stopWords map[string]bool, weight int) []string {
	for _, tok := range tokens {
		if stopWords[tok] {
			continue
		}
		for range weight {
			raw = append(raw, tok)
		}
	}
	return raw
}

func bm25TokensFromText(text string, tokenizer *regexp.Regexp, stopWords map[string]bool, weight int) []string {
	if text == "" {
		return nil
	}
	return bm25AppendFilteredTokens(nil, tokenizer.FindAllString(strings.ToLower(text), -1), stopWords, weight)
}

func bm25BuildDocumentTokens(t *db.ToolRecord, tokenizer *regexp.Regexp) []string {
	desc := roleTagStripper.ReplaceAllString(t.Description, "")
	raw := bm25TokensFromText(desc, tokenizer, bm25StopWords, 1)
	raw = bm25AppendFilteredTokens(raw, tokenizer.FindAllString(strings.ToLower(t.Name), -1), bm25StopWords, 2)
	raw = bm25AppendFilteredTokens(raw, tokenizer.FindAllString(strings.ToLower(t.URN), -1), bm25StopWords, 3)
	for _, si := range t.SyntheticIntents {
		raw = bm25AppendFilteredTokens(raw, tokenizer.FindAllString(strings.ToLower(si), -1), bm25StopWords, 2)
	}
	for _, lt := range t.LexicalTokens {
		raw = bm25AppendFilteredTokens(raw, tokenizer.FindAllString(strings.ToLower(lt), -1), bm25StopWords, 3)
	}
	if t.Intent != "" {
		raw = bm25AppendFilteredTokens(raw, tokenizer.FindAllString(strings.ToLower(t.Intent), -1), bm25StopWords, 2)
	}
	return raw
}

func bm25ProxyReliability(t *db.ToolRecord) float64 {
	if t.Metrics.ProxyReliability > 0 {
		return t.Metrics.ProxyReliability
	}
	return 1.0
}

func bm25IndexDocFreq(scorer *BM25Scorer, tokens []string) {
	seen := make(map[string]bool)
	for _, tok := range tokens {
		if seen[tok] {
			continue
		}
		scorer.docFreq[tok]++
		seen[tok] = true
	}
}
