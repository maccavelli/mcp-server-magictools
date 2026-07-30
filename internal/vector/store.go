// Package vector provides the HNSW semantic search engine and embedding upsert path.
package vector

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/hnsw"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

// FailureAnchorPrefix is the namespace prefix for contrastive failure-anchor
// nodes stored in the HNSW graph. These nodes are NOT tools and are managed by
// their own lifecycle (intelligence.PruneFailureAnchors), so graph reconciliation
// against the tool set must preserve them. The intelligence package aliases this
// constant so there is a single source of truth.
const FailureAnchorPrefix = "fail:"

// Engine is the central abstraction for semantic intelligence inside magictools.
type Engine struct {
	mu              sync.RWMutex
	metaMu          sync.Mutex // Protects metadata file read/write operations
	graph           *hnsw.Graph[string]
	embedder        Embedder
	dbPath          string
	metaPath        string
	initialized     bool
	corrupt         atomic.Bool // set when a search panic is intercepted
	tombstones      sync.Map    // O(1) in-memory filter mask for deleted vectors
	embeddingHashes sync.Map    // id -> content hash for upsert invalidation
	expectedDims    int         // configured embedding dimensionality (0 = unchecked)
}

var (
	GlobalEngine *Engine
	initOnce     sync.Once
)

// dimensionSentinel stores the embedding configuration hash to detect config changes.
type dimensionSentinel struct {
	Provider   string   `json:"provider"`
	Model      string   `json:"model"`
	Dims       int      `json:"dims"`
	APIURL     string   `json:"api_url,omitempty,omitzero"`
	Hash       string   `json:"hash"`
	Tombstones []string `json:"tombstones,omitempty"`
}

func computeSentinelHash(provider, model string, dims int, apiURL string) string {
	data := fmt.Sprintf("%s:%s:%d:%s", provider, model, dims, apiURL)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(data)))
}

// createHNSWGraph constructs an HNSW graph properly initialized with
// the required Distance function and configuration parameters.
// 🧠 ADR-0016: M=32 and EfSearch=100 for improved recall with ~80 tools.
// The computational cost is negligible at this scale but recall improvement
// is significant for niche tool discovery.
// ADR-0016 HNSW tuning. Kept as constants so they can be re-applied after Import,
// which otherwise silently overwrites them with whatever was persisted (HNW-6).
const (
	hnswM        = 32  // 2x connectivity for better recall
	hnswEfSearch = 100 // 5x beam width — negligible cost at this scale
)

func createHNSWGraph() *hnsw.Graph[string] {
	g := hnsw.NewGraph[string]()
	g.Distance = hnsw.CosineDistance
	g.M = hnswM
	g.EfSearch = hnswEfSearch
	// HNW-9: leave g.Ml at the library default (hnsw.NewGraph sets 0.25). It must NOT
	// be 0 — maxLevel() panics on Ml==0, so do not "reset" it to zero here.
	return g
}

// InitGlobalEngine initializes the global singleton for the HNSW index.
// It reads embedding configuration from the Config struct.
func InitGlobalEngine(dbDir string, cfg *config.Config) error {
	var err error
	initOnce.Do(func() { err = vectorBootEngine(dbDir, cfg) })
	return err
}

// VectorEnabled returns true if the engine has a valid embedder configured.
func (e *Engine) VectorEnabled() bool {
	if e == nil {
		return false
	}
	return e.embedder != nil && e.graph != nil
}

// GetEngine returns the global engine safely.
func GetEngine() *Engine {
	return GlobalEngine
}

// NewTestEngine returns an initialized in-memory engine for integration tests.
func NewTestEngine(embedder Embedder, dims int) *Engine {
	return &Engine{
		graph:        createHNSWGraph(),
		embedder:     embedder,
		expectedDims: dims,
		initialized:  true,
	}
}

