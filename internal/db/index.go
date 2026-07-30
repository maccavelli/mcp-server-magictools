package db

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sync"
	"sync/atomic"

	"github.com/blevesearch/bleve/v2"
	"github.com/blevesearch/bleve/v2/analysis"
	"github.com/blevesearch/bleve/v2/analysis/token/camelcase"
	"github.com/blevesearch/bleve/v2/analysis/token/edgengram"
	"github.com/blevesearch/bleve/v2/analysis/token/lowercase"
	"github.com/blevesearch/bleve/v2/analysis/tokenizer/unicode"
	"github.com/blevesearch/bleve/v2/registry"
	"github.com/blevesearch/bleve/v2/search/query"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

const edgeNgramAnalyzerName = "name_edge_ngram"

func recordSchemaMutation() {
	telemetry.SearchMetrics.SchemaMutations.Add(1)
}

var roleTagStripperIndex = regexp.MustCompile(`(?i)\[.*?\]\s*`)

func init() {
	// BLV-7: the standalone "camel_split" analyzer was registered but never attached
	// to any field — the edge-ngram analyzer below already includes the camelCase
	// split filter, so "sequentialthinking" → "sequential"/"thinking" is served there.
	registry.RegisterAnalyzer(edgeNgramAnalyzerName, func(config map[string]any, cache *registry.Cache) (analysis.Analyzer, error) {
		tokenizer, err := cache.TokenizerNamed(unicode.Name)
		if err != nil {
			return nil, err
		}
		camelFilter, err := cache.TokenFilterNamed(camelcase.Name)
		if err != nil {
			return nil, err
		}
		lcFilter, err := cache.TokenFilterNamed(lowercase.Name)
		if err != nil {
			return nil, err
		}
		ngramFilter := edgengram.NewEdgeNgramFilter(edgengram.FRONT, 3, 14)
		return &analysis.DefaultAnalyzer{
			Tokenizer:    tokenizer,
			TokenFilters: []analysis.TokenFilter{camelFilter, lcFilter, ngramFilter},
		}, nil
	})
}

// BleveToolDocument is an adapter struct that provides proper JSON tags for Bleve
// indexing. ToolRecord's intelligence fields (SyntheticIntents, LexicalTokens, etc.)
// use json:"-" tags to exclude them from BadgerDB serialization, which also makes them
// invisible to Bleve's reflection-based field discovery. This adapter bridges the gap.
type BleveToolDocument struct {
	TypeField        string   `json:"_type"`
	URN              string   `json:"urn"`
	Name             string   `json:"name"`
	Server           string   `json:"server"`
	Description      string   `json:"description"`
	LiteSummary      string   `json:"lite_summary"`
	Category         string   `json:"category"`
	Intent           string   `json:"intent"`
	SyntheticIntents string   `json:"synthetic_intents"`
	LexicalTokens    []string `json:"lexical_tokens"`
	NegativeTriggers []string `json:"negative_triggers"`
	UsageCount       int64    `json:"usage_count"`
	ProxyReliability float64  `json:"proxy_reliability"`
	EnumValues       []string `json:"enum_values"`
	Role             string   `json:"role"`

	// DAG discovery fields wired into the lexical query (buildDAGDiscoveryQueries).
	Requires []string `json:"requires"`
	Triggers []string `json:"triggers"`

	// Materialized parameter names + exact-lookup keyword copies.
	ParameterNames []string `json:"parameter_names"`
	QualifiedName  string   `json:"qualified_name"` // server:tool_name keyword for exact URN-style lookup
	NameExact      string   `json:"name_exact"`     // BLV-5: lowercased keyword copy of Name for exact/wildcard name matching

	// BLV-8: phase, is_native, input_contract, output_contract, last_used_at,
	// depends_on, total_calls, failure_rate, avg_latency_ms were indexed but never
	// queried/sorted/faceted/retrieved on the lexical leg — dead index bloat. They
	// remain on ToolRecord (the BadgerDB source of truth) and are read from there.
}

// Type natively binds this schema directly to the specific "tool" document mapping in Bleve
func (d BleveToolDocument) Type() string {
	return "tool"
}

