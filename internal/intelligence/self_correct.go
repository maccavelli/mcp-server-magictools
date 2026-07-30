package intelligence

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

// ---------------------------------------------------------------------------
// Option 3: Intent-Keyed Outcome Tracking
// ---------------------------------------------------------------------------

// NormalizeIntent produces a stable hash-friendly string from a raw intent.
func NormalizeIntent(intent string) string {
	return strings.TrimSpace(strings.ToLower(intent))
}

// IntentToolHash computes the synergy key for an intent→tool outcome pair.
func IntentToolHash(intent, toolURN string) string {
	key := NormalizeIntent(intent) + ":" + toolURN
	return fmt.Sprintf("it:%x", sha256.Sum256([]byte(key)))
}

// RecordIntentOutcome writes a success/failure signal for an intent→tool pair
// to the BadgerDB synergy store. This is called in real-time during pipeline
// execution, not deferred to generate_audit_report.
func RecordIntentOutcome(store *db.Store, intent, toolURN string, success bool) {
	if store == nil {
		return
	}
	hash := IntentToolHash(intent, toolURN)
	store.RecordSynergy(hash, success)
	slog.Debug("self-correct: recorded intent outcome",
		"intent", intent[:min(40, len(intent))], "urn", toolURN, "success", success)
}

// GetIntentToolScore returns a Laplace-smoothed [0,1] confidence score
// for how well a tool has historically performed for a given intent class.
// Returns 0.0 if no data exists (neutral — no penalty, no boost).
func GetIntentToolScore(store *db.Store, intent, toolURN string) float64 {
	if store == nil {
		return 0.0
	}
	hash := IntentToolHash(intent, toolURN)
	successes, penalties := store.GetSynergy(hash)
	total := successes + penalties
	if total == 0 {
		return 0.0 // No data — neutral
	}
	return float64(successes) / float64(total+1) // Laplace smoothing
}

// ---------------------------------------------------------------------------
// Option 5: Pseudo-Relevance Feedback (PRF)
// ---------------------------------------------------------------------------

// stopWords contains ONLY common English stop words filtered during PRF term extraction.
// Domain-specific action verbs (search, execute, create, etc.) are intentionally EXCLUDED
// because they carry strong intent signal for tool discovery.
var stopWords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "is": true, "it": true, "that": true,
	"this": true, "as": true, "are": true, "was": true, "be": true, "has": true,
	"have": true, "had": true, "not": true, "will": true, "can": true, "do": true,
	"does": true, "did": true, "all": true, "each": true, "which": true, "when": true,
	"there": true, "their": true, "them": true, "then": true, "than": true,

	// Only truly generic terms that appear in nearly every tool description
	"tool": true, "tools": true, "used": true, "using": true,
	"data": true, "string": true, "name": true, "path": true,
}

// ExtractPRFTerms extracts discriminative terms from the top-K search results'
// descriptions and synthetic intents for query expansion. Returns the top-T
// terms sorted by frequency, excluding terms already in the original query.
func ExtractPRFTerms(topHits []*db.ToolRecord, originalQuery string, maxTerms int) []string {
	if len(topHits) == 0 {
		return nil
	}

	queryTokens := make(map[string]bool)
	for _, t := range tokenize(originalQuery) {
		queryTokens[t] = true
	}

	// Collect term frequencies from top results (cap at 3).
	termFreq := make(map[string]int)
	limit := min(3, len(topHits))
	for _, hit := range topHits[:limit] {
		// Extract from description.
		for _, t := range tokenize(hit.HighlightedDescription) {
			if !stopWords[t] && len(t) > 3 && !queryTokens[t] {
				termFreq[t]++
			}
		}
		// Extract from description (raw).
		for _, t := range tokenize(hit.Description) {
			if !stopWords[t] && len(t) > 3 && !queryTokens[t] {
				termFreq[t]++
			}
		}
		for _, t := range tokenize(strings.Join(hit.SyntheticIntents, " ")) {
			if !stopWords[t] && len(t) > 3 && !queryTokens[t] {
				termFreq[t]++
			}
		}
		for _, tok := range hit.LexicalTokens {
			t := strings.ToLower(tok)
			if !stopWords[t] && len(t) > 3 && !queryTokens[t] {
				termFreq[t]++
			}
		}
	}

	if len(termFreq) == 0 {
		return nil
	}

	// Sort by frequency descending.
	type termEntry struct {
		term string
		freq int
	}
	var entries []termEntry
	for t, f := range termFreq {
		entries = append(entries, termEntry{t, f})
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].freq > entries[j].freq
	})

	var result []string
	for i, e := range entries {
		if i >= maxTerms {
			break
		}
		result = append(result, e.term)
	}
	return result
}