// Save serializes the HNSW graph to disk.
func (e *Engine) Save() error {
	e.mu.RLock()
	defer e.mu.RUnlock()

	tmpPath := e.dbPath + ".tmp"
	f, err := os.Create(tmpPath) //nolint:gosec // G304: tmp path derived from controlled db directory
	if err != nil {
		return fmt.Errorf("failed to create temporary vector db backup: %w", err)
	}

	if err := e.graph.Export(f); err != nil {
		closeFileOrWarn(f, "vector-export")
		removeOrWarn(tmpPath)
		return fmt.Errorf("failed to export hnsw graph: %w", err)
	}
	if err := f.Sync(); err != nil {
		closeFileOrWarn(f, "vector-sync")
		removeOrWarn(tmpPath)
		return fmt.Errorf("failed to sync vector db file: %w", err)
	}
	closeFileOrWarn(f, "vector-save")

	if err := os.Rename(tmpPath, e.dbPath); err != nil {
		removeOrWarn(tmpPath)
		return fmt.Errorf("failed to commit vector db backup: %w", err)
	}
	return nil
}

// UpsertDocument embeds text and inserts or replaces the vector when contentHash changes.
// When contentHash matches the last upsert for id, the call is a no-op.
func (e *Engine) UpsertDocument(ctx context.Context, id, text, contentHash string) error {
	if !e.VectorEnabled() {
		return fmt.Errorf("cannot upsert document: Vector capability is offline")
	}

	e.healIfCorrupt()

	if contentHash != "" {
		if prev, ok := e.embeddingHashes.Load(id); ok {
			prevHash, hashOK := embeddingHashFromMapVal(prev)
			if hashOK && prevHash == contentHash && e.isActiveDocument(id) {
				telemetry.SearchMetrics.CacheHits.Add(1)
				telemetry.SearchMetrics.VectorStaleSkips.Add(1)
				return nil
			}
		}
	} else if e.isActiveDocument(id) {
		telemetry.SearchMetrics.CacheHits.Add(1)
		telemetry.SearchMetrics.VectorStaleSkips.Add(1)
		return nil
	}

	telemetry.SearchMetrics.CacheMisses.Add(1)

	if e.HasDocument(id) {
		e.DeleteDocument(id)
		e.healIfCorrupt()
	}

	if err := e.addDocumentUnchecked(ctx, id, text); err != nil {
		return err
	}
	if contentHash != "" {
		e.embeddingHashes.Store(id, contentHash)
	}
	return nil
}

// AddDocument embeds text and inserts the vector into the HNSW index.
// For Gemini, this uses RETRIEVAL_DOCUMENT task type for asymmetric search.
func (e *Engine) AddDocument(ctx context.Context, id string, text string) (err error) {
	if !e.VectorEnabled() {
		return fmt.Errorf("cannot add document: Vector capability is offline")
	}

	e.healIfCorrupt()

	e.mu.RLock()
	_, exists := e.graph.Lookup(id)
	e.mu.RUnlock()
	if exists {
		return nil
	}

	return e.addDocumentUnchecked(ctx, id, text)
}

func (e *Engine) addDocumentUnchecked(ctx context.Context, id string, text string) error {
	docCtx := WithTaskType(ctx, "RETRIEVAL_DOCUMENT")

	start := time.Now()
	vector, embedErr := e.embedder.Embed(docCtx, text)
	telemetry.SearchMetrics.EmbedLatencyMs.Add(time.Since(start).Milliseconds())
	if embedErr != nil {
		return embedErr
	}

	return e.AddVector(ctx, id, vector)
}