// ToBleveDoc converts a ToolRecord (with overlaid intelligence) to a BleveToolDocument.
func ToBleveDoc(r *ToolRecord) BleveToolDocument {
	cleanDesc := roleTagStripperIndex.ReplaceAllString(r.Description, "")
	return BleveToolDocument{
		TypeField:        "tool",
		URN:              r.URN,
		Name:             r.Name,
		NameExact:        strings.ToLower(r.Name),
		Server:           r.Server,
		QualifiedName:    r.Server + ":" + r.Name,
		Description:      cleanDesc,
		LiteSummary:      r.LiteSummary,
		Category:         strings.ToLower(r.Category),
		Intent:           r.Intent,
		SyntheticIntents: strings.Join(r.SyntheticIntents, " "),
		LexicalTokens:    r.LexicalTokens,
		NegativeTriggers: r.NegativeTriggers,
		UsageCount:       r.UsageCount,
		ProxyReliability: r.Metrics.ProxyReliability,
		EnumValues:       r.EnumValues,
		Role:             r.Role,

		// DAG discovery fields wired into the lexical query.
		Requires: r.Requires,
		Triggers: r.Triggers,

		ParameterNames: r.ParameterNames,
	}
}

// SyntheticIntent represents a successfully validated Prompt-to-DAG vector pairing organically tracked locally.
type SyntheticIntent struct {
	TypeField string   `json:"_type"`
	ID        string   `json:"id"`
	Prompt    string   `json:"prompt"`
	DAG       []string `json:"dag"`
	Timestamp int64    `json:"timestamp"` // Unix timestamp for confidence decay
}

func (s SyntheticIntent) Type() string {
	return "intent_cache"
}

// HFSCTraceDocument represents a single Socratic LLM execution natively indexed for historical auditing.
type HFSCTraceDocument struct {
	TypeField  string `json:"_type"`
	ID         string `json:"id"`
	Server     string `json:"server"`
	Tier       string `json:"tier"`
	Model      string `json:"model"`
	LatencyMs  int64  `json:"latency_ms"`
	Tokens     int    `json:"tokens"`
	ReceivedAt int64  `json:"received_at"` // Unix timestamp
}

func (h HFSCTraceDocument) Type() string {
	return "hfsc_trace"
}

// SearchIndex manages the Bleve index
type SearchIndex struct {
	index atomic.Value // holds bleve.Index
	mu    sync.RWMutex
}

// GetUnderlyingIndex returns the underlying bleve.Index
func (si *SearchIndex) GetUnderlyingIndex() bleve.Index {
	if v := si.index.Load(); v != nil {
		return v.(bleve.Index)
	}
	return nil
}