// ComputeResultOverlap measures the overlap between two result sets by URN.
// Returns a value in [0,1] where 1.0 means identical result sets.
func ComputeResultOverlap(setA, setB []*db.ToolRecord) float64 {
	if len(setA) == 0 || len(setB) == 0 {
		return 0.0
	}
	urnSet := make(map[string]bool, len(setA))
	for _, r := range setA {
		urnSet[r.URN] = true
	}
	var overlap int
	for _, r := range setB {
		if urnSet[r.URN] {
			overlap++
		}
	}
	maxLen := max(len(setA), len(setB))
	if maxLen == 0 {
		return 0.0
	}
	return float64(overlap) / float64(maxLen)
}

// tokenize splits text into lowercase alpha tokens.
func tokenize(text string) []string {
	words := strings.FieldsFunc(strings.ToLower(text), func(c rune) bool {
		return (c < 'a' || c > 'z') && (c < '0' || c > '9')
	})
	return words
}

// ---------------------------------------------------------------------------
// Option 6: Contrastive Failure Anchors via HNSW
// ---------------------------------------------------------------------------

// FailureAnchorPrefix is the namespace prefix for failure anchors in the HNSW graph.
// Aliased from the vector package (the graph owns these keys) so reconciliation
// sweeps and anchor creation share a single source of truth.
const FailureAnchorPrefix = vector.FailureAnchorPrefix

// RecordFailureAnchor embeds a failure context into the HNSW vector graph as a
// negative anchor. Future searches that land near this anchor will penalize the
// associated tool. Gracefully no-ops when vector is offline.
func RecordFailureAnchor(ctx context.Context, toolURN, intent, errorClass string) {
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return // Graceful fallback: no vector = no anchors.
	}

	anchorText := fmt.Sprintf("%s %s %s", intent, errorClass, toolURN)
	anchorID := fmt.Sprintf("%s%s:%x", FailureAnchorPrefix, toolURN,
		sha256.Sum256([]byte(anchorText)))

	// Skip if already embedded (dedup).
	if e.HasDocument(anchorID) {
		return
	}

	if err := e.AddDocument(ctx, anchorID, anchorText); err != nil {
		slog.Warn("self-correct: failed to record failure anchor",
			"urn", toolURN, "error", err)
		return
	}
	slog.Info("self-correct: recorded failure anchor",
		"urn", toolURN, "error_class", errorClass, "anchor_id", anchorID)
}

// CheckFailureProximity checks if the given intent is semantically close to any
// recorded failure anchors for the specified tool. Returns a penalty multiplier
// in [0.5, 1.0] where 1.0 means no penalty. Gracefully returns 1.0 when
// vector is offline or anchors have been pruned.
func CheckFailureProximity(ctx context.Context, store *db.Store, intent, toolURN string) float64 {
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return 1.0 // Graceful fallback: no vector = no penalty.
	}

	// Skip if this tool's anchors have been pruned (recovery detected).
	if isToolPruned(store, toolURN) {
		return 1.0
	}

	// Search for intent proximity to failure anchors. SearchWithScores now hides
	// anchors from tool search, so query the dedicated anchor path here.
	results, err := e.SearchFailureAnchors(ctx, intent, 10)
	if err != nil {
		return 1.0
	}

	// Check if any results are failure anchors for this tool.
	prefix := FailureAnchorPrefix + toolURN + ":"
	for _, r := range results {
		if strings.HasPrefix(r.Key, prefix) && r.Score > 0.85 {
			slog.Info("self-correct: failure proximity detected",
				"urn", toolURN, "anchor", r.Key, "cosine", r.Score)
			// Proportional penalty: higher cosine = stronger penalty.
			// Score 0.85 → penalty 0.9, Score 1.0 → penalty 0.5.
			penalty := 1.0 - ((r.Score - 0.85) / 0.15 * 0.5)
			if penalty < 0.5 {
				penalty = 0.5
			}
			return penalty
		}
	}
	return 1.0 // No nearby failure anchors.
}

