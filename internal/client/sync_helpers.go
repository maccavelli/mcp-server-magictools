// Package client provides functionality for the client subsystem.
package client

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// hashSchema returns a stable SHA256 hash of a tool's semantically meaningful fields.
// 🛡️ STABILITY: Only name, description, and inputSchema are hashed. Transient fields
// (_meta, annotations, outputSchema, title, icons) are excluded to prevent hash drift
// caused by SDK upgrades, struct tag changes, or server-side metadata mutations.
func (m *WarmRegistry) hashSchema(t *mcp.Tool) string {
	// Build a canonical struct with only the fields that define tool identity.
	canonical := struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		InputSchema any    `json:"inputSchema"`
	}{
		Name:        t.Name,
		Description: t.Description,
		InputSchema: t.InputSchema,
	}

	data, _ := json.Marshal(canonical) //nolint:errcheck // safe: canonical is always marshallable

	// Deep-sort string arrays (e.g., "required", "enum") for deterministic hashing
	// regardless of server-side array ordering.
	var raw any
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Sprintf("%x", sha256.Sum256(data))
	}
	deepSortStringArrays(raw)

	sortedData, err := json.Marshal(raw)
	if err != nil {
		return fmt.Sprintf("%x", sha256.Sum256(data))
	}
	return fmt.Sprintf("%x", sha256.Sum256(sortedData))
}

// deepSortStringArrays recursively walks a JSON structure and sorts any
// arrays that contain only string elements. This ensures deterministic
// hashing regardless of the order servers return "required" or "enum" arrays.
func deepSortStringArrays(v any) {
	switch v := v.(type) {
	case map[string]any:
		for _, val := range v {
			deepSortStringArrays(val)
		}
	case []any:
		allStrings := true
		strs := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				strs = append(strs, s)
			} else {
				allStrings = false
				break
			}
		}
		if allStrings && len(strs) > 0 {
			sort.Strings(strs)
			for i, s := range strs {
				v[i] = s
			}
		} else {
			for _, item := range v {
				deepSortStringArrays(item)
			}
		}
	}
}

// deriveCategory infers a tool category from the server name.
func (m *WarmRegistry) deriveCategory(server string) string {
	s := strings.ToLower(server)
	switch {
	case strings.Contains(s, serverRecall) || strings.Contains(s, categoryMemory):
		return categoryMemory
	case strings.Contains(s, "git") || strings.Contains(s, "github") || strings.Contains(s, "gitlab") || strings.Contains(s, "glab"):
		return categoryDevops
	case strings.Contains(s, "file") || strings.Contains(s, "fs"):
		return categoryFilesystem
	case strings.Contains(s, categorySearch) || strings.Contains(s, "duckduckgo"):
		return categorySearch
	case strings.Contains(s, categorySystem) || strings.Contains(s, "shell") || strings.Contains(s, "bash") || strings.Contains(s, "exec"):
		return categorySystem
	case strings.Contains(s, "seq-thinking") || strings.Contains(s, "sequential"):
		return categoryAgent
	case strings.Contains(s, "modernizer") || strings.Contains(s, categoryRefactor):
		return categoryRefactor
	case s == serverMagictools:
		return categoryCore
	default:
		return categoryPlugin
	}
}

// toSchemaMap converts a tool's input schema to a map[string]interface{}.
func (m *WarmRegistry) toSchemaMap(schema any) map[string]any {
	if schema == nil {
		return nil
	}
	if res, ok := schema.(map[string]any); ok {
		return res
	}
	data, _ := json.Marshal(schema) //nolint:errcheck // safe: schema roundtrip
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		slog.Debug("sync: failed to unmarshal JSON for summarization", "error", err)
	}
	return out
}

// minifyDescription truncates long tool descriptions for IDE token efficiency.
func (m *WarmRegistry) minifyDescription(desc string) string {
	if m.Config.NoOptimize || len(desc) < 200 {
		return desc
	}
	sentences := strings.Split(desc, ". ")
	if len(sentences) > 2 {
		return strings.Join(sentences[:2], ". ") + "..."
	}
	return desc[:197] + "..."
}

// synonyms maps common search terms to semantically related alternatives.
// Applied at index time to expand the intent field for richer BM25 matching.
var synonyms = map[string][]string{
	"think":          {"reason", "analyze", "problem-solve", "deliberate", "reflect", "reasoning"},
	"thinking":       {"reasoning", "analysis", "deliberation", "reflection", "chain-of-thought"},
	categorySearch:   {"find", "lookup", synonymQuery, "research", "discover"},
	"log":            {"diagnostic", "debug", "audit", "trace", "monitor"},
	categoryRefactor: {"modernize", "clean", "improve", "restructure", "optimize"},
	"file":           {"directory", synonymPath, categoryFilesystem, "disk", "folder"},
	categoryMemory:   {"recall", "remember", "context", "history", "persist"},
	"git":            {"commit", "branch", "merge", "push", "pull", "version-control"},
	"deploy":         {"release", "ci", "cd", "pipeline", "build"},
	"design":         {"architecture", "plan", "blueprint", "structure", "layout"},
	"skill":          {"workflow", "bootstrap", "automation", "task", "goal"},
	"sequential":     {"step-by-step", "chain", "multi-step", "ordered", "progression"},
	"brainstorm":     {"critique", "review", "evaluate", "assess", "ideate"},
	"web":            {"internet", "online", "http", "url", "website"},
	synonymRecall:    {"remember", "retrieve", "context", categoryMemory, "history"},
}