// NewSearchIndex creates an in-memory fast-path index shadowing BadgerDB.
func NewSearchIndex(dbPath string) (*SearchIndex, error) {
	// Define Mapping
	indexMapping := bleve.NewIndexMapping()
	// BLV-4: pin BM25 explicitly. The default scorer is tf-idf, yet the code,
	// telemetry, and NormalizeBM25 (atan squash) all assume a BM25 distribution.
	// Making it explicit means the gate floor and base factor are tuned against the
	// scorer they name. The schema-version bump below forces a one-time rebuild.
	// Value matches bleve_index_api.BM25Scoring; using the literal avoids promoting
	// that transitive package to a direct dependency.
	indexMapping.ScoringModel = "bm25"

	// Map Tool Fields
	toolMapping := bleve.NewDocumentMapping()

	// - URN: Keyword (Exact match, no tokenization)
	urnFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("urn", urnFieldMapping)

	// - Name: Text with edge ngram analyzer for partial matches
	nameFieldMapping := bleve.NewTextFieldMapping()
	nameFieldMapping.Analyzer = edgeNgramAnalyzerName
	toolMapping.AddFieldMappingsAt("name", nameFieldMapping)

	// - Server: Keyword (Exact match for telemetry filtering)
	serverFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("server", serverFieldMapping)

	// - QualifiedName: Keyword (server:tool_name exact match)
	qualifiedNameFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("qualified_name", qualifiedNameFieldMapping)

	// - NameExact: Keyword (BLV-5) — a non-ngram copy of the name so the high-boost
	// exact/wildcard name legs actually match (the "name" field is edge-ngrammed).
	nameExactFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("name_exact", nameExactFieldMapping)

	// - Description: Text (BM25 Full-text)
	descFieldMapping := bleve.NewTextFieldMapping()
	toolMapping.AddFieldMappingsAt("description", descFieldMapping)

	// - LiteSummary: Text (BM25 condensed description)
	liteSummaryFieldMapping := bleve.NewTextFieldMapping()
	liteSummaryFieldMapping.Analyzer = "en"
	toolMapping.AddFieldMappingsAt("lite_summary", liteSummaryFieldMapping)

	// - Category: Keyword for exact category filter conjunction
	catFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("category", catFieldMapping)

	// - EnumValues: Keyword array for exact match deep schema constraints ("CRITICAL")
	enumValuesFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("enum_values", enumValuesFieldMapping)

	// - Intent: Text field for conceptual matching (semantic substitute)
	intentFieldMapping := bleve.NewTextFieldMapping()
	intentFieldMapping.Analyzer = "en"
	toolMapping.AddFieldMappingsAt("intent", intentFieldMapping)

	// - SyntheticIntents: Text field for LLM generated intents
	syntheticIntentsFieldMapping := bleve.NewTextFieldMapping()
	syntheticIntentsFieldMapping.Analyzer = "en"
	toolMapping.AddFieldMappingsAt("synthetic_intents", syntheticIntentsFieldMapping)

	// - LexicalTokens: Keyword array for targeted terminology
	lexicalTokensFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("lexical_tokens", lexicalTokensFieldMapping)

	// - UsageCount: Numeric for scoring boost
	usageFieldMapping := bleve.NewNumericFieldMapping()
	toolMapping.AddFieldMappingsAt("usage_count", usageFieldMapping)

	// - ProxyReliability: Numeric for empirical trust boost natively mapping Handler telemetry to Index Math
	reliabilityFieldMapping := bleve.NewNumericFieldMapping()
	toolMapping.AddFieldMappingsAt("proxy_reliability", reliabilityFieldMapping)

	// - Role: Keyword (Exact match for pipeline DAG filtering — ANALYZER, MUTATOR, CRITIC, etc.)
	roleFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("role", roleFieldMapping)

	// - NegativeTriggers: Keyword array for anti-intent exclusion filtering
	negativeTriggersFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("negative_triggers", negativeTriggersFieldMapping)

	// ═══ DAG discovery fields (queried by buildDAGDiscoveryQueries / param matching) ═══

	// - Requires: Keyword array for dependency graph queries ("find tools that require discover_project")
	requiresFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("requires", requiresFieldMapping)

	// - Triggers: Keyword array for forward edge queries
	triggersFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("triggers", triggersFieldMapping)

	// - ParameterNames: Keyword array for schema-aware parameter matching ("tools with session_id")
	parameterNamesFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("parameter_names", parameterNamesFieldMapping)

	// _type: Keyword field for parity diagnostics — must be explicitly mapped
	// so TermQuery("tool") on "_type" returns accurate counts.
	typeFieldMapping := bleve.NewKeywordFieldMapping()
	toolMapping.AddFieldMappingsAt("_type", typeFieldMapping)

	// BLV-8: make the tool mapping authoritative. Every indexed tool field above is
	// explicitly mapped, so disabling dynamic indexing changes nothing today — but it
	// stops a future BleveToolDocument field from being silently indexed (and bloating
	// every segment) without a deliberate mapping + schema bump. The intent_cache and
	// hfsc_trace mappings deliberately stay dynamic (they index unmapped fields like
	// received_at, which pruneSyntheticDocs sorts on).
	toolMapping.Dynamic = false

	indexMapping.AddDocumentMapping("tool", toolMapping)

	// 🛡️ INTENT CACHE MAPPING
	intentCacheMapping := bleve.NewDocumentMapping()
	intentCacheMapping.AddFieldMappingsAt("_type", typeFieldMapping)
	promptFieldMapping := bleve.NewTextFieldMapping()
	promptFieldMapping.Analyzer = "en"
	intentCacheMapping.AddFieldMappingsAt("prompt", promptFieldMapping)

	dagFieldMapping := bleve.NewKeywordFieldMapping()
	intentCacheMapping.AddFieldMappingsAt("dag", dagFieldMapping)

	indexMapping.AddDocumentMapping("intent_cache", intentCacheMapping)

	// 🛡️ HFSC TRACE MAPPING
	traceMapping := bleve.NewDocumentMapping()
	traceMapping.AddFieldMappingsAt("_type", typeFieldMapping)

	serverFieldMappingTrace := bleve.NewKeywordFieldMapping()
	traceMapping.AddFieldMappingsAt("server", serverFieldMappingTrace)

	tierFieldMapping := bleve.NewKeywordFieldMapping()
	traceMapping.AddFieldMappingsAt("tier", tierFieldMapping)

	modelFieldMapping := bleve.NewKeywordFieldMapping()
	traceMapping.AddFieldMappingsAt("model", modelFieldMapping)

	indexMapping.AddDocumentMapping("hfsc_trace", traceMapping)

	const bleveSchemaVersion = "v7" // Bump when mapping changes require rebuild (v7: BLV-8 pruned 9 unread tool fields + tool mapping non-dynamic)

	// 🛡️ PERSIST-01: Use persistent disk-backed index when dbPath is provided.
	// Falls back to in-memory for tests or when no path is given.
	blevePath := ""
	if dbPath != "" {
		blevePath = filepath.Join(dbPath, "bleve_index")
	}

	var err error
	var index bleve.Index
	if blevePath == "" {
		// In-memory fallback (tests, ephemeral mode)
		index, err = bleve.NewMemOnly(indexMapping)
		if err != nil {
			return nil, fmt.Errorf("failed to create memory bleve index: %w", err)
		}
		slog.Info("search index: created in-memory (no dbPath provided)", "component", "index")
	} else if _, statErr := os.Stat(blevePath); os.IsNotExist(statErr) {
		// First boot — create new persistent index
		index, err = bleve.New(blevePath, indexMapping)
		if err != nil {
			slog.Warn("search index: failed to create persistent index, falling back to memory",
				"component", "index", "path", blevePath, "error", err)
			index, err = bleve.NewMemOnly(indexMapping)
			if err != nil {
				return nil, fmt.Errorf("failed to create fallback memory bleve index: %w", err)
			}
		} else {
			slog.Info("search index: created NEW persistent index", "component", "index", "path", blevePath)
			_ = os.WriteFile(filepath.Join(blevePath, ".schema_version"), []byte(bleveSchemaVersion), 0644)
			recordSchemaMutation()
		}
	} else {
		// Existing index — check schema version
		versionFile := filepath.Join(blevePath, ".schema_version")
		needsRebuild := false
		if vb, err := os.ReadFile(versionFile); err != nil || string(vb) != bleveSchemaVersion {
			needsRebuild = true
		}

		if needsRebuild {
			slog.Warn("search index: schema version mismatch, rebuilding", "component", "index", "path", blevePath)
			_ = os.RemoveAll(blevePath)
			index, err = bleve.New(blevePath, indexMapping)
			if err != nil {
				return nil, fmt.Errorf("failed to recreate bleve index after schema upgrade: %w", err)
			}
			_ = os.WriteFile(versionFile, []byte(bleveSchemaVersion), 0644)
			slog.Info("search index: rebuilt with new schema", "component", "index", "version", bleveSchemaVersion)
			recordSchemaMutation()
		} else {
			index, err = bleve.Open(blevePath)
			if err != nil {
				slog.Warn("search index: failed to open existing index, rebuilding",
					"component", "index", "path", blevePath, "error", err)
				_ = os.RemoveAll(blevePath)
				index, err = bleve.New(blevePath, indexMapping)
				if err != nil {
					return nil, fmt.Errorf("failed to recreate bleve index: %w", err)
				}
				_ = os.WriteFile(versionFile, []byte(bleveSchemaVersion), 0644)
				slog.Info("search index: rebuilt persistent index after corruption", "component", "index", "path", blevePath)
				recordSchemaMutation()
			} else {
				slog.Info("search index: opened existing persistent index", "component", "index", "path", blevePath)
			}
		}
	}

	si := &SearchIndex{}
	si.index.Store(index)
	return si, nil
}