// AddVector injects a pre-computed float32 vector directly into the HNSW index.
func (e *Engine) AddVector(ctx context.Context, id string, vector []float32) (err error) {
	if !e.VectorEnabled() {
		return fmt.Errorf("cannot add vector: Vector capability is offline")
	}

	// 🛡️ SELF-HEALING: Rebuild graph if previous operations detected corruption
	e.healIfCorrupt()

	// 🛡️ NATIVE LOOKUP GUARD: Skip if already embedded, preventing panics and saving API tokens
	e.mu.RLock()
	_, exists := e.graph.Lookup(id)
	e.mu.RUnlock()
	if exists {
		// INT-4: the node is still physically present but may have been tombstoned
		// by a prior DeleteDocuments (tombstone-only, no immediate rebuild). A re-add
		// must resurrect it — clear the tombstone so it isn't shadowed from search and
		// then physically dropped by the next deferred rebuild.
		e.tombstones.Delete(id)
		return nil
	}

	// 🛡️ NIL VECTOR GUARD: Prevent HNSW CosineDistance panic on nil/empty embedding.
	if len(vector) == 0 {
		return fmt.Errorf("embedding returned nil/empty vector for %q", id)
	}
	if e.expectedDims > 0 && len(vector) != e.expectedDims {
		return fmt.Errorf("embedding dimension mismatch for %q: got %d want %d", id, len(vector), e.expectedDims)
	}
	// IDX-5: reject non-finite or zero-norm vectors — CosineDistance of these is
	// NaN, which corrupts neighbor selection and all subsequent search ordering.
	var sumSq float64
	for _, c := range vector {
		f := float64(c)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("embedding contains a non-finite component for %q", id)
		}
		sumSq += f * f
	}
	if sumSq == 0 {
		return fmt.Errorf("embedding is a zero vector for %q", id)
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	// 🛡️ DEFENSIVE: Belt-and-suspenders nil graph check.
	if e.graph == nil {
		return fmt.Errorf("HNSW graph is nil, cannot add document %q", id)
	}

	// 🛡️ PANIC GUARD: Catch HNSW panics during Add() due to corrupt topology.
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("vector engine: intercepted native HNSW add panic from corrupt topology, scheduling self-healing rebuild.", "component", "vector", "urn", id, "panic", r)
				e.corrupt.Store(true)
				err = fmt.Errorf("HNSW Add panicked: %v", r)
			}
		}()
		e.graph.Add(hnsw.MakeNode(id, vector))
	}()

	if err == nil {
		// INT-4: a prior DeleteDocuments may have tombstoned this id. Clear it now
		// that the node is freshly added, so the new node is visible to search and
		// not physically dropped by the next deferred rebuild. (AddDocument and
		// UpsertDocument both funnel through here, so this is the single choke point.)
		e.tombstones.Delete(id)
	}
	return err
}

// DeleteDocument removes a natively embedded document from the HNSW index using async Tombstone & Rebuild.
// HNW-13: returns whether id was actually an active document — the old form always
// reported true (DeleteDocuments counts ids tombstoned, not ids that existed), so
// callers couldn't distinguish a real delete from a no-op (and absent ids left a
// phantom tombstone behind).
func (e *Engine) DeleteDocument(id string) bool {
	if !e.isActiveDocument(id) {
		return false
	}
	return e.DeleteDocuments(id) > 0
}

// DeleteDocuments tombstones multiple documents and persists the tombstone meta
// file a single time (IDX-8) instead of once per id — a K-document purge was
// previously O(K) meta writes plus O(K²) tombstone collection. The graph rebuild
// is deferred via the corrupt flag and happens on the next index operation.
// Returns the number of ids tombstoned.
func (e *Engine) DeleteDocuments(ids ...string) int {
	if !e.VectorEnabled() || e.graph == nil || len(ids) == 0 {
		return 0
	}
	for _, id := range ids {
		e.tombstones.Store(id, true) // O(1) tombstone mask
		e.embeddingHashes.Delete(id) // HNW-8: drop the upsert-invalidation entry now
	}
	e.corrupt.Store(true) // trigger async rebuild (once)
	// Persist tombstones to the meta file a single time.
	e.saveMeta(e.collectTombstones())
	return len(ids)
}

func (e *Engine) collectTombstones() []string {
	var list []string
	e.tombstones.Range(func(key, value any) bool {
		if val, ok := value.(bool); ok && val {
			if s, ok := key.(string); ok {
				list = append(list, s)
			}
		}
		return true
	})
	return list
}

func (e *Engine) saveMeta(tombstones []string) {
	if e.metaPath == "" {
		return
	}
	e.metaMu.Lock()
	defer e.metaMu.Unlock()

	var stored dimensionSentinel
	if data, err := os.ReadFile(e.metaPath); err == nil {
		if err := json.Unmarshal(data, &stored); err != nil {
			slog.Warn("vector: failed to unmarshal meta file", "path", e.metaPath, "error", err)
		}
	}
	stored.Tombstones = tombstones
	if data, err := json.MarshalIndent(stored, "", "  "); err == nil {
		writeFileOrWarn(e.metaPath, data, 0o600) //nolint:gosec // G306: user-local vector metadata
	}
}