// CheckFailureProximityBatch embeds the query ONCE and returns per-tool penalty multipliers.
// This eliminates the N+1 embedding API calls that occurred when CheckFailureProximity
// was called in a loop for each search result.
func CheckFailureProximityBatch(ctx context.Context, store *db.Store, intent string, toolURNs []string) map[string]float64 {
	penalties := make(map[string]float64, len(toolURNs))
	for _, urn := range toolURNs {
		penalties[urn] = 1.0 // Default: no penalty
	}

	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return penalties
	}

	// Single embedding call for all tools (anchors only — see CheckFailureProximity).
	results, err := e.SearchFailureAnchors(ctx, intent, 10)
	if err != nil {
		return penalties
	}

	// Check each tool's failure anchors against the shared results
	for _, urn := range toolURNs {
		if isToolPruned(store, urn) {
			continue
		}
		prefix := FailureAnchorPrefix + urn + ":"
		for _, r := range results {
			if !strings.HasPrefix(r.Key, prefix) || r.Score <= 0.85 {
				continue
			}
			slog.Info("self-correct: failure proximity detected (batch)",
				"urn", urn, "anchor", r.Key, "cosine", r.Score)
			penalty := 1.0 - ((r.Score - 0.85) / 0.15 * 0.5)
			if penalty < 0.5 {
				penalty = 0.5
			}
			penalties[urn] = penalty
			break
		}
	}
	return penalties
}

// PruneFailureAnchors marks failure anchors as pruned for tools that have recovered
// (ProxyReliability exceeds the threshold). Since the HNSW library does not support
// node deletion, we record a "pruned" synergy entry that CheckFailureProximity
// consults to skip stale anchors. Gracefully no-ops when vector is offline.
func PruneFailureAnchors(store *db.Store, toolURN string, reliability float64) {
	if reliability < 1.1 || store == nil {
		return // Not yet recovered — threshold set to 1.1 to allow non-native tools to recover.
	}

	// Record a "pruned" marker in synergy DB for this tool.
	// CheckFailureProximity will check this marker before applying penalty.
	pruneKey := fmt.Sprintf("pruned:%s", toolURN)
	store.RecordSynergy(pruneKey, true)

	// Physically purge all failure anchor vectors for this URN from HNSW
	e := vector.GetEngine()
	if e != nil && e.VectorEnabled() {
		prefix := FailureAnchorPrefix + toolURN + ":"
		var anchorKeys []string
		for _, key := range e.Keys() {
			if strings.HasPrefix(key, prefix) {
				anchorKeys = append(anchorKeys, key)
			}
		}
		e.DeleteDocuments(anchorKeys...) // IDX-8: one tombstone+meta write for the sweep
	}

	slog.Info("self-correct: marked and purged failure anchors for recovered tool",
		"urn", toolURN, "reliability", reliability)
}

// isToolPruned checks if failure anchors for a tool have been pruned.
func isToolPruned(store *db.Store, toolURN string) bool {
	if store == nil {
		return false
	}
	pruneKey := fmt.Sprintf("pruned:%s", toolURN)
	successes, _ := store.GetSynergy(pruneKey)
	return successes > 0
}

// ClassifyError categorizes an error string into a structured error class.
func ClassifyError(errStr string) string {
	lower := strings.ToLower(errStr)
	switch {
	case strings.Contains(lower, "deadline exceeded") || strings.Contains(lower, "timeout"):
		return "TIMEOUT"
	case strings.Contains(lower, "does not match schema") || strings.Contains(lower, "missing properties"):
		return "SCHEMA_MISMATCH"
	case strings.Contains(lower, "too large") || strings.Contains(lower, "exceeds limit"):
		return "CONTEXT_OVERFLOW"
	case strings.Contains(lower, "connection refused") || strings.Contains(lower, "eof") || strings.Contains(lower, "broken pipe") || strings.Contains(lower, "epipe"):
		return errClassServerUnavailable
	default:
		return "TOOL_ERROR"
	}
}