// Search performs a multi-stage ranked search with usage-weighted scoring.
func (si *SearchIndex) Search(ctx context.Context, queryStr string, category string, serverConstraint string, domain SearchDomain) (*bleve.SearchResult, error) {
	si.mu.RLock()
	defer si.mu.RUnlock()

	finalQuery := assembleCoreSearchQuery(queryStr)
	magictoolsBoost := bleve.NewTermQuery("magictools")
	magictoolsBoost.SetField("server")
	magictoolsBoost.SetBoost(1.25)

	// 🚀 Proxy Reliability Skew: boost genuinely above-baseline tools. BLV-3:
	// ProxyReliability is centered at 1.0 (range [0.5,2.0], default 1.0), so the old
	// >=0.90 floor matched essentially every tool and gave no discrimination. Use a
	// floor meaningfully above baseline, matching the post-fusion `>1.0` treatment.
	highTrustMin := 1.3
	highTrustQuery := bleve.NewNumericRangeQuery(&highTrustMin, nil)
	highTrustQuery.SetField("proxy_reliability")
	highTrustQuery.SetBoost(2.5)

	// 🚀 Empirical Usage Tracking Skew: Value experience organically
	highUsageMin := 50.0
	highUsageQuery := bleve.NewNumericRangeQuery(&highUsageMin, nil)
	highUsageQuery.SetField("usage_count")
	highUsageQuery.SetBoost(2.0)

	boostWrapper := bleve.NewBooleanQuery()
	boostWrapper.AddMust(finalQuery)
	boostWrapper.AddShould(magictoolsBoost)
	boostWrapper.AddShould(highTrustQuery)
	boostWrapper.AddShould(highUsageQuery)
	finalQuery = boostWrapper

	// 🛡️ DOMAIN-AWARE SHARDING: Enforce strict visibility boundaries
	switch domain {
	case DomainUserLand:
		var mustNot []query.Query
		for _, srv := range []string{serverBrainstorm, serverGoModernizer} {
			if !IsServerVisibleInDomain(srv, queryStr, serverConstraint, DomainUserLand) {
				q := bleve.NewTermQuery(srv)
				q.SetField("server")
				mustNot = append(mustNot, q)
			}
		}
		if len(mustNot) > 0 {
			bq := bleve.NewBooleanQuery()
			bq.AddMust(finalQuery)
			bq.AddMustNot(mustNot...)
			finalQuery = bq
		}
	case DomainPipelineOrchestration:
		bq := bleve.NewBooleanQuery()
		bq.AddMust(finalQuery)

		var allowed []query.Query
		for _, srv := range []string{serverMagictools, serverBrainstorm, serverGoModernizer} {
			if IsServerVisibleInDomain(srv, queryStr, serverConstraint, DomainPipelineOrchestration) {
				q := bleve.NewTermQuery(srv)
				q.SetField("server")
				allowed = append(allowed, q)
			}
		}
		if len(allowed) > 0 {
			dis := bleve.NewDisjunctionQuery(allowed...)
			bq.AddMust(dis)
		}
		finalQuery = bq
	case DomainSystem:
		// No sharding, show all tools natively
	}

	// 5. HIERARCHICAL ROUTING: Strict category filter (keyword field — exact match)
	if category != "" {
		catQuery := bleve.NewTermQuery(strings.ToLower(category))
		catQuery.SetField("category")
		finalQuery = bleve.NewConjunctionQuery(finalQuery, catQuery)
	}

	if serverConstraint != "" {
		// Enforce pure node orchestration functionally
		serverQuery := bleve.NewTermQuery(serverConstraint)
		serverQuery.SetField("server")

		finalQuery = bleve.NewConjunctionQuery(finalQuery, serverQuery)
	}

	// BLV-6: constrain to tool documents. intent_cache / hfsc_trace traces share
	// the index; without this a whitespace query (which collapses to Wildcard("*"))
	// or any future shared field could surface non-tool docs into tool search.
	typeQuery := bleve.NewTermQuery("tool")
	typeQuery.SetField("_type")
	finalQuery = bleve.NewConjunctionQuery(finalQuery, typeQuery)

	searchRequest := bleve.NewSearchRequest(finalQuery)
	searchRequest.Size = searchRecallK
	searchRequest.Fields = []string{"urn", "name", "server", "description"}
	highlight := bleve.NewHighlightWithStyle("html")
	highlight.AddField("description")
	searchRequest.Highlight = highlight

	// Density Facet Tracking maps cross-server tool balancing recursively tracking "hot" boundaries.
	catFacet := bleve.NewFacetRequest("category", 10)
	searchRequest.AddFacet("category", catFacet)

	// BLV-2: honor cancellation/deadline on the lexical leg (a broad fuzzy/wildcard
	// query with faceting + highlighting can be expensive).
	results, err := si.index.Load().(bleve.Index).SearchInContext(ctx, searchRequest)
	if err != nil {
		return nil, err
	}

	return results, nil
}