// HasDocument returns true if the URN natively exists within the active HNSW graph graph.
func (e *Engine) HasDocument(id string) bool {
	if !e.VectorEnabled() || e.graph == nil {
		return false
	}
	e.mu.RLock()
	_, exists := e.graph.Lookup(id)
	e.mu.RUnlock()
	return exists
}

// isActiveDocument returns true when id is in the graph and not tombstoned.
func (e *Engine) isActiveDocument(id string) bool {
	if !e.HasDocument(id) {
		return false
	}
	_, tombstoned := e.tombstones.Load(id)
	return !tombstoned
}

// GetVector returns the natively embedded vector for a given URN.
func (e *Engine) GetVector(id string) ([]float32, bool) {
	if !e.VectorEnabled() || e.graph == nil {
		return nil, false
	}
	e.mu.RLock()
	vec, exists := e.graph.Lookup(id)
	e.mu.RUnlock()
	return vec, exists
}

// RequiresHydration returns true if the native HNSW graph is mathematically empty and requires a structural injection.
func (e *Engine) RequiresHydration() bool {
	if !e.VectorEnabled() || e.graph == nil {
		return false
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.graph.Len() == 0
}

// Len returns the number of nodes currently in the HNSW graph.
func (e *Engine) Len() int {
	if !e.VectorEnabled() || e.graph == nil {
		return 0
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.graph.Len()
}

// Keys returns all node keys currently in the HNSW graph.
func (e *Engine) Keys() []string {
	if !e.VectorEnabled() || e.graph == nil {
		return nil
	}
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.graph.Keys()
}

// PruneOrphanedNodes reconciles the HNSW graph against a set of valid keys.
// Any node whose key is NOT in validKeys is removed from the graph.
// Returns the number of pruned nodes. Persists the cleaned graph to disk.
//
// 🛡️ REBUILD STRATEGY: Instead of calling graph.Delete() which corrupts
// HNSW layer topology (leaving nil entry points that cause SIGSEGV on
// subsequent Search calls), we extract all valid node embeddings and
// reconstruct the graph from scratch. This is safe because Lookup()
// returns stored vectors without any API calls.
func (e *Engine) PruneOrphanedNodes(validKeys map[string]bool) int {
	if !e.VectorEnabled() || e.graph == nil {
		return 0
	}

	e.mu.Lock()
	allKeys := e.graph.Keys()

	// Collect valid nodes with their embeddings
	type nodeEntry struct {
		key string
		vec hnsw.Vector
	}
	var validNodes []nodeEntry
	var pruned int
	for _, key := range allKeys {
		// Failure-anchor nodes (fail:<urn>:<hash>) are not tools; they have their
		// own lifecycle and must survive tool-set reconciliation. Preserving them
		// here keeps the contrastive-penalty feature alive across GC/purge sweeps.
		if validKeys[key] || strings.HasPrefix(key, FailureAnchorPrefix) {
			if vec, ok := e.graph.Lookup(key); ok && len(vec) > 0 {
				validNodes = append(validNodes, nodeEntry{key: key, vec: vec})
			}
		} else {
			pruned++
		}
	}

	if pruned > 0 {
		// Rebuild the graph from valid nodes only
		newGraph := createHNSWGraph()
		for _, entry := range validNodes {
			newGraph.Add(hnsw.MakeNode(entry.key, entry.vec))
		}
		e.graph = newGraph
		e.corrupt.Store(false)
	}
	e.mu.Unlock()

	if pruned > 0 {
		slog.Info("vector engine: pruned orphaned HNSW nodes via rebuild",
			"component", "vector",
			"pruned", pruned,
			"remaining", e.Len())
		if err := e.Save(); err != nil {
			slog.Warn("vector engine: failed to persist rebuilt graph",
				"component", "vector", "error", err)
		}
	}

	return pruned
}

// rebuildGraph extracts all valid node embeddings from the current graph
// and reconstructs it from scratch. This heals corrupt HNSW layer topology
// caused by Delete operations without requiring any embedding API calls.
// MUST be called with e.mu held exclusively (write lock).
func (e *Engine) rebuildGraph() {
	if e.graph == nil {
		e.graph = createHNSWGraph()
		e.corrupt.Store(false)
		return
	}

	// 🛡️ RACE CONDITION FIX: Clear corrupt flag BEFORE processing.
	// If DeleteDocument fires concurrently, it sets it back to true,
	// ensuring the deleted node is picked up in a subsequent rebuild.
	e.corrupt.Store(false)

	keys := e.graph.Keys()
	type nodeEntry struct {
		key string
		vec hnsw.Vector
	}
	var validNodes []nodeEntry
	for _, key := range keys {
		if _, deleted := e.tombstones.Load(key); deleted {
			continue // excluded from the rebuilt graph
		}
		if vec, ok := e.graph.Lookup(key); ok && len(vec) > 0 {
			validNodes = append(validNodes, nodeEntry{key: key, vec: vec})
		}
	}

	newGraph := createHNSWGraph()
	for _, entry := range validNodes {
		newGraph.Add(hnsw.MakeNode(entry.key, entry.vec))
	}
	e.graph = newGraph

	// HNW-7/8: reap redundant bookkeeping. Every tombstoned key was excluded from
	// newGraph, so any tombstone or embedding-hash whose key is absent from the
	// rebuilt graph is dead weight and is dropped — this also reclaims tombstones
	// for ids that were never in the graph (delete-of-absent), which the old
	// per-key delete missed. A tombstone for a key still present (a delete that
	// raced in after the snapshot) is preserved; corrupt is already set, so the
	// next heal re-excludes it.
	e.tombstones.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok {
			if _, inGraph := newGraph.Lookup(k); !inGraph {
				e.tombstones.Delete(k)
			}
		}
		return true
	})
	e.embeddingHashes.Range(func(key, _ any) bool {
		if k, ok := key.(string); ok {
			if _, inGraph := newGraph.Lookup(k); !inGraph {
				e.embeddingHashes.Delete(k)
			}
		}
		return true
	})

	slog.Info("vector engine: self-healing rebuild completed",
		"component", "vector",
		"recovered_nodes", len(validNodes))
}