// parseAnnotations extracts structured ALIASES:, USE_WHEN:, and CASCADES: blocks
// from tool descriptions. Returns the parsed terms and the description without annotations.
func parseAnnotations(desc string) (aliases, useWhen, cascades []string) {
	lines := strings.SplitSeq(desc, "\n")
	for line := range lines {
		line = strings.TrimSpace(line)
		upper := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(upper, "ALIASES:"):
			aliases = splitAnnotation(line[len("ALIASES:"):])
		case strings.HasPrefix(upper, "ALSO KNOWN AS:"):
			aliases = splitAnnotation(line[len("ALSO KNOWN AS:"):])
		case strings.HasPrefix(upper, "USE_WHEN:"):
			useWhen = splitAnnotation(line[len("USE_WHEN:"):])
		case strings.HasPrefix(upper, "USE WHEN:"):
			useWhen = splitAnnotation(line[len("USE WHEN:"):])
		case strings.HasPrefix(upper, "CASCADES:"):
			cascades = splitAnnotation(line[len("CASCADES:"):])
		}
	}
	return aliases, useWhen, cascades
}

// splitAnnotation splits a comma-separated annotation value into trimmed terms.
func splitAnnotation(val string) []string {
	val = strings.TrimSpace(val)
	if val == "" {
		return nil
	}
	parts := strings.Split(val, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, strings.ToLower(p))
		}
	}
	return result
}

// expandSynonyms returns additional terms for any word that has synonyms defined.
func expandSynonyms(words []string) []string {
	var expanded []string
	for _, w := range words {
		if syns, ok := synonyms[w]; ok {
			expanded = append(expanded, syns...)
		}
	}
	return expanded
}

// extractIntent builds a search-optimized intent string from a tool's name and description.
func (m *WarmRegistry) extractIntent(name, desc string) string {
	unique := make(map[string]bool)
	var intent []string

	addUnique := func(terms []string) {
		for _, t := range terms {
			t = strings.ToLower(strings.Trim(t, ".,!?:;()[]\""))
			if t != "" && !unique[t] {
				unique[t] = true
				intent = append(intent, t)
			}
		}
	}

	// Phase 1: Parse structured annotations from description
	aliases, useWhen, _ := parseAnnotations(desc)
	addUnique(aliases)
	addUnique(useWhen)

	// Phase 2: Extract keywords from name + description
	stopWords := map[string]bool{
		"and": true, "the": true, "for": true, "with": true, "this": true,
		"tool": true, "that": true, "from": true, "into": true, "your": true,
		"will": true, "also": true, "been": true, "have": true, "when": true,
		"each": true, "more": true, "uses": true, "used": true,
	}
	words := strings.Fields(strings.ToLower(name + " " + desc))
	var keywords []string
	for _, w := range words {
		w = strings.Trim(w, ".,!?:;()[]\"")
		if strings.HasPrefix(strings.ToUpper(w), "ALIASES:") || strings.HasPrefix(strings.ToUpper(w), "USE_WHEN:") || strings.HasPrefix(strings.ToUpper(w), "CASCADES:") {
			continue
		}
		if len(w) > 3 && !stopWords[w] && !unique[w] {
			unique[w] = true
			keywords = append(keywords, w)
			intent = append(intent, w)
			if len(keywords) > 15 {
				break
			}
		}
	}

	// Phase 3: Synonym expansion on all collected terms
	allTerms := make([]string, len(intent))
	copy(allTerms, intent)
	expanded := expandSynonyms(allTerms)
	addUnique(expanded)

	return strings.Join(intent, " ")
}

// markOffline sets a server's status to offline and opens its circuit breaker.
func (m *WarmRegistry) markOffline(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.Servers[name]; ok {
		s.Status = StatusOffline
		s.ConsecutiveErrors = CircuitBreakerThreshold
		s.LastFailure = time.Now()
	}
}