// IndexSyntheticIntent maps a successful Socratic interaction vector dynamically into the secondary GhostIndex
func (si *SearchIndex) IndexSyntheticIntent(prompt string, dag []string) error {
	si.mu.RLock()
	defer si.mu.RUnlock()
	id := fmt.Sprintf("intent:%x", sha256.Sum256([]byte(prompt)))
	doc := SyntheticIntent{
		TypeField: "intent_cache",
		ID:        id,
		Prompt:    prompt,
		DAG:       dag,
		Timestamp: time.Now().Unix(),
	}
	return si.index.Load().(bleve.Index).Index(id, doc)
}

// IndexHFSCTrace adds a single HFSC telemetry payload to the Bleve index for historical auditing.
func (si *SearchIndex) IndexHFSCTrace(doc HFSCTraceDocument) error {
	si.mu.RLock()
	defer si.mu.RUnlock()
	doc.TypeField = "hfsc_trace"
	return si.index.Load().(bleve.Index).Index(doc.ID, doc)
}

// maxSyntheticDocsPerType caps how many intent_cache / hfsc_trace documents are
// retained. These accrete without bound otherwise (BLV-11), inflating index size
// and segment-merge cost while contributing nothing to tool ranking.
const maxSyntheticDocsPerType = 2000

// PruneSyntheticDocs trims old synthetic (non-tool) documents down to the retention
// cap, deleting the oldest beyond it. Cheap when under the cap (the paginated probe
// returns nothing). Intended to run on the periodic maintenance/GC tick.
func (si *SearchIndex) PruneSyntheticDocs() (int, error) {
	return si.pruneSyntheticDocs(maxSyntheticDocsPerType)
}