// healIfCorrupt checks the corruption flag and triggers a self-healing rebuild.
// Persistence is performed after releasing the write lock to avoid deadlocking
// with Save(), which acquires a read lock on the same mutex.
func (e *Engine) healIfCorrupt() {
	if !e.corrupt.Load() {
		return
	}

	e.mu.Lock()
	if !e.corrupt.Load() {
		e.mu.Unlock()
		return
	}

	e.rebuildGraph()
	e.mu.Unlock()

	if e.dbPath != "" {
		e.saveMeta(e.collectTombstones())
		if err := e.Save(); err != nil {
			slog.Warn("vector engine: failed to persist healed graph",
				"component", "vector", "error", err)
		}
	}
}

// Search queries the HNSW index for nearest neighbors to the intent.
// For Gemini, this uses RETRIEVAL_QUERY task type for asymmetric search.
func (e *Engine) Search(ctx context.Context, intent string, k int) ([]string, error) {
	if !e.VectorEnabled() {
		return nil, fmt.Errorf("cannot search: Vector capability is offline")
	}

	// 🛡️ SELF-HEALING: Rebuild graph if previous search detected corruption
	e.healIfCorrupt()

	// 🛡️ ASYMMETRIC EMBEDDING: Mark as query for searching
	queryCtx := WithTaskType(ctx, "RETRIEVAL_QUERY")

	targetVec, err := e.embedder.Embed(queryCtx, intent)
	if err != nil {
		return nil, err
	}

	e.mu.RLock()
	var nodes []hnsw.Node[string]

	// 🛡️ NIL DEREF GUARD: The `github.com/coder/hnsw` library has a critical native fault
	// where Delete() removes nodes from layer maps but never trims empty layers from the
	// layers slice. This leaves nil entry points that cause SIGSEGV during Search traversal.
	if e.graph.Len() > 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("vector engine: intercepted native HNSW search panic from corrupt topology, scheduling self-healing rebuild.", "component", "vector", "panic", r)
					e.corrupt.Store(true)
				}
			}()
			nodes = e.graph.Search(targetVec, k*5)
		}()
	}
	e.mu.RUnlock()

	// IDX-2: graph.Search returns candidates in heap order (only index 0 is
	// guaranteed nearest). Score every non-tombstoned node and sort by actual
	// cosine before truncating to k, so the true nearest-k are returned in order.
	scored := make([]ScoredResult, 0, len(nodes))
	for _, n := range nodes {
		if _, deleted := e.tombstones.Load(n.Key); deleted {
			continue
		}
		scored = append(scored, ScoredResult{Key: n.Key, Score: 1.0 - float64(hnsw.CosineDistance(targetVec, n.Value))})
	}
	sort.Slice(scored, func(i, j int) bool { return scored[i].Score > scored[j].Score })
	if len(scored) > k {
		scored = scored[:k]
	}
	var results []string
	var topScore float64
	for i, s := range scored {
		results = append(results, s.Key)
		if i == 0 {
			topScore = s.Score
		}
	}

	// 🛡️ TELEMETRY: Native increment within the mathematical kernel
	telemetry.SearchMetrics.VectorSearches.Add(1)
	if len(nodes) > 0 {
		for {
			oldBits := telemetry.SearchMetrics.TotalConfidenceScore.Load()
			oldScore := math.Float64frombits(oldBits)

			var newScore float64
			if oldScore == 0 {
				newScore = topScore
			} else {
				// EMA: 15% Latest Value + 85% Historic Ceiling
				newScore = (topScore * 0.15) + (oldScore * 0.85)
			}

			newBits := math.Float64bits(newScore)
			if telemetry.SearchMetrics.TotalConfidenceScore.CompareAndSwap(oldBits, newBits) {
				break
			}
		}
	}

	return results, nil
}