// logSyncError captures sync failure details with a stderr tail to aid debugging.
func (m *WarmRegistry) logSyncError(server string, err error) {
	id := fmt.Sprintf("sync_error:%d:%s", time.Now().Unix(), server)
	var tail string
	logPath := m.Config.LogPath
	if logPath == "" {
		logPath = config.DefaultLogPath()
	}
	if f, oerr := os.Open(filepath.Clean(logPath)); oerr == nil { //nolint:gosec // log path from controlled config directory
		defer func() {
			if closeErr := f.Close(); closeErr != nil {
				slog.Debug("sync: log tail close failed", "error", closeErr)
			}
		}()
		if fi, statErr := f.Stat(); statErr == nil {
			offset := max(fi.Size()-4096, 0)
			if _, seekErr := f.Seek(offset, io.SeekStart); seekErr != nil {
				slog.Debug("sync: seek failed in file reader", "error", seekErr)
			} else {
				data := make([]byte, 4096)
				n, readErr := f.Read(data)
				if readErr != nil && readErr != io.EOF {
					slog.Debug("sync: log tail read failed", "error", readErr)
				} else if n > 0 {
					lines := strings.Split(string(data[:n]), "\n")
					if len(lines) > 20 {
						lines = lines[len(lines)-21:]
					}
					tail = "\n[STDERR TAIL]\n" + strings.Join(lines, "\n")
				}
			}
		}
	}

	msg := fmt.Sprintf("[%s] Sync Failure: %v%s", time.Now().Format(time.RFC3339), err, tail)
	if err := m.Store.SaveRawResource(id, []byte(msg)); err != nil {
		slog.Warn("sync: failed to save raw resource for streaming", "id", id, "error", err)
	}
}

// ─── Semantic Hydrator: Data Population Functions ──────────────────────────

// hydratorVersion is the current version stamp for hydrated intelligence.
// Bump this value to force re-hydration of all tools after logic changes.
const hydratorVersion = "hydrated:v3"

// computeWBase calculates the baseline Trust Factor for a tool using structural
// analysis signals rather than keyword-matching heuristics.
//
// Formula: W_base = 0.7 + specificity(0.15) + exclusivity(0.10) + complexity(0.10) + categoryWeight
//
// Signals:
//   - specificity: ratio of required parameters to total (more required = more deterministic)
//   - exclusivity: normalized count of negative triggers (more distinct = better routing)
//   - complexity:  total parameter count normalized (more params = more specific interface)
//   - categoryWeight: configurable per-category offset
//
// Output is clamped to [0.5, 1.3].
func computeWBase(_, category string, schema map[string]any, negativeTriggers []string) float64 {
	// ─── Signal 1: Specificity (required/total parameter ratio) ─────────
	var specificity float64
	var totalParams, requiredParams int

	if schema != nil {
		if props, ok := schema["properties"].(map[string]any); ok {
			totalParams = len(props)
		}
		switch req := schema["required"].(type) {
		case []any:
			requiredParams = len(req)
		case []string:
			requiredParams = len(req)
		}
	}

	if totalParams > 0 {
		specificity = float64(requiredParams) / float64(totalParams)
	} else {
		// Tools with no parameters are often action-only (high specificity)
		specificity = 0.5
	}

	// ─── Signal 2: Exclusivity (negative trigger diversity) ─────────────
	var exclusivity float64
	triggerCount := len(negativeTriggers)
	switch {
	case triggerCount >= 6:
		exclusivity = 1.0
	case triggerCount >= 4:
		exclusivity = 0.7
	case triggerCount >= 2:
		exclusivity = 0.4
	default:
		exclusivity = 0.1
	}

	// ─── Signal 3: Schema Complexity (normalized param count) ───────────
	var complexity float64
	switch {
	case totalParams >= 6:
		complexity = 1.0
	case totalParams >= 4:
		complexity = 0.8
	case totalParams >= 2:
		complexity = 0.5
	case totalParams == 1:
		complexity = 0.3
	default:
		complexity = 0.1
	}

	// ─── Signal 4: Category Weight ─────────────────────────────────────
	var categoryWeight float64
	switch strings.ToLower(category) {
	case categoryCore, categoryOrchestrator:
		categoryWeight = 0.05
	case categoryAgent:
		categoryWeight = 0.03
	case categoryMemory:
		categoryWeight = 0.02
	case categoryDevops, categoryFilesystem, categorySearch, categoryRefactor:
		categoryWeight = 0.0
	case categoryPlugin:
		categoryWeight = -0.03
	}

	wBase := 0.7 + (specificity * 0.15) + (exclusivity * 0.10) + (complexity * 0.10) + categoryWeight

	// Clamp output to [0.5, 1.3]
	if wBase < 0.5 {
		wBase = 0.5
	}
	if wBase > 1.3 {
		wBase = 1.3
	}

	return wBase
}