func (si *SearchIndex) pruneSyntheticDocs(keep int) (int, error) {
	si.mu.RLock()
	idx, ok := si.index.Load().(bleve.Index)
	si.mu.RUnlock()
	if !ok || idx == nil {
		return 0, fmt.Errorf("search index not initialized")
	}

	types := []struct{ typ, tsField string }{
		{"intent_cache", "timestamp"},
		{"hfsc_trace", "received_at"},
	}

	total := 0
	for _, t := range types {
		q := bleve.NewTermQuery(t.typ)
		q.SetField("_type")
		req := bleve.NewSearchRequest(q)
		req.SortBy([]string{"-" + t.tsField}) // newest first
		req.From = keep                       // skip the ones we retain
		req.Size = 10000                      // bound the per-sweep delete volume
		res, err := idx.Search(req)
		if err != nil || len(res.Hits) == 0 {
			continue
		}
		batch := idx.NewBatch()
		for _, hit := range res.Hits {
			batch.Delete(hit.ID)
		}
		if err := idx.Batch(batch); err == nil {
			total += len(res.Hits)
		}
	}
	return total, nil
}

// GhostResult holds a URN and its associated timestamp for decay computation.
type GhostResult struct {
	URN       string
	Timestamp int64
}

// SearchSyntheticIntents performs a localized semantic probe across past successful orchestrations extracting pure DAG URIs dynamically.
func (si *SearchIndex) SearchSyntheticIntents(prompt string) ([]GhostResult, error) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	idx, ok := si.index.Load().(bleve.Index)
	if !ok || idx == nil {
		return nil, fmt.Errorf("search index not initialized")
	}

	query := bleve.NewMatchQuery(prompt)
	query.SetField("prompt")

	req := bleve.NewSearchRequest(query)
	req.Size = 1 // Only load highest confidence Ghost DAG natively
	req.Fields = []string{"dag", "timestamp"}

	res, err := idx.Search(req)
	if err != nil || len(res.Hits) == 0 {
		return nil, err
	}

	hit := res.Hits[0]

	// Extract timestamp for decay
	var ts int64
	if tsRaw, ok := hit.Fields["timestamp"].(float64); ok {
		ts = int64(tsRaw)
	}

	// Extract string slices gracefully natively
	var results []GhostResult
	if dagRaw, ok := hit.Fields["dag"].([]any); ok {
		for _, u := range dagRaw {
			results = append(results, GhostResult{URN: fmt.Sprintf("%v", u), Timestamp: ts})
		}
	} else if dagSingle, ok := hit.Fields["dag"].(string); ok {
		results = append(results, GhostResult{URN: dagSingle, Timestamp: ts})
	}

	return results, nil
}