// ScoredResult pairs a URN key with its actual cosine similarity score.
type ScoredResult struct {
	Key   string
	Score float64
}

// searchScored is the shared scoring core: embed the intent, compute true cosine
// for every candidate the HNSW graph returns, drop tombstoned and !keep(key)
// nodes, sort by descending score, and truncate to k. keep==nil keeps everything.
func (e *Engine) searchScored(ctx context.Context, intent string, k int, keep func(key string) bool) ([]ScoredResult, error) {
	if !e.VectorEnabled() {
		return nil, fmt.Errorf("cannot search: Vector capability is offline")
	}

	// 🛡️ SELF-HEALING: Rebuild graph if previous search detected corruption
	e.healIfCorrupt()

	queryCtx := WithTaskType(ctx, "RETRIEVAL_QUERY")

	embedStart := time.Now()
	targetVec, err := e.embedder.Embed(queryCtx, intent)
	telemetry.SearchMetrics.QueryEmbedLatencyMs.Add(time.Since(embedStart).Milliseconds())
	if err != nil {
		return nil, err
	}

	e.mu.RLock()
	var nodes []hnsw.Node[string]
	if e.graph.Len() > 0 {
		// 🛡️ NIL DEREF GUARD: Match Search() panic recovery pattern to prevent
		// unrecovered SIGSEGV from corrupt HNSW layer topology.
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("vector engine: intercepted native HNSW SearchWithScores panic from corrupt topology, scheduling self-healing rebuild.", "component", "vector", "panic", r)
					e.corrupt.Store(true)
				}
			}()
			nodes = e.graph.Search(targetVec, k*5)
		}()
	}
	e.mu.RUnlock()

	results := make([]ScoredResult, 0, len(nodes))
	for _, n := range nodes {
		if _, deleted := e.tombstones.Load(n.Key); deleted {
			continue
		}
		if keep != nil && !keep(n.Key) {
			continue
		}
		cosineScore := 1.0 - float64(hnsw.CosineDistance(targetVec, n.Value))
		results = append(results, ScoredResult{Key: n.Key, Score: cosineScore})
	}
	// IDX-2: sort by actual cosine before truncating (graph order is heap order).
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > k {
		results = results[:k]
	}
	return results, nil
}

// notFailureAnchor reports whether a key is a real document (not a failure anchor).
func notFailureAnchor(key string) bool { return !strings.HasPrefix(key, FailureAnchorPrefix) }