// generateSyntheticIntents produces 14 deterministic natural language phrases
// that a user might say to trigger this tool through a proxy. Uses template
// expansion from the tool name, description keywords, and category.
func generateSyntheticIntents(name, description, category string) []string {
	// Extract primary verb and noun from the tool name
	nameParts := strings.Split(strings.ReplaceAll(name, "-", "_"), "_")
	var verb, noun string
	if len(nameParts) >= 2 {
		verb = nameParts[0]
		noun = strings.Join(nameParts[1:], " ")
	} else if len(nameParts) == 1 {
		verb = "use"
		noun = nameParts[0]
	}

	// Extract 3 key description words for enrichment
	descWords := strings.Fields(strings.ToLower(description))
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "this": true, "that": true,
		"with": true, "from": true, "into": true, "tool": true, "your": true,
		"will": true, "also": true, "must": true, "been": true, "have": true,
	}
	var keyWords []string
	for _, w := range descWords {
		w = strings.Trim(w, ".,!?:;()[]\"")
		if len(w) > 4 && !stopWords[w] && len(keyWords) < 3 {
			keyWords = append(keyWords, w)
		}
	}
	if len(keyWords) == 0 {
		keyWords = []string{noun}
	}

	intents := []string{
		fmt.Sprintf("I need to %s %s", verb, noun),
		fmt.Sprintf("How do I %s %s", verb, noun),
		fmt.Sprintf("%s my %s", verb, noun),
		fmt.Sprintf("run %s", name),
		fmt.Sprintf("execute %s", name),
		fmt.Sprintf("use %s tool", name),
		fmt.Sprintf("help me %s", noun),
		fmt.Sprintf("I want to %s something", verb),
		fmt.Sprintf("can you %s the %s", verb, noun),
		fmt.Sprintf("please %s %s for me", verb, noun),
		fmt.Sprintf("%s %s in my project", verb, noun),
		fmt.Sprintf("analyze %s using %s", keyWords[0], category),
		fmt.Sprintf("perform %s analysis", noun),
		fmt.Sprintf("start %s workflow", noun),
	}

	return intents
}

// generateLexicalTokens extracts targeted technical keywords from a tool's
// name and input schema parameter names that specifically signal relevance.
func generateLexicalTokens(name string, schema map[string]any) []string {
	unique := make(map[string]bool)
	var tokens []string

	add := func(t string) {
		t = strings.ToLower(strings.Trim(t, ".,!?:;()[]\""))
		if t != "" && !unique[t] {
			unique[t] = true
			tokens = append(tokens, t)
		}
	}

	// Split tool name into tokens
	for part := range strings.SplitSeq(strings.ReplaceAll(name, "-", "_"), "_") {
		add(part)
	}

	// Extract parameter names from inputSchema.properties
	if schema != nil {
		if props, ok := schema["properties"].(map[string]any); ok {
			for paramName := range props {
				add(paramName)
				// Also split camelCase/snake_case parameter names
				for sub := range strings.SplitSeq(paramName, "_") {
					add(sub)
				}
			}
		}
	}

	return tokens
}

// crossCategoryExclusions maps each category to phrases that should NOT trigger its tools.
var crossCategoryExclusions = map[string][]string{
	categoryDevops: {
		phraseRefactorCode, "analyze complexity", "brainstorm design",
		phraseSearchTheWeb, phraseRecallMemory, "sequential thinking",
	},
	categoryPlugin: {
		phraseGitCommit, phraseSearchTheWeb, phraseReadFile,
		"list directory", "web search", "deploy application",
	},
	categoryAgent: {
		"git push", "deploy server", "read filesystem",
		"search duckduckgo", "commit changes", "list files",
	},
	categoryMemory: {
		"refactor package", "search internet", "git merge",
		"analyze AST", "brainstorm feature", "file operations",
	},
	categorySearch: {
		phraseGitCommit, phraseRefactorCode, phraseRecallMemory,
		"analyze complexity", phraseReadFile, "deploy pipeline",
	},
	categoryFilesystem: {
		"git push", phraseSearchWeb, "recall context",
		"refactor module", "brainstorm idea", "deploy release",
	},
	categoryOrchestrator: {
		phraseRefactorCode, phraseSearchWeb, phraseGitCommit,
		phraseReadFile, phraseRecallMemory, "brainstorm design",
	},
	categoryRefactor: {
		phraseGitCommit, phraseSearchWeb, phraseRecallMemory,
		phraseReadFile, "list directory", "deploy pipeline",
	},
}

// generateNegativeTriggers returns 6 phrases that sound similar but should
// never trigger this specific tool, based on cross-category exclusion mappings.
func generateNegativeTriggers(_, category string) []string {
	cat := strings.ToLower(category)
	if exclusions, ok := crossCategoryExclusions[cat]; ok {
		return exclusions
	}

	// Default fallback for unknown categories
	return []string{
		"unrelated operation",
		"different domain",
		"wrong tool category",
		"incorrect action type",
		"mismatched function",
		"off-topic request",
	}
}
