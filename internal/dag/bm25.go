// Package dag provides functionality for the dag subsystem.
package dag

import (
	"maps"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// roleTagStripper removes [ROLE: X] prefixes from descriptions before BM25 tokenization.
var roleTagStripper = regexp.MustCompile(`(?i)\[ROLE:\s*\w+\]\s*`)

// Document represents an indexed tool with its enriched token corpus and
// empirical reliability score for post-BM25 confidence weighting.
type Document struct {
	URN              string
	Tokens           []string
	ProxyReliability float64 // BLV-10: reliability multiplier from ToolIntelligence, range [0.5,2.0], centered at 1.0 (neutral)
}

// BM25Scorer calculates semantic relevance using BM25.
type BM25Scorer struct {
	k1      float64
	b       float64
	docs    []Document
	avgdl   float64
	docFreq map[string]int
}

// NewBM25Scorer initializes the scoring matrix from pipeline-eligible tools.
// The token corpus is now enriched with intelligence fields (SyntheticIntents,
// LexicalTokens, Intent) that were previously ignored, giving the scorer access
// to the same semantic signal that makes the Bleve index effective.
func NewBM25Scorer(tools []*db.ToolRecord) *BM25Scorer {
	scorer := &BM25Scorer{k1: 1.2, b: 0.75, docFreq: make(map[string]int)}
	tokenizer := regexp.MustCompile(`[a-zA-Z0-9]+`)
	var totalLength int
	for _, t := range tools {
		if t == nil {
			continue
		}
		rawTokens := bm25BuildDocumentTokens(t, tokenizer)
		scorer.docs = append(scorer.docs, Document{
			URN: t.URN, Tokens: rawTokens, ProxyReliability: bm25ProxyReliability(t),
		})
		totalLength += len(rawTokens)
		bm25IndexDocFreq(scorer, rawTokens)
	}
	if len(scorer.docs) > 0 {
		scorer.avgdl = float64(totalLength) / float64(len(scorer.docs))
	}
	return scorer
}

// Score calculates the BM25 score for each document against the query,
// applies a ProxyReliability multiplier to weight empirical trust, then
// normalizes via sigmoid saturation to preserve natural score distribution.
func (s *BM25Scorer) Score(query string) map[string]float64 {
	scores := make(map[string]float64)
	queryTokens := strings.Fields(strings.ToLower(query))

	N := float64(len(s.docs))

	for _, doc := range s.docs {
		var docScore float64
		D := float64(len(doc.Tokens))

		termFreq := make(map[string]float64)
		for _, tok := range doc.Tokens {
			termFreq[tok]++
		}

		for _, qToken := range queryTokens {
			// Calculate IDF
			n := float64(s.docFreq[qToken])
			idf := math.Log(((N - n + 0.5) / (n + 0.5)) + 1.0)

			// Calculate TF component
			tf := termFreq[qToken]
			if tf == 0 {
				continue
			}

			tfNum := tf * (s.k1 + 1.0)
			tfDen := tf + s.k1*(1.0-s.b+s.b*(D/s.avgdl))

			docScore += idf * (tfNum / tfDen)
		}

		// ── ProxyReliability Multiplier ──
		// Scale raw BM25 score by the tool's historical success rate.
		// Tools that consistently succeed get a confidence lift; tools with
		// low reliability are dampened. Default reliability is 1.0 (neutral).
		scores[doc.URN] = docScore * doc.ProxyReliability
	}

	// ── Sigmoid Saturation Normalization ──
	// Replaces min-max normalization to preserve natural BM25 score distribution.
	// Truly irrelevant tools cluster near 0, truly relevant tools near 1, and
	// moderate tools spread across the middle — the median anchors the curve
	// to the actual corpus, not to the min/max extremes.
	sigmoidNormalize(scores)

	return scores
}

// sigmoidNormalize applies sigmoid saturation: 1 / (1 + exp(-k * (score - median))).
// This preserves score magnitude — a tool that barely matched doesn't get artificially
// inflated to 1.0 just because it's the best of a weak set.
func sigmoidNormalize(scores map[string]float64) {
	if len(scores) == 0 {
		return
	}

	// Collect raw scores for median computation.
	vals := make([]float64, 0, len(scores))
	for _, v := range scores {
		vals = append(vals, v)
	}
	sort.Float64s(vals)

	// Compute median.
	median := vals[len(vals)/2]
	if len(vals)%2 == 0 && len(vals) >= 2 {
		median = (vals[len(vals)/2-1] + vals[len(vals)/2]) / 2.0
	}

	// Steepness factor. Higher k = sharper separation between relevant/irrelevant.
	// k=4.0 works well for typical 15-30 tool candidate pools.
	k := 4.0

	for urn, raw := range scores {
		scores[urn] = 1.0 / (1.0 + math.Exp(-k*(raw-median)))
	}
}

// ScoreGroupedByServer runs BM25 independently within each server group, then
// sigmoid-normalizes within each group before merging. This prevents
// keyword-dense brainstorm descriptions from systematically outscoring
// technical go-modernizer descriptions due to cross-server IDF contamination.
//
// Each server gets its own IDF table and avgdl, so a go-modernizer tool
// matching 3 out of 5 query terms within a 15-tool go-modernizer pool scores
// fairly against a brainstorm tool matching 4 out of 5 within a 15-tool
// brainstorm pool.
func ScoreGroupedByServer(query string, tools []*db.ToolRecord) map[string]float64 {
	// Group tools by server.
	groups := make(map[string][]*db.ToolRecord)
	for _, t := range tools {
		if t == nil {
			continue
		}
		groups[t.Server] = append(groups[t.Server], t)
	}

	merged := make(map[string]float64)

	for _, serverTools := range groups {
		if len(serverTools) == 0 {
			continue
		}

		// Build and score a per-server BM25 scorer. Each server group gets
		// its own IDF table and average document length, ensuring that
		// keyword density differences between servers don't cross-contaminate.
		scorer := NewBM25Scorer(serverTools)
		serverScores := scorer.Score(query)

		// Merge per-server scores into the global result.
		// Scores are already sigmoid-normalized within each server group
		// (Score() calls sigmoidNormalize internally), so they're on a
		// comparable [0,1] scale.
		maps.Copy(merged, serverScores)
	}

	return merged
}