// GetToolsByServer natively interrogates the search index extracting raw URIs for a specific server instance.
func (si *SearchIndex) GetToolsByServer(serverName string, limit int) ([]string, error) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	idx, ok := si.index.Load().(bleve.Index)
	if !ok || idx == nil {
		return nil, fmt.Errorf("search index not initialized")
	}

	query := bleve.NewTermQuery(serverName)
	query.SetField("server")

	req := bleve.NewSearchRequest(query)
	req.Size = limit
	req.Fields = []string{"urn"}

	res, err := idx.Search(req)
	if err != nil {
		return nil, err
	}

	var urns []string
	for _, hit := range res.Hits {
		if urn, ok := hit.Fields["urn"].(string); ok {
			urns = append(urns, urn)
		} else {
			urns = append(urns, hit.ID)
		}
	}
	return urns, nil
}

// IsEmpty returns true if the index has no documents
func (si *SearchIndex) IsEmpty() bool {
	si.mu.RLock()
	defer si.mu.RUnlock()
	idx, ok := si.index.Load().(bleve.Index)
	if !ok || idx == nil {
		return true
	}
	count, err := idx.DocCount()
	return err != nil || count == 0
}

// DocCount returns the number of active documents inside the semantic inference index natively
func (si *SearchIndex) DocCount() (uint64, error) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	idx, ok := si.index.Load().(bleve.Index)
	if !ok || idx == nil {
		return 0, fmt.Errorf("search index not initialized")
	}
	return idx.DocCount()
}

// ToolCount returns the number of active documents of type "tool" in the index.
func (si *SearchIndex) ToolCount() (uint64, error) {
	si.mu.RLock()
	defer si.mu.RUnlock()
	idx, ok := si.index.Load().(bleve.Index)
	if !ok || idx == nil {
		return 0, fmt.Errorf("search index not initialized")
	}
	q := bleve.NewTermQuery("tool")
	q.SetField("_type")
	searchReq := bleve.NewSearchRequest(q)
	searchReq.Size = 0
	res, err := idx.Search(searchReq)
	if err != nil {
		return 0, err
	}
	return res.Total, nil
}

// IndexRecord adds or updates a record in the index using the Bleve adapter.
func (si *SearchIndex) IndexRecord(doc BleveToolDocument) error {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.index.Load().(bleve.Index).Index(doc.URN, doc)
}

// IndexBatch adds or updates a slice of Bleve documents in the index efficiently via a single transaction.
func (si *SearchIndex) IndexBatch(docs []BleveToolDocument) error {
	si.mu.RLock()
	defer si.mu.RUnlock()
	idx, ok := si.index.Load().(bleve.Index)
	if !ok || idx == nil {
		return fmt.Errorf("search index not initialized")
	}

	batch := idx.NewBatch()
	var skipped int
	var firstErr error
	for _, doc := range docs {
		if err := batch.Index(doc.URN, doc); err != nil {
			// BLV-1: skip the offending doc rather than aborting the whole batch,
			// which previously dropped up to a full batch (~1000 tools) from the
			// lexical index on a single bad document.
			skipped++
			if firstErr == nil {
				firstErr = err
			}
			slog.Warn("search index: skipping un-indexable document in batch", "urn", doc.URN, "error", err)
			continue
		}
	}
	if err := idx.Batch(batch); err != nil {
		return err
	}
	if skipped > 0 {
		return fmt.Errorf("indexed batch with %d document(s) skipped: %w", skipped, firstErr)
	}
	return nil
}

// DeleteRecord removes a record from the index
func (si *SearchIndex) DeleteRecord(urn string) error {
	si.mu.RLock()
	defer si.mu.RUnlock()
	return si.index.Load().(bleve.Index).Delete(urn)
}

// SwapIndex safely hot-swaps the underlying bleve.Index pointer and closes the old one
func (si *SearchIndex) SwapIndex(newIndex bleve.Index) error {
	si.mu.Lock()
	oldIndex := si.index.Load().(bleve.Index)
	si.index.Store(newIndex)
	si.mu.Unlock()
	if oldIndex != nil {
		return oldIndex.Close()
	}
	return nil
}

// Close closes the index
func (si *SearchIndex) Close() error {
	si.mu.Lock()
	defer si.mu.Unlock()
	idx := si.index.Load().(bleve.Index)
	if idx != nil {
		return idx.Close()
	}
	return nil
}