// SearchWithScores queries the HNSW index and returns actual cosine similarity scores
// for every returned node, replacing rank-based approximations with mathematically precise values.
//
// INT-5: failure anchors are excluded so they never consume top-k slots and starve
// real-tool recall. Anchor proximity has its own path (SearchFailureAnchors).
func (e *Engine) SearchWithScores(ctx context.Context, intent string, k int) ([]ScoredResult, error) {
	results, err := e.searchScored(ctx, intent, k, notFailureAnchor)
	if err != nil {
		return nil, err
	}

	telemetry.SearchMetrics.VectorSearches.Add(1)
	if len(results) > 0 {
		for {
			oldBits := telemetry.SearchMetrics.TotalConfidenceScore.Load()
			oldScore := math.Float64frombits(oldBits)

			var newScore float64
			if oldScore == 0 {
				newScore = results[0].Score
			} else {
				// EMA: 15% Latest Value + 85% Historic Ceiling
				newScore = (results[0].Score * 0.15) + (oldScore * 0.85)
			}

			newBits := math.Float64bits(newScore)
			if telemetry.SearchMetrics.TotalConfidenceScore.CompareAndSwap(oldBits, newBits) {
				break
			}
		}
	}

	return results, nil
}

// SearchFailureAnchors returns only failure-anchor nodes near the intent. The
// self-correction penalty path needs to see the anchors that SearchWithScores
// deliberately hides from tool search.
func (e *Engine) SearchFailureAnchors(ctx context.Context, intent string, k int) ([]ScoredResult, error) {
	return e.searchScored(ctx, intent, k, func(key string) bool {
		return strings.HasPrefix(key, FailureAnchorPrefix)
	})
}

// SearchByNode looks up a pre-embedded URN natively within the HNSW graph and computes actual Cosine Distances.
// 🛡️ PERFORMS ZERO API REQUESTS: Fast local execution securely bypassing MCP JSON-RPC timeouts.
func (e *Engine) SearchByNode(ctx context.Context, urn string, k int) ([]ScoredResult, error) {
	if !e.VectorEnabled() {
		return nil, fmt.Errorf("cannot search: Vector capability is offline")
	}

	// 🛡️ SELF-HEALING: Rebuild graph if previous search detected corruption
	e.healIfCorrupt()

	e.mu.RLock()
	targetVec, ok := e.graph.Lookup(urn)
	if !ok {
		e.mu.RUnlock()
		return nil, fmt.Errorf("urn %s not found in vector graph", urn)
	}

	var nodes []hnsw.Node[string]
	if e.graph.Len() > 0 {
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("vector engine: intercepted native HNSW SearchByNode panic from corrupt topology, scheduling self-healing rebuild.", "component", "vector", "panic", r)
					e.corrupt.Store(true)
				}
			}()
			nodes = e.graph.Search(targetVec, k*5) // Fetch extra to account for tombstones
		}()
	}
	e.mu.RUnlock()

	results := make([]ScoredResult, 0, len(nodes))
	for _, n := range nodes {
		// Skip self entirely natively
		if n.Key == urn {
			continue
		}
		if _, deleted := e.tombstones.Load(n.Key); deleted {
			continue
		}
		cosineScore := 1.0 - float64(hnsw.CosineDistance(targetVec, n.Value))
		results = append(results, ScoredResult{Key: n.Key, Score: cosineScore})
	}
	// IDX-2 (HNW-1): graph.Search returns heap order, not nearest-first. Sort by
	// actual cosine before truncating so callers get the true top-k.
	sort.Slice(results, func(i, j int) bool { return results[i].Score > results[j].Score })
	if len(results) > k {
		results = results[:k]
	}

	telemetry.SearchMetrics.VectorSearches.Add(1)
	if len(results) > 0 {
		for {
			oldBits := telemetry.SearchMetrics.TotalConfidenceScore.Load()
			oldScore := math.Float64frombits(oldBits)

			var newScore float64
			if oldScore == 0 {
				newScore = results[0].Score
			} else {
				// EMA: 15% Latest Value + 85% Historic Ceiling
				newScore = (results[0].Score * 0.15) + (oldScore * 0.85)
			}

			newBits := math.Float64bits(newScore)
			if telemetry.SearchMetrics.TotalConfidenceScore.CompareAndSwap(oldBits, newBits) {
				break
			}
		}
	}

	return results, nil
}
