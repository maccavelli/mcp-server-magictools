// Package db provides functionality for the db subsystem.
package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/dgraph-io/badger/v4/options"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

// CacheMetrics defines the CacheMetrics structure.
type CacheMetrics struct {
	Hits      uint64
	Misses    uint64
	Entries   int
	Tools     int
	Intel     int
	BleveDocs uint64
}

// SearchDomain defines the logical visibility scope for a tool search request.
type SearchDomain string

const (
	// DomainUserLand: Mask brainstorm and go-modernizer tools (Default for align_tools)
	DomainUserLand SearchDomain = "user_land"
	// DomainPipelineOrchestration: Search only brainstorm, go-modernizer, and magictools synthesizers
	DomainPipelineOrchestration SearchDomain = "pipeline_orchestration"
	// DomainSystem: Search all tools without sharding (Diagnostics/Maintenance)
	DomainSystem SearchDomain = "system"
)

// Store wraps BadgerDB and Bleve index
type Store struct {
	DB                *badger.DB
	Index             *SearchIndex
	Path              string // 🛡️ BASTION ROOT: The absolute path to the database directory
	PidPath           string
	Cache             *RegistryCache
	PendingHydrations atomic.Int64 // 🛡️ DIRTY FLAG: Tracks pending hydration count for optimistic skip
	toolsCount        atomic.Int64
	intelCount        atomic.Int64
	SynergySuccess    sync.Map           // Tracks RRF synergy strengths
	SynergyPenalty    sync.Map           // Tracks RRF synergy rejections
	synergyMu         sync.Mutex         // Protects synergy counter decay
	metricsBuf        sync.Map           // 🛡️ BATCHING: Accumulates ToolMetrics to prevent LSM write contention
	SearchGates       SearchGateConfig   // Per-leg confidence floors (synced from orchestrator config)
	Fusion            FusionConfig       // Score-fusion + ranking-boost weights (synced from orchestrator config)
	closing           chan struct{}      // 🛡️ LIFECYCLE: Signaled on Close() to abort background goroutines
	bgWg              sync.WaitGroup     // 🛡️ LIFECYCLE: Blocks Close() until background goroutines halt
	ctx               context.Context    // 🛡️ LIFECYCLE: Unified context for DB operations
	cancel            context.CancelFunc // 🛡️ LIFECYCLE: Triggers ctx.Done() during Close()
}

// ToolMetrics stores dynamic intent reliability scoring for search rescoring
type ToolMetrics struct {
	ProxyReliability     float64 `json:"proxy_reliability"`
	TotalSuccessfulCalls int     `json:"total_successful_calls"`
	TotalCalls           int     `json:"total_calls"`
	FailureRate          float64 `json:"failure_rate"`
	AvgLatencyMs         int64   `json:"avg_latency_ms"`
	LastErrorClass       string  `json:"last_error_class,omitempty,omitzero"`
}

// ToolRecord is undocumented but satisfies standard structural requirements.
type ToolRecord struct {
	URN         string         `json:"urn"`
	Name        string         `json:"name"`
	Server      string         `json:"server"`
	Description string         `json:"description"` // Full description
	InputSchema map[string]any `json:"input_schema"`
	LiteSummary string         `json:"lite_summary"`
	Category    string         `json:"category"`
	DependsOn   []string       `json:"depends_on"` // Required URNs for topological DAG sorting
	Requires    []string       `json:"requires,omitempty,omitzero"`
	Triggers    []string       `json:"triggers,omitempty,omitzero"`

	// 🛡️ PIPELINE TAXONOMY: Role and Phase classification for compose_pipeline DAG intelligence.
	// Role: ANALYZER, MUTATOR, CRITIC, SYNTHESIZER, DIAGNOSTIC
	// Phase: 1=DISCOVERY, 2=ANALYSIS, 3=PROPOSAL, 4=ADVERSARIAL, 5=SYNTHESIS
	Role           string `json:"role,omitempty,omitzero"`
	Phase          int    `json:"phase,omitempty,omitzero"`
	InputContract  string `json:"input_contract,omitempty,omitzero"`
	OutputContract string `json:"output_contract,omitempty,omitzero"`

	SchemaHash     string         `json:"schema_hash"`
	LastSyncedAt   int64          `json:"last_synced_at"`
	UsageCount     int64          `json:"usage_count"`
	LastUsedAt     int64          `json:"last_used_at"`
	TimeoutSecs    int            `json:"timeout_secs,omitempty,omitzero"`
	IsNative       bool           `json:"is_native,omitempty,omitzero"`
	Intent         string         `json:"intent,omitempty,omitzero"`
	ZeroValues     map[string]any `json:"zero_values,omitempty,omitzero"`     // Pre-computed schema defaults and fast auto-coercion fallbacks natively
	ParameterNames []string       `json:"parameter_names,omitempty,omitzero"` // Materialized from InputSchema.properties keys for Bleve keyword + HNSW embedding
	EnumValues     []string       `json:"enum_values,omitempty,omitzero"`     // Extracted string enums for Bleve lexical discovery
	Vector         []float32      `json:"vector,omitempty,omitzero"`          // HNSW cached semantic representation
	EmbeddingHash  string         `json:"embedding_hash,omitempty,omitzero"`  // Checksum of model+text to guarantee cache invalidation
	Metrics        ToolMetrics    `json:"-"`

	// Intelligence
	AnalysisStatus   string   `json:"-"` // pending, hydrated, failed
	SyntheticIntents []string `json:"-"`
	LexicalTokens    []string `json:"-"`
	NegativeTriggers []string `json:"-"`

	// Diagnostics (Transient)
	ConfidenceScore        float64 `json:"-"`
	HighlightedDescription string  `json:"-"`
}

// ToolIntelligence structurally defines the persisted semantic properties.
// It is intentionally split from ToolRecord to prevent aggressive orchestrator wiping (tools/list sync)
// from terminating previously learned LLM intents and utilization proxies.
type ToolIntelligence struct {
	AnalysisStatus   string      `json:"analysis_status,omitempty,omitzero"` // pending, hydrated, failed
	SchemaHash       string      `json:"schema_hash,omitempty,omitzero"`
	SyntheticIntents []string    `json:"synthetic_intents,omitempty,omitzero"`
	LexicalTokens    []string    `json:"lexical_tokens,omitempty,omitzero"`
	NegativeTriggers []string    `json:"negative_triggers,omitempty,omitzero"`
	Metrics          ToolMetrics `json:"metrics"`
}

// OverlayIntelligence merges persisted intelligence properties into a ToolRecord.
// This is the single authoritative merge point — all call sites MUST use this method
// instead of manually copying fields, preventing drift when new properties are added.
func (r *ToolRecord) OverlayIntelligence(intel *ToolIntelligence) {
	if intel == nil {
		return
	}
	r.AnalysisStatus = intel.AnalysisStatus
	r.SyntheticIntents = intel.SyntheticIntents
	r.LexicalTokens = intel.LexicalTokens
	r.NegativeTriggers = intel.NegativeTriggers
	r.Metrics = intel.Metrics
}

// countKeys performs a fast prefix-based scan counting total BadgerDB keys, exclusively for cold-boot seeding
func (s *Store) countKeys(prefix string) (int, error) {
	count := 0
	err := s.DB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // Optimize explicitly for counting keys
		it := txn.NewIterator(opts)
		defer it.Close()
		p := []byte(prefix)
		for it.Seek(p); it.ValidForPrefix(p); it.Next() {
			count++
		}
		return nil
	})
	return count, err
}

// GetMetrics returns a unified snapshot of Cache, Store, and Intelligence sizes
func (s *Store) GetMetrics() CacheMetrics {
	var metrics CacheMetrics
	if s == nil {
		return metrics
	}
	if s.Cache != nil {
		h, m, e := s.Cache.GetMetrics()
		metrics.Hits = h
		metrics.Misses = m
		metrics.Entries = e
	}
	metrics.Tools = int(s.toolsCount.Load())
	metrics.Intel = int(s.intelCount.Load())

	if s.Index != nil {
		if count, err := s.Index.DocCount(); err == nil {
			metrics.BleveDocs = count
		}
	}
	return metrics
}

// RecordSynergy synchronously updates RAM models and dispatches an async BadegerDB transaction safely.
func (s *Store) RecordSynergy(hash string, success bool) {
	if s == nil || s.DB == nil {
		return
	}
	succKey := []byte("synergy:success:" + hash)
	penKey := []byte("synergy:penalty:" + hash)

	succCounter := synergyCounterOrNew(&s.SynergySuccess, hash)
	penCounter := synergyCounterOrNew(&s.SynergyPenalty, hash)

	s.synergyMu.Lock()
	currSucc := succCounter.Load()
	currPen := penCounter.Load()

	// 🚀 CHRONOLOGICAL ENTROPY DECAY CEILING GUARD
	decayTriggered := false
	if currSucc+currPen > 500 {
		currSucc /= 2
		currPen /= 2
		succCounter.Store(currSucc)
		penCounter.Store(currPen)
		decayTriggered = true
	}

	var newSucc, newPen int64
	if success {
		newSucc = succCounter.Add(1)
		newPen = penCounter.Load()
	} else {
		newPen = penCounter.Add(1)
		newSucc = succCounter.Load()
	}
	s.synergyMu.Unlock()

	// 🛡️ LIFECYCLE: Register async disk flush with bgWg so Close() waits for completion.
	// Without this, Store.Close() can close the badger DB while this goroutine is
	// mid-transaction, causing "send on closed channel" panics during test teardown.
	s.bgWg.Add(1)
	go func(sK, pK []byte, sV, pV int64, updateBoth bool, isSuccess bool) {
		defer s.bgWg.Done()

		// Abort if the store is shutting down — the DB may already be closed.
		select {
		case <-s.closing:
			return
		default:
		}

		err := s.DB.Update(func(txn *badger.Txn) error {
			if updateBoth {
				if err := txn.Set(sK, []byte(strconv.FormatInt(sV, 10))); err != nil {
					return err
				}
				return txn.Set(pK, []byte(strconv.FormatInt(pV, 10)))
			}
			if isSuccess {
				return txn.Set(sK, []byte(strconv.FormatInt(sV, 10)))
			}
			return txn.Set(pK, []byte(strconv.FormatInt(pV, 10)))
		})
		if err != nil {
			slog.Error("database: failed to persist RRF synergy weight", "hash", hash, "error", err)
		}
	}(succKey, penKey, newSucc, newPen, decayTriggered, success)
	s.updateGlobalLearningWeight()
}

// updateGlobalLearningWeight scans synergy map and updates the global telemetry metric
func (s *Store) updateGlobalLearningWeight() {
	if s == nil {
		return
	}
	var totalSucc, totalPen int64
	s.SynergySuccess.Range(func(k, v any) bool {
		if counter, ok := v.(*atomic.Int64); ok {
			totalSucc += counter.Load()
		}
		return true
	})
	s.SynergyPenalty.Range(func(k, v any) bool {
		if counter, ok := v.(*atomic.Int64); ok {
			totalPen += counter.Load()
		}
		return true
	})
	totalEvents := totalSucc + totalPen
	if totalEvents > 0 {
		rate := float64(totalSucc) / float64(totalEvents)
		telemetry.SearchMetrics.LearningWeight.Store(math.Float64bits(rate))
	} else {
		telemetry.SearchMetrics.LearningWeight.Store(0)
	}
}

// GetSynergy returns the active heuristic trust models for the given Hash transition in O(1) latency.
func (s *Store) GetSynergy(hash string) (successes int64, penalties int64) {
	if s == nil {
		return 0, 0
	}
	if v, ok := s.SynergySuccess.Load(hash); ok {
		if counter, ok := atomicInt64FromMapVal(v); ok {
			successes = counter.Load()
		}
	}
	if v, ok := s.SynergyPenalty.Load(hash); ok {
		if counter, ok := atomicInt64FromMapVal(v); ok {
			penalties = counter.Load()
		}
	}
	return successes, penalties
}

// maxSynergyHashes caps how many intent→tool transition weights are retained.
// BDG-9: these keys accrete one per unique transition and are never removed (the
// decay ceiling only halves values), so the on-disk set and the boot seed scan
// grow without bound. Eviction keeps the most-reinforced transitions.
const maxSynergyHashes = 25000

// PruneSynergyWeights trims the synergy weight set to the retention cap, evicting
// the lowest-weight (least-reinforced) transitions first. Intended for the GC tick.
func (s *Store) PruneSynergyWeights() int {
	return s.evictSynergyWeightsBeyondCap(maxSynergyHashes)
}

func (s *Store) evictSynergyWeightsBeyondCap(maxKeys int) int {
	if s == nil || s.DB == nil {
		return 0
	}

	// Snapshot every hash with its combined (success+penalty) weight.
	weights := make(map[string]int64)
	s.SynergySuccess.Range(func(k, v any) bool {
		key, keyOK := stringFromMapKey(k)
		if c, ok := atomicInt64FromMapVal(v); keyOK && ok {
			weights[key] += c.Load()
		}
		return true
	})
	s.SynergyPenalty.Range(func(k, v any) bool {
		key, keyOK := stringFromMapKey(k)
		if c, ok := atomicInt64FromMapVal(v); keyOK && ok {
			weights[key] += c.Load()
		}
		return true
	})
	if len(weights) <= maxKeys {
		return 0
	}

	type hashWeight struct {
		hash   string
		weight int64
	}
	list := make([]hashWeight, 0, len(weights))
	for h, w := range weights {
		list = append(list, hashWeight{h, w})
	}
	// Ascending by weight — evict the lowest-value transitions.
	sort.Slice(list, func(i, j int) bool { return list[i].weight < list[j].weight })
	evict := list[:len(list)-maxKeys]

	s.synergyMu.Lock()
	for _, e := range evict {
		s.SynergySuccess.Delete(e.hash)
		s.SynergyPenalty.Delete(e.hash)
	}
	s.synergyMu.Unlock()

	wb := s.DB.NewWriteBatch()
	for _, e := range evict {
		wbDeleteOrWarn(wb, []byte("synergy:success:"+e.hash))
		wbDeleteOrWarn(wb, []byte("synergy:penalty:"+e.hash))
	}
	if err := wb.Flush(); err != nil {
		slog.Warn("database: failed to flush synergy weight prune", "error", err)
	}

	s.updateGlobalLearningWeight()
	return len(evict)
}

// seedSynergyWeights parses historical synergy on boot.
func (s *Store) seedSynergyWeights() {
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true

		// BDG-11: count corrupt values rather than silently dropping them.
		var parseFailures int

		// Load Successes
		it := txn.NewIterator(opts)
		defer it.Close()
		prefixS := []byte("synergy:success:")
		for it.Seek(prefixS); it.ValidForPrefix(prefixS); it.Next() {
			item := it.Item()
			hash := strings.TrimPrefix(string(item.Key()), string(prefixS))
			itemValueOrWarn(item, func(v []byte) error {
				if val, err := strconv.ParseInt(string(v), 10, 64); err == nil {
					counter := &atomic.Int64{}
					counter.Store(val)
					s.SynergySuccess.Store(hash, counter)
				} else {
					parseFailures++
				}
				return nil
			})
		}

		// Load Penalties
		it2 := txn.NewIterator(opts)
		defer it2.Close()
		prefixP := []byte("synergy:penalty:")
		for it2.Seek(prefixP); it2.ValidForPrefix(prefixP); it2.Next() {
			item := it2.Item()
			hash := strings.TrimPrefix(string(item.Key()), string(prefixP))
			itemValueOrWarn(item, func(v []byte) error {
				if val, err := strconv.ParseInt(string(v), 10, 64); err == nil {
					counter := &atomic.Int64{}
					counter.Store(val)
					s.SynergyPenalty.Store(hash, counter)
				} else {
					parseFailures++
				}
				return nil
			})
		}
		if parseFailures > 0 {
			slog.Warn("database: skipped corrupt synergy weight values during seed", "count", parseFailures)
		}
		return nil
	})
	s.updateGlobalLearningWeight()
}

// ResolveGCInterval returns the BadgerDB GC ticker interval.
// Default: 30 minutes. Configurable via MAGICTOOLS_BADGER_GC_INTERVAL env var.
// Accepts any value parseable by time.ParseDuration (e.g. "5m", "1h", "30s").
func ResolveGCInterval() time.Duration {
	const defaultInterval = 30 * time.Minute
	val := os.Getenv("MAGICTOOLS_BADGER_GC_INTERVAL")
	if val == "" {
		return defaultInterval
	}
	d, err := time.ParseDuration(val)
	if err != nil || d <= 0 {
		slog.Warn("database: invalid MAGICTOOLS_BADGER_GC_INTERVAL, using default",
			"value", val, "default", defaultInterval)
		return defaultInterval
	}
	return d
}

// StartBackgroundGC triggers Badger's value log garbage collection in a loop.
// This is critical for bastion environments to keep the disk footprint small.
func (s *Store) StartBackgroundGC(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.FlushMetrics()
			return
		case <-ticker.C:
			s.runBackgroundGCTick()
		}
	}
}

// NewStore initializes Badger and Bleve with lock cleanup logic
func NewStore(path string, limitOpts ...int) (*Store, error) {
	if err := os.MkdirAll(path, 0o750); err != nil {
		slog.Error("database: failed to create directory", "op", "NewStore", "path", path, "error", err)
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	if err := cleanupStaleProcess(path); err != nil {
		slog.Warn("Cleanup of stale process failed", "error", err)
	}

	if err := AcquireLock(path); err != nil {
		return nil, err
	}

	// 1. Initialize Primary Storage (Badger)
	opts := badger.DefaultOptions(path).
		WithLogger(nil).
		WithSyncWrites(false).                             // Changed to false to avoid massive sync blocks during JSON ingest
		WithCompression(options.ZSTD).                     // Force ZSTD for the VLOG to stop the 2GB bloat
		WithValueThreshold(1 << 20).                       // 1MB — keeps values in LSM tree
		WithValueLogFileSize(16 << 20).                    // 16MB — safety net rotation
		WithValueLogMaxEntries(100).                       // Aggressive rotation
		WithNumVersionsToKeep(1).                          // Keep only latest version
		WithIndexCacheSize(16 << 20).                      // 16MB
		WithBlockCacheSize(32 << 20).                      // 32MB
		WithMemTableSize(8 << 20).                         // 8MB
		WithNumMemtables(2).                               // 2
		WithNumLevelZeroTables(2).                         // 2
		WithNumLevelZeroTablesStall(4).                    // 4
		WithCompactL0OnClose(false).                       // 🛡️ F4: Avoid massive IO stall during Bastion shutdown
		WithChecksumVerificationMode(options.OnTableRead). // 🛡️ F5: Verify SST checksums on open — detect silent corruption
		WithNumGoroutines(4)                               // 🛡️ Constrain CPU threads to 2 CPUs limit

	db, err := badger.Open(opts)
	if err != nil {
		slog.Error("database: failed to open badger", "op", "NewStore", "path", path, "error", err)
		return nil, fmt.Errorf("failed to open badger db: %w", err)
	}

	// 2. Initialize Search Index (Bleve)
	index, err := NewSearchIndex(path)
	if err != nil {
		if closeErr := db.Close(); closeErr != nil {
			slog.Error("Failed to close database after search index failure", "error", closeErr)
		}
		return nil, fmt.Errorf("failed to open search index: %w", err)
	}

	pidPath := filepath.Join(path, "server.pid")
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		slog.Warn("Failed to write PID file", "error", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s := &Store{
		DB:          db,
		Index:       index,
		Path:        path,
		PidPath:     pidPath,
		Cache:       NewRegistryCache(limitOpts...),
		SearchGates: DefaultSearchGates(),
		Fusion:      DefaultFusionConfig(),
		closing:     make(chan struct{}),
		ctx:         ctx,
		cancel:      cancel,
	}

	// 3. Reconcile drift or empty
	toolCount, dErr := index.ToolCount()
	keyCount, kErr := s.countKeys("tool:")
	drifting := false
	if dErr == nil && kErr == nil && toolCount != safeUint64FromInt(keyCount) {
		drifting = true
		slog.Warn("Index drift detected", "badger_keys", keyCount, "bleve_docs", toolCount)
	}

	// 🛡️ COLD BOOT ATOMIC SEEDING
	s.toolsCount.Store(int64(keyCount))
	s.intelCount.Store(int64(countKeysOrZero(s, "intel:")))
	s.seedSynergyWeights()

	// Lazy-reindex if the search index is empty or drifted (Backgrounded to avoid IDE handshake timeouts)
	if index.IsEmpty() || drifting {
		s.startBackgroundReindexIfNeeded(ctx, index, drifting)
	}

	return s, nil
}

// Close closes the database and index
func (s *Store) Close() error {
	// 🛡️ LIFECYCLE: Signal background goroutines to abort before closing DB.
	close(s.closing)
	if s.cancel != nil {
		s.cancel()
	}
	s.bgWg.Wait() // Guaranteed shutdown before DB Reference drop

	if err := os.Remove(s.PidPath); err != nil && !os.IsNotExist(err) {
		slog.Warn("Failed to remove PID file on close", "path", s.PidPath, "error", err)
	}
	if dbLockFile != nil {
		if err := releaseOSLock(dbLockFile); err != nil {
			slog.Warn("Failed to release database lock", "error", err)
		}
		if err := dbLockFile.Close(); err != nil {
			slog.Warn("Failed to close database lock file", "error", err)
		}
	}
	if err := s.Index.Close(); err != nil {
		slog.Error("Failed to close search index", "error", err)
	}
	return s.DB.Close()
}

// UpdateWithRetryCtx wraps badger.Update with exponential backoff for Transaction Conflicts,
// respecting the provided context for cancellation. Request-path callers should use this
// variant to ensure backoff sleeps abort promptly during shutdown or request cancellation.
func (s *Store) UpdateWithRetryCtx(ctx context.Context, fn func(txn *badger.Txn) error) error {
	maxRetries := 5
	backoff := 10 * time.Millisecond

	var err error
	for range maxRetries {
		err = s.DB.Update(fn)
		if err == nil {
			return nil
		}
		if !errors.Is(err, badger.ErrConflict) {
			return err
		}

		timer := time.NewTimer(backoff)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
		backoff *= 2
	}
	slog.Warn("database: transaction conflict exhausted retries", "op", "UpdateWithRetryCtx", "retries", maxRetries, "error", err)
	return fmt.Errorf("transaction conflict after %d retries: %w", maxRetries, err)
}

// UpdateWithRetry wraps badger.Update with exponential backoff for Transaction Conflicts.
// For cancellation support, use UpdateWithRetryCtx.
func (s *Store) UpdateWithRetry(fn func(txn *badger.Txn) error) error {
	return s.UpdateWithRetryCtx(context.Background(), fn)
}

// BatchSaveTools saves multiple tools and their schemas. Writes go through a Badger
// WriteBatch (BDG-2), which flushes internally as it fills, so a large server sync
// can never exceed the single-transaction size limit (ErrTxnTooBig) the way the old
// one-shot Update did. The new-tool count uses a cheap read pass beforehand.
func (s *Store) BatchSaveTools(records []*ToolRecord, schemas map[string]map[string]any) error {
	// Pre-pass: count which tool URNs are new (for the in-memory counter only).
	var totalNewTools int64
	if len(records) > 0 {
		viewOrWarn(s.DB, func(txn *badger.Txn) error {
			for _, record := range records {
				if _, gErr := txn.Get([]byte("tool:" + record.URN)); errors.Is(gErr, badger.ErrKeyNotFound) {
					totalNewTools++
				}
			}
			return nil
		})
	}

	wb := s.DB.NewWriteBatch()
	// 1. Save all schemas
	for hash, schema := range schemas {
		data, err := json.Marshal(schema)
		if err != nil {
			continue // Skip bad schemas
		}
		compressed, err := util.Compress(data)
		if err != nil {
			continue
		}
		if err := wb.Set([]byte("schema:"+hash), compressed); err != nil {
			wb.Cancel()
			return err
		}
	}

	// 2. Save all tool records + category index
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			continue
		}
		if err := wb.Set([]byte("tool:"+record.URN), data); err != nil {
			wb.Cancel()
			return err
		}
		catKey := []byte("cat:" + record.Category + ":" + record.URN)
		if err := wb.Set(catKey, []byte(record.URN)); err != nil {
			wb.Cancel()
			return err
		}
	}

	if err := wb.Flush(); err != nil {
		return err
	}

	if totalNewTools > 0 {
		s.toolsCount.Add(totalNewTools)
	}

	// 3. Post-transaction updates (Search Index and Micro-Cache)
	s.Cache.SetCategories(nil)
	for hash, schema := range schemas {
		s.Cache.Set("schema:"+hash, schema, 2*time.Hour)
	}

	// 🛡️ BATCH INTELLIGENCE HYDRATION
	intelMap := make(map[string]*ToolIntelligence, len(records))
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		for _, record := range records {
			overlayUsageStat(txn, record) // BDG-3: cache/index the live usage count
			if item, err := txn.Get([]byte("intel:" + record.URN)); err == nil {
				itemValueOrWarn(item, func(val []byte) error {
					var intel ToolIntelligence
					if json.Unmarshal(val, &intel) == nil {
						intelMap[record.URN] = &intel
					}
					return nil
				})
			}
		}
		return nil
	})

	var bleveBatch []BleveToolDocument
	for _, record := range records {
		if intel, exists := intelMap[record.URN]; exists {
			record.OverlayIntelligence(intel)
		}
		bleveBatch = append(bleveBatch, ToBleveDoc(record))
		s.Cache.Set("tool:"+record.URN, record, 2*time.Hour)
	}

	if len(bleveBatch) > 0 {
		if err := s.Index.IndexBatch(bleveBatch); err != nil {
			slog.Warn("Failed to update search index for tools in batch", "error", err)
		}
	}

	return nil
}

// SaveTool persists a tool record and triggers a 1:1 Bleve index sync.
// GetToolVector returns the embedding vector persisted under a tool's dedicated
// "vec:" key (IDX-9), or nil if absent (e.g. a record not yet re-saved).
func (s *Store) GetToolVector(urn string) []float32 {
	var vec []float32
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("vec:" + urn))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &vec)
		})
	})
	return vec
}

// usageStat holds the volatile per-tool counters persisted under "usage:<urn>".
// BDG-3: keeping these out of the tool: blob means UpdateToolUsage (the hottest
// path, fired on every proxy call) rewrites a ~tiny key instead of re-marshaling
// the entire record — InputSchema and all — on each increment.
type usageStat struct {
	Count      int64 `json:"c"`
	LastUsedAt int64 `json:"l"`
}

func usageKey(urn string) []byte { return []byte("usage:" + urn) }

// readUsageStat returns the persisted usage counters for urn within txn, if any.
func readUsageStat(txn *badger.Txn, urn string) (usageStat, bool) {
	item, err := txn.Get(usageKey(urn))
	if err != nil {
		return usageStat{}, false
	}
	var u usageStat
	if verr := item.Value(func(val []byte) error { return json.Unmarshal(val, &u) }); verr != nil {
		return usageStat{}, false
	}
	return u, true
}

// overlayUsageStat refreshes a record's volatile counters from the usage: key.
// When the key is absent (an un-migrated record) the record's own blob values are
// kept, so existing data reads correctly until the first UpdateToolUsage migrates it.
func overlayUsageStat(txn *badger.Txn, r *ToolRecord) {
	if u, ok := readUsageStat(txn, r.URN); ok {
		r.UsageCount = u.Count
		r.LastUsedAt = u.LastUsedAt
	}
}

func (s *Store) SaveTool(record *ToolRecord) error {
	s.Cache.Delete(record.Server)
	// IDX-9: persist the large embedding vector under a separate "vec:" key so the
	// hot per-call UpdateToolUsage rewrites only the small tool record. Marshal a
	// copy with Vector cleared so the caller's record is not mutated.
	vec := record.Vector
	recForStore := *record
	recForStore.Vector = nil
	data, err := json.Marshal(&recForStore)
	if err != nil {
		return err
	}

	var isNew bool
	// BDG-8: route through the conflict-retry wrapper, and bump the in-memory counter
	// only AFTER a successful commit (it used to be incremented inside the closure,
	// so a retried/aborted transaction drifted the count upward).
	err = s.UpdateWithRetry(func(txn *badger.Txn) error {
		isNew = false // retry-safe
		if _, gErr := txn.Get([]byte("tool:" + record.URN)); errors.Is(gErr, badger.ErrKeyNotFound) {
			isNew = true
		}
		if err := txn.Set([]byte("tool:"+record.URN), data); err != nil {
			return err
		}
		if len(vec) > 0 {
			vb, mErr := json.Marshal(vec)
			if mErr != nil {
				return mErr
			}
			if err := txn.Set([]byte("vec:"+record.URN), vb); err != nil {
				return err
			}
		} else {
			// BDG-7: clear any previously-stored vector so a re-save without one does
			// not leave a stale embedding behind for GetToolVector to return.
			if err := txn.Delete([]byte("vec:" + record.URN)); err != nil {
				return err
			}
		}

		// Update category index
		catKey := []byte("cat:" + record.Category + ":" + record.URN)
		return txn.Set(catKey, []byte(record.URN))
	})
	if err != nil {
		return err
	}
	if isNew {
		s.toolsCount.Add(1)
	}

	// BDG-3: refresh the volatile counters from the usage: key so the cached record
	// and the Bleve doc reflect the live count, not the (often zero) value a sync
	// caller supplied. The usage: key — not this blob — is the source of truth.
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		overlayUsageStat(txn, record)
		return nil
	})

	// 🛡️ DYNAMIC OVERLAY: Aggregate semantic parameters if they exist for this schema before indexing
	if intel, err := s.GetIntelligence(record.URN); err == nil && intel != nil {
		record.OverlayIntelligence(intel)
	}

	// Update search index
	if err := s.Index.IndexRecord(ToBleveDoc(record)); err != nil {
		slog.Warn("Failed to update search index for tool", "urn", record.URN, "error", err)
	}

	// CACHE update: Lower I/O for subsequent Definition/Schema lookups. BDG-7: cache
	// a copy with Vector cleared so the cached object matches what a cold GetTool
	// returns from the vector-free blob (and doesn't pin the embedding in the cache).
	cached := *record
	cached.Vector = nil
	s.Cache.Set("tool:"+record.URN, &cached, 2*time.Hour)
	// Invalidate category cache (new category could have been added)
	s.Cache.SetCategories(nil)
	return nil
}

// GetIntelligence retrieves the LLM-mapped traits for a particular ToolRecord.
// This data resides under the intel:<urn> namespace permanently.
func (s *Store) GetIntelligence(urn string) (*ToolIntelligence, error) {
	var intel ToolIntelligence
	err := s.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("intel:" + urn))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			return json.Unmarshal(val, &intel)
		})
	})
	if err != nil {
		return nil, err
	}
	return &intel, nil
}

// SaveIntelligence persists semantic knowledge under the intel:<urn> namespace.
// Triggering cache eviction and search re-indexing structurally coordinates orchestrator awareness.
func (s *Store) SaveIntelligence(urn string, intel *ToolIntelligence) error {
	data, err := json.Marshal(intel)
	if err != nil {
		return err
	}
	var isNew bool
	// BDG-8: route through the conflict-retry wrapper and bump the in-memory counter
	// only after a successful commit (it was incremented inside the closure, drifting
	// the count up on an aborted/retried transaction).
	err = s.UpdateWithRetry(func(txn *badger.Txn) error {
		isNew = false // retry-safe
		if _, gErr := txn.Get([]byte("intel:" + urn)); errors.Is(gErr, badger.ErrKeyNotFound) {
			isNew = true
		}
		return txn.Set([]byte("intel:"+urn), data)
	})

	if err != nil {
		return err
	}
	if isNew {
		s.intelCount.Add(1)
	}

	// 🛡️ CACHE-AWARE OVERLAY: merge into a private copy and replace the cache entry
	// rather than mutating the shared cached object in place. In-place mutation races
	// with concurrent GetTool readers and the async indexer below (BDG-1): cached
	// *ToolRecord objects are treated as immutable once published.
	var merged *ToolRecord
	if val, ok := s.Cache.Peek("tool:" + urn); ok {
		if record, ok := val.(*ToolRecord); ok {
			cp := *record
			cp.OverlayIntelligence(intel)
			merged = &cp
			s.Cache.Set("tool:"+urn, merged, 2*time.Hour)
		}
	} else {
		// Cache cold — drop any stale entry; the indexer fetches a fresh record.
		s.Cache.Delete("tool:" + urn)
	}

	// Async Bleve re-index. Use the already-merged private snapshot when we have
	// one; otherwise fetch a fresh (copied) record inside the goroutine.
	s.bgWg.Add(1)
	go func(ctx context.Context, snapshot *ToolRecord) {
		defer s.bgWg.Done()

		// Preempt before fetching
		select {
		case <-ctx.Done():
			return
		default:
		}

		rec := snapshot
		if rec == nil {
			fetched, err := s.GetTool(urn)
			if err != nil || fetched == nil {
				return
			}
			rec = fetched
		}

		// Preempt before indexing
		select {
		case <-ctx.Done():
			return
		default:
		}

		if err := s.Index.IndexRecord(ToBleveDoc(rec)); err != nil {
			slog.Warn("Failed to update search index for intelligence overlay", "urn", urn, "error", err)
		}
	}(s.ctx, merged)
	return nil
}

type metricDelta struct {
	mu         sync.Mutex
	Successes  int
	Total      int
	LatencySum int64
	LastError  string
}

var lastToolSync atomic.Int64

// UpdateToolMetrics modifies the dynamic index weight based on proxy call execution results
// BDG-4: the former `confidence` parameter was silently discarded. The orchestrator
// signal that supplied it defaults to 0 when absent, so feeding it into scoring would
// inject noise; the misleading dead parameter has been removed instead.
func (s *Store) UpdateToolMetrics(urn string, success bool) error {
	delta := metricDeltaOrNew(&s.metricsBuf, urn)
	delta.mu.Lock()
	delta.Total++
	if success {
		delta.Successes++
	}
	delta.mu.Unlock()
	return nil
}

// FlushMetrics synchronizes in-memory tool metric aggregations to BadgerDB.
func (s *Store) FlushMetrics() {
	loaded := s.collectPendingMetrics()
	if len(loaded) == 0 {
		return
	}
	err := s.UpdateWithRetry(func(txn *badger.Txn) error {
		return s.flushLoadedMetrics(txn, loaded)
	})
	if err != nil {
		slog.Error("failed to flush metrics batch", "error", err)
		s.restoreLoadedMetrics(loaded)
		return
	}
	s.schedulePostFlushToolRefresh(loaded)
}

// IncrementToolCalls updates the rolling average latency for a tool after a proxy call.
// Buffers via metricsBuf to eliminate transaction mutex contention under load.
func (s *Store) IncrementToolCalls(urn string, latencyMs int64) {
	delta := metricDeltaOrNew(&s.metricsBuf, urn)
	delta.mu.Lock()
	delta.LatencySum += latencyMs
	// We increment total in UpdateToolMetrics to avoid double-counting if both are called.
	delta.mu.Unlock()
}

// RecordToolError records the most recent error class on a tool's intelligence record.
// Buffers via metricsBuf to eliminate transaction mutex contention under load.
func (s *Store) RecordToolError(urn string, errorClass string) {
	delta := metricDeltaOrNew(&s.metricsBuf, urn)
	delta.mu.Lock()
	delta.LastError = errorClass
	delta.mu.Unlock()
}

// SaveSchema persists a deduplicated tool schema
func (s *Store) SaveSchema(hash string, schema map[string]any) error {
	data, err := json.Marshal(schema)
	if err != nil {
		return err
	}
	compressed, err := util.Compress(data)
	if err != nil {
		slog.Error("database: schema compression failed", "op", "SaveSchema", "hash", hash, "error", err)
		return fmt.Errorf("failed to compress schema: %w", err)
	}
	err = s.DB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("schema:"+hash), compressed)
	})
	if err == nil {
		s.Cache.Set("schema:"+hash, schema, 2*time.Hour)
	}
	return err
}

// GetSchema retrieves a tool schema by hash, using the Micro-Cache to avoid Badger/ZSTD overhead.
func (s *Store) GetSchema(hash string) (map[string]any, error) {
	if hash == "" {
		return nil, badger.ErrKeyNotFound
	}
	// 1. Cache Check
	if val, ok := s.Cache.Get("schema:" + hash); ok {
		if m, ok := val.(map[string]any); ok {
			return m, nil
		}
	}

	var m map[string]any
	err := s.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("schema:" + hash))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			decompressed, err := util.Decompress(val)
			if err != nil {
				return err
			}
			return json.Unmarshal(decompressed, &m)
		})
	})
	if err == nil {
		s.Cache.Set("schema:"+hash, m, 2*time.Hour)
	}
	return m, err
}

// GetTool retrieves a tool record by URN, prioritizing the Micro-Cache to lower read I/O.
func (s *Store) GetTool(urn string) (*ToolRecord, error) {
	// 1. Cache Check (Bastion's best friend)
	if val, ok := s.Cache.Get("tool:" + urn); ok {
		if record, ok := val.(*ToolRecord); ok {
			// BDG-1: hand callers an independent shallow copy. The cached object is
			// shared; returning it by reference lets callers that stamp transient
			// fields (SearchTools scoring, usage bumps) race with — and contaminate —
			// concurrent readers of the same cached record.
			rec := *record
			return &rec, nil
		}
	}

	var record ToolRecord
	err := s.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("tool:" + urn))
		if err != nil {
			return err
		}
		if uErr := item.Value(func(val []byte) error {
			return json.Unmarshal(val, &record)
		}); uErr != nil {
			return uErr
		}
		// BDG-3: volatile counters live under usage:<urn>; overlay them in the same txn.
		overlayUsageStat(txn, &record)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// 🛡️ DYNAMIC OVERLAY: Aggregate semantic parameters if they exist for this schema
	if intel, err := s.GetIntelligence(urn); err == nil && intel != nil {
		record.OverlayIntelligence(intel)
	}

	s.HydrateToolSchema(&record)

	// Pop the cache for next time
	s.Cache.Set("tool:"+urn, &record, 2*time.Hour)
	return &record, nil
}

// ResolveToolSchema returns the authoritative JSON schema for a tool record,
// preferring embedded InputSchema and falling back to schema:{hash} storage.
func (s *Store) ResolveToolSchema(record *ToolRecord) map[string]any {
	if record == nil {
		return nil
	}
	if record.InputSchema != nil {
		return record.InputSchema
	}
	if record.SchemaHash == "" {
		return nil
	}
	schema, err := s.GetSchema(record.SchemaHash)
	if err != nil || schema == nil {
		return nil
	}
	return schema
}

// HydrateToolSchema attaches resolved schema and precomputed zero-values in-memory.
func (s *Store) HydrateToolSchema(record *ToolRecord) {
	if record == nil {
		return
	}
	if record.InputSchema == nil {
		record.InputSchema = s.ResolveToolSchema(record)
	}
	if len(record.ZeroValues) == 0 && record.InputSchema != nil {
		record.ZeroValues = ComputeZeroValues(record.InputSchema)
	}
}

// ComputeZeroValues natively intercepts a JSONSchema parameter map extracting required structures
// substituting default or empty primitive proxies dynamically.
func ComputeZeroValues(schema map[string]any) map[string]any {
	zeroVals := make(map[string]any)
	if schema == nil {
		return zeroVals
	}

	reqRaw, ok := schema["required"]
	if !ok {
		return zeroVals
	}
	requiredArgs, ok := reqRaw.([]any)
	if !ok {
		return zeroVals
	}

	propsRaw, ok := schema["properties"]
	var props map[string]any
	if ok {
		props, _ = mapFromAny(propsRaw)
	}

	for _, reqIntf := range requiredArgs {
		key, ok := reqIntf.(string)
		if !ok {
			continue
		}

		var propType string
		if props != nil {
			if propDefRaw, hasProp := props[key]; hasProp {
				if propDef, ok := propDefRaw.(map[string]any); ok {
					if defRaw, hasDef := propDef["default"]; hasDef {
						zeroVals[key] = defRaw
						continue
					}
					if typeRaw, hasType := propDef["type"]; hasType {
						if typeStr, ok := typeRaw.(string); ok {
							propType = typeStr
						}
					}
				}
			}
		}

		switch propType {
		case "string":
			zeroVals[key] = ""
		case "integer", "number":
			zeroVals[key] = 0
		case "boolean":
			zeroVals[key] = false
		case "array":
			zeroVals[key] = []any{}
		case schemaTypeObject:
			zeroVals[key] = map[string]any{}
		default:
			zeroVals[key] = "" // fast, safe string natively
		}
	}
	return zeroVals
}

// RecordSearchGraphCompleteness stores HNSW node count / Bleve tool doc ratio for health dashboards.
func RecordSearchGraphCompleteness(s *Store) {
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() || s == nil || s.Index == nil {
		return
	}
	bleveCount, err := s.Index.ToolCount()
	if err != nil || bleveCount == 0 {
		return
	}
	ratio := float64(e.Len()) / float64(bleveCount)
	telemetry.SearchMetrics.GraphCompletenessRatio.Store(math.Float64bits(ratio))
}

// SearchTools performs Keyword search on tool names and descriptions using Bleve index with optional category domain filtering.
// Results are re-ranked by blending BM25 relevance with usage frequency.
func (s *Store) SearchTools(ctx context.Context, query string, category string, serverConstraint string, scoreThreshold float64, alpha float64, domain SearchDomain, skipVector bool) ([]*ToolRecord, error) {
	start := time.Now()
	defer func() {
		telemetry.SearchMetrics.TotalLatencyMs.Add(time.Since(start).Milliseconds())
	}()

	if query == "" {
		var results []*ToolRecord
		err := s.DB.View(func(txn *badger.Txn) error {
			// Preload intelMap to prevent N+1 query storm
			intelMap := make(map[string]*ToolIntelligence)
			intelIt := txn.NewIterator(badger.DefaultIteratorOptions)
			intelPrefix := []byte("intel:")
			for intelIt.Seek(intelPrefix); intelIt.ValidForPrefix(intelPrefix); intelIt.Next() {
				select {
				case <-ctx.Done():
					intelIt.Close()
					return ctx.Err()
				default:
				}
				item := intelIt.Item()
				urn := strings.TrimPrefix(string(item.Key()), "intel:")
				itemValueOrWarn(item, func(val []byte) error {
					var ti ToolIntelligence
					if json.Unmarshal(val, &ti) == nil {
						intelMap[urn] = &ti
					}
					return nil
				})
			}
			intelIt.Close()

			it := txn.NewIterator(badger.DefaultIteratorOptions)
			defer it.Close()
			prefix := []byte("tool:")
			for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
				select {
				case <-ctx.Done():
					return ctx.Err()
				default:
				}
				item := it.Item()
				valErr := item.Value(func(val []byte) error {
					var r ToolRecord
					if err := json.Unmarshal(val, &r); err == nil {
						if !recordAllowedInSearch(&r, query, category, serverConstraint, domain) {
							return nil
						}
						// 🛡️ DYNAMIC OVERLAY: Overlay from map instead of nested GetIntelligence view
						if intel, exists := intelMap[r.URN]; exists {
							r.OverlayIntelligence(intel)
						}
						results = append(results, &r)
					}
					return nil
				})
				if valErr != nil && !errors.Is(valErr, badger.ErrDiscardedTxn) {
					slog.Debug("failed to parse index tool", "error", valErr)
				}
			}
			return nil
		})
		return results, err
	}

	return s.searchToolsWithQuery(ctx, query, category, serverConstraint, scoreThreshold, alpha, domain, skipVector)
}

// applyLexicalBoosts applies action verb boosts and negative trigger penalties to a blended score.
// Checks ALL query tokens for verb presence (not just tokens[0]) to catch "search tools" → "search".
func applyLexicalBoosts(blended float64, r *ToolRecord, queryLower string) float64 {
	tokens := strings.Fields(queryLower)
	if len(tokens) > 0 {
		actionVerbs := []string{"get", "delete", actionVerbSearch, "list", "read", "write", "create", "update"}
		nameLower := strings.ToLower(r.Name)
	verbLoop:
		for _, token := range tokens {
			for _, v := range actionVerbs {
				if token == v && strings.Contains(nameLower, v) {
					blended *= 1.15
					telemetry.SearchMetrics.ActionVerbBoostsApplied.Add(1)
					break verbLoop
				}
			}
		}
	}

	// 🛡️ Apply Semantic Negative Triggers Penalty (word-boundary matching)
	if matchesNegativeTriggers(queryLower, r.NegativeTriggers) {
		blended *= 0.3
	}
	return blended
}

// fallbackTextMatch returns true when any query word appears in searchable tool text fields.
func fallbackTextMatch(r *ToolRecord, words []string, queryLower string) bool {
	if len(words) == 0 {
		return strings.Contains(strings.ToLower(r.Name), queryLower) ||
			strings.Contains(strings.ToLower(r.URN), queryLower)
	}

	haystacks := []string{
		strings.ToLower(r.Name),
		strings.ToLower(r.URN),
		strings.ToLower(r.Description),
		strings.ToLower(r.Intent),
		strings.ToLower(r.LiteSummary),
		strings.ToLower(strings.Join(r.SyntheticIntents, " ")),
		strings.ToLower(strings.Join(r.LexicalTokens, " ")),
		strings.ToLower(strings.Join(r.ParameterNames, " ")),
		strings.ToLower(strings.Join(r.EnumValues, " ")),
		strings.ToLower(strings.Join(r.Requires, " ")),
		strings.ToLower(strings.Join(r.Triggers, " ")),
		strings.ToLower(r.Role),
	}
	for _, word := range words {
		if len(word) < 2 {
			continue
		}
		for _, hay := range haystacks {
			if hay != "" && strings.Contains(hay, word) {
				return true
			}
		}
	}
	return false
}

// fallbackMatchScore weights substring matches for fallback ranking.
func fallbackMatchScore(r *ToolRecord, words []string, queryLower string) float64 {
	if r == nil {
		return 0
	}
	var score float64
	nameLower := strings.ToLower(r.Name)
	urnLower := strings.ToLower(r.URN)
	descLower := strings.ToLower(r.Description)
	intentLower := strings.ToLower(r.Intent)
	synthLower := strings.ToLower(strings.Join(r.SyntheticIntents, " "))

	matchWord := func(word string, hay string, weight float64) {
		if len(word) < 2 || hay == "" {
			return
		}
		if strings.Contains(hay, word) {
			score += weight
		}
	}

	if len(words) == 0 {
		matchWord(queryLower, nameLower, 3)
		matchWord(queryLower, urnLower, 2)
	} else {
		for _, word := range words {
			matchWord(word, nameLower, 3)
			matchWord(word, urnLower, 2)
			matchWord(word, intentLower, 2)
			matchWord(word, synthLower, 2)
			matchWord(word, descLower, 1)
		}
	}
	if score > 10 {
		score = 10
	}
	return score / 10.0
}

// SearchToolsFallback implements a pure linear substring scan across the BadgerDB key space.
// Used exclusively as a fallback when the Bleve search index returns zero hits or misses the threshold.
func (s *Store) SearchToolsFallback(query string, category string, serverConstraint string, domain SearchDomain) ([]*ToolRecord, error) {
	var results []*ToolRecord
	err := s.DB.View(func(txn *badger.Txn) error {
		intelMap := loadIntelMapInTxn(txn)
		scanFallbackToolsInTxn(txn, intelMap, query, category, serverConstraint, domain, &results)
		return nil
	})
	sortToolRecordsByConfidence(results)
	return results, err
}

// GetCategories returns all unique categories across all tools.
func (s *Store) GetCategories() ([]string, error) {
	// 1. Cache Check
	if cats, ok := s.Cache.GetCategories(); ok && len(cats) > 0 {
		return cats, nil
	}

	categories := make(map[string]struct{})
	err := s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("cat:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.Key() // cat:<category>:<server>:<name>
			// BDG-10: SplitN(3) so the multi-colon URN tail isn't split needlessly;
			// parts[1] is the category (which by convention contains no colon).
			parts := strings.SplitN(string(key), ":", 3)
			if len(parts) >= 2 {
				categories[parts[1]] = struct{}{}
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	results := make([]string, 0, len(categories))
	for k := range categories {
		results = append(results, k)
	}

	// Populate cache
	s.Cache.SetCategories(results)
	return results, nil
}

// SaveRawResource stores large tool outputs
func (s *Store) SaveRawResource(id string, data []byte) error {
	compressed, err := util.Compress(data)
	if err != nil {
		return err
	}

	return s.DB.Update(func(txn *badger.Txn) error {
		// 🛡️ FIX: Add TTL to prevent unbounded disk growth from cached proxy results.
		e := badger.NewEntry([]byte("raw:"+id), compressed).WithTTL(1 * time.Hour)
		return txn.SetEntry(e)
	})
}

// GetRawResource retrieves large tool outputs
func (s *Store) GetRawResource(id string) ([]byte, error) {
	var compressed []byte
	err := s.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("raw:" + id))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			compressed = append([]byte{}, val...)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return util.Decompress(compressed)
}

// UpdateToolUsage increments the usage counter for a tool.
// BDG-3: the counters live under a dedicated tiny "usage:<urn>" key, so this hot
// path rewrites only ~30 bytes instead of re-marshaling the whole tool record
// (InputSchema included) on every proxy call. GetTool overlays the value back.
func (s *Store) UpdateToolUsage(urn string) {
	var newCount, newLast int64
	err := s.UpdateWithRetry(func(txn *badger.Txn) error {
		// Don't track usage for a tool that no longer exists (purged mid-sync).
		item, err := txn.Get([]byte("tool:" + urn))
		if err != nil {
			if errors.Is(err, badger.ErrKeyNotFound) {
				return nil // SILENT: key was likely purged during a sync/rename
			}
			return err
		}

		u, ok := readUsageStat(txn, urn)
		if !ok {
			// Migration: seed the counter once from the blob's historical value so
			// pre-split records don't reset to zero on their first post-upgrade call.
			itemValueOrWarn(item, func(val []byte) error {
				var r ToolRecord
				if json.Unmarshal(val, &r) == nil {
					u.Count = r.UsageCount
				}
				return nil
			})
		}

		u.Count++
		u.LastUsedAt = time.Now().Unix()
		newCount, newLast = u.Count, u.LastUsedAt
		data, mErr := json.Marshal(u)
		if mErr != nil {
			return mErr
		}
		return txn.Set(usageKey(urn), data)
	})

	if err != nil {
		slog.Warn("Failed to update tool usage stats", "urn", urn, "error", err)
		return
	}

	// 🛡️ CACHE-AWARE: keep the cached record's counters fresh so a cache-hit GetTool
	// (which returns early, before the usage overlay) doesn't serve a stale count.
	// Replace with an updated copy rather than mutating the shared object in place.
	if val, ok := s.Cache.Peek("tool:" + urn); ok {
		if record, ok := val.(*ToolRecord); ok {
			cp := *record
			cp.UsageCount = newCount
			cp.LastUsedAt = newLast
			s.Cache.Set("tool:"+urn, &cp, 2*time.Hour)
		}
	}
}

// ReindexAllTools performs a full scan of Badger and updates the Bleve index.
// Used during lazy boot re-indexing and auto-heal recovery.
func (s *Store) ReindexAllTools() error {
	return s.DB.View(func(txn *badger.Txn) error {
		// 🛡️ PERF: Pre-load all intelligence records in a single scan to
		// eliminate N+1 GetIntelligence reads (one per tool). With ~200 tools,
		// this replaces ~200 Badger transactions with a single iterator pass.
		intelMap := loadIntelMapInTxn(txn)

		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("tool:")
		var batch []BleveToolDocument

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var r ToolRecord
				if err := json.Unmarshal(val, &r); err == nil {
					// 🛡️ DYNAMIC OVERLAY: Aggregate semantic parameters for complete Bleve indexing
					if intel, ok := intelMap[r.URN]; ok && intel != nil {
						r.OverlayIntelligence(intel)
					}
					batch = append(batch, ToBleveDoc(&r))

					// Flush batch when it hits 1000 to cap memory footprint
					if len(batch) >= 1000 {
						if err := s.Index.IndexBatch(batch); err != nil {
							slog.Warn("Failed to re-index tool batch in search index", "error", err)
						}
						batch = make([]BleveToolDocument, 0, 1000)
					}
				}
				return nil
			})
			if err != nil {
				slog.Error("Failed to read tool value during re-indexing", "key", string(item.Key()), "error", err)
			}
		}

		// Flush remaining batch elements
		if len(batch) > 0 {
			if err := s.Index.IndexBatch(batch); err != nil {
				slog.Warn("Failed to re-index final tool batch in search index", "error", err)
			}
		}

		return nil
	})
}

// WarmCache pre-loads all tool records from Badger into the L1 RegistryCache.
// Called during boot to eliminate first-query cold misses. Uses a single Badger
// View transaction with pre-loaded intelligence for the overlay merge.
func (s *Store) WarmCache() int {
	var warmed int
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		intelMap := loadIntelMapInTxn(txn)

		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			itemValueOrWarn(item, func(val []byte) error {
				var r ToolRecord
				if json.Unmarshal(val, &r) == nil {
					if intel, ok := intelMap[r.URN]; ok && intel != nil {
						r.OverlayIntelligence(intel)
					}
					if len(r.ZeroValues) == 0 {
						r.ZeroValues = ComputeZeroValues(r.InputSchema)
					}
					s.Cache.Set("tool:"+r.URN, &r, 2*time.Hour)
					warmed++
				}
				return nil
			})
		}
		return nil
	})
	return warmed
}

// PurgeServerTools removes all tool records for a specific server.
func (s *Store) PurgeServerTools(serverName string) error {
	var purgedTools int64
	var purgedIntel int64

	var plan serverPurgePlan
	err := s.DB.View(func(txn *badger.Txn) error {
		plan = collectServerPurgePlan(txn, s, serverName)
		return nil
	})
	toDelete := plan.toDelete
	toolURNs := plan.toolURNs

	if err == nil && len(toDelete) > 0 {
		// Write phase: delete via a WriteBatch (auto-flushes by size).
		wb := s.DB.NewWriteBatch()
		for _, key := range toDelete {
			ks := string(key)
			if strings.HasPrefix(ks, "tool:") {
				purgedTools++
			} else if strings.HasPrefix(ks, "intel:") {
				purgedIntel++
			}
			if dErr := wb.Delete(key); dErr != nil {
				wb.Cancel()
				err = dErr
				break
			}
		}
		if err == nil {
			err = wb.Flush()
		}
		if err == nil {
			// Remove purged tools from the search index (outside the Badger batch).
			for _, urn := range toolURNs {
				if iErr := s.Index.DeleteRecord(urn); iErr != nil {
					slog.Warn("Failed to remove purged tool from search index", "urn", urn, "error", iErr)
				}
			}
			s.Cache.SetCategories(nil) // Invalidate category cache after purge
			slog.Info("purged tools for server", "server", serverName, "keys_deleted", len(toDelete))
		}
	}

	if err == nil {
		if purgedTools > 0 {
			s.toolsCount.Add(-purgedTools)
		}
		if purgedIntel > 0 {
			s.intelCount.Add(-purgedIntel)
		}

		// 🛡️ GAP 1 FIX: Clear the sync hash so a re-added server doesn't hit a stale hash-gate.
		s.updateWithRetryOrWarn(func(txn *badger.Txn) error {
			return txn.Delete([]byte("sync_hash:" + serverName))
		})

		// 🛡️ GAP 3 FIX: Reconcile HNSW graph via safe rebuild instead of per-document Delete().
		// graph.Delete() corrupts HNSW layer topology; PruneOrphanedNodes rebuilds from scratch.
		if purgedTools > 0 {
			if e := vector.GetEngine(); e != nil && e.VectorEnabled() {
				validURNs := s.GetAllToolURNs()
				if pruned := e.PruneOrphanedNodes(validURNs); pruned > 0 {
					slog.Info("database: reconciled HNSW graph after server purge",
						"server", serverName, "vector_nodes_pruned", pruned)
				}
			}
		}
	}
	return err
}

// PurgeStaleServerTools performs a delta-aware removal of tool records for a server.
// Only keys NOT present in validURNs are deleted. This is the safe counterpart to
// PurgeServerTools — designed for background execution after BatchSaveTools has already
// upserted the current tool set, preventing any zero-tools window.
func (s *Store) PurgeStaleServerTools(serverName string, validURNs []string) error {
	validMap := make(map[string]bool, len(validURNs))
	for _, urn := range validURNs {
		validMap[urn] = true
	}

	var purgedCount int64
	var purgedIntel int64
	var purgedURNs []string // IDX-8: tombstone the HNSW docs once, after the txn

	err := s.UpdateWithRetry(func(txn *badger.Txn) error {
		purgedCount = 0 // Reset counter for retry safety
		purgedIntel = 0
		purgedURNs = nil // reset for retry safety
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("tool:" + serverName + ":")
		type staleEntry struct {
			key    []byte
			urn    string
			catKey []byte
		}
		var toDelete []staleEntry

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			keyStr := string(item.Key())
			urn := keyStr[5:] // strip "tool:" prefix

			if validMap[urn] {
				continue // tool is current, keep it
			}

			entry := staleEntry{
				key: item.KeyCopy(nil),
				urn: urn,
			}

			// Extract category for cat: key cleanup
			itemValueOrWarn(item, func(val []byte) error {
				var r ToolRecord
				if err := json.Unmarshal(val, &r); err == nil {
					entry.catKey = []byte("cat:" + r.Category + ":" + r.URN)
				}
				return nil
			})

			toDelete = append(toDelete, entry)
		}

		for _, entry := range toDelete {
			purgedCount++
			if err := txn.Delete(entry.key); err != nil {
				slog.Warn("failed to delete stale tool key", "key", string(entry.key), "error", err)
			}
			if entry.catKey != nil {
				deleteKeyOrWarn(txn, entry.catKey)
			}

			intelKey := []byte("intel:" + entry.urn)
			if _, gErr := txn.Get(intelKey); gErr == nil {
				purgedIntel++
				deleteKeyOrWarn(txn, intelKey)
			}
			// Remove from search index
			if err := s.Index.DeleteRecord(entry.urn); err != nil {
				slog.Warn("failed to remove stale tool from search index", "urn", entry.urn, "error", err)
			}
			// IDX-9: drop the separately-stored embedding vector.
			deleteKeyOrWarn(txn, []byte("vec:"+entry.urn))
			// BDG-3: drop the separately-stored usage counters.
			deleteKeyOrWarn(txn, []byte("usage:"+entry.urn))
			// IDX-8: collect for a single batched HNSW tombstone after the txn.
			purgedURNs = append(purgedURNs, entry.urn)
			// Clear cache
			s.Cache.Delete("tool:" + entry.urn)
		}

		if len(toDelete) > 0 {
			s.Cache.SetCategories(nil) // Invalidate category cache
			slog.Info("database: purged stale tools via delta-aware sweep", "server", serverName, "stale_keys", len(toDelete))
		}
		return nil
	})

	if err == nil {
		if purgedCount > 0 {
			s.toolsCount.Add(-purgedCount)
		}
		if purgedIntel > 0 {
			s.intelCount.Add(-purgedIntel)
		}
		// IDX-8: one batched HNSW tombstone + meta write for the whole sweep.
		if e := vector.GetEngine(); e != nil && len(purgedURNs) > 0 {
			e.DeleteDocuments(purgedURNs...)
		}
	}

	return err
}

// GetServerSyncHash retrieves the composite tool hash for a server from the last sync.
// Returns empty string if no hash is stored (first boot or schema change).
func (s *Store) GetServerSyncHash(server string) string {
	var hash string
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		item, err := txn.Get([]byte("sync_hash:" + server))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			hash = string(val)
			return nil
		})
	})
	return hash
}

// SaveServerSyncHash persists the composite tool hash for a server.
func (s *Store) SaveServerSyncHash(server, hash string) {
	s.updateWithRetryOrWarn(func(txn *badger.Txn) error {
		return txn.Set([]byte("sync_hash:"+server), []byte(hash))
	})
}

// GetStaleServers returns a list of server names that have tools in the DB but are not in the activeNames list.
func (s *Store) GetStaleServers(activeNames []string) ([]string, error) {
	activeMap := make(map[string]bool, len(activeNames))
	for _, name := range activeNames {
		activeMap[name] = true
	}

	staleServers := make(map[string]bool)
	err := s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := string(item.Key()) // format: tool:<server>:<name>
			parts := strings.Split(key, ":")
			if len(parts) >= 3 {
				serverName := parts[1]
				if !activeMap[serverName] {
					staleServers[serverName] = true
				}
			}
		}
		return nil
	})

	var stale []string
	for k := range staleServers {
		stale = append(stale, k)
	}
	return stale, err
}

// PurgeOrphanedServers finds and removes all tool records for servers not in the active list.
func (s *Store) PurgeOrphanedServers(activeNames []string) error {
	stale, err := s.GetStaleServers(activeNames)
	if err != nil {
		return err
	}
	for _, serverName := range stale {
		slog.Info("database: sweeping orphaned server records", "server", serverName)
		if err := s.PurgeServerTools(serverName); err != nil {
			slog.Warn("database: failed to purge orphaned server tools", "server", serverName, "error", err)
		}
		if err := s.PurgeServerIntelligence(serverName); err != nil {
			slog.Warn("database: failed to purge orphaned server intelligence", "server", serverName, "error", err)
		}
	}
	return nil
}

// PruneOrphanedIntelligence safely performs a delta-aware removal of orphaned semantic weights.
// It deletes intel:<serverName>:* keys that are NOT present in the validURNs slice.
func (s *Store) PruneOrphanedIntelligence(serverName string, validURNs []string) error {
	validMap := make(map[string]bool, len(validURNs))
	for _, urn := range validURNs {
		validMap[urn] = true
	}

	var purgedIntel int64

	err := s.UpdateWithRetry(func(txn *badger.Txn) error {
		purgedIntel = 0

		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("intel:" + serverName + ":")
		var toDelete [][]byte

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			keyStr := string(item.Key())

			// Format: intel:<server>:<tool>
			urn := keyStr[6:] // strip "intel:"

			if !validMap[urn] {
				toDelete = append(toDelete, item.KeyCopy(nil))
			}
		}

		for _, key := range toDelete {
			purgedIntel++
			if err := txn.Delete(key); err != nil {
				slog.Warn("failed to drop orphaned intelligence key", "key", string(key), "error", err)
			}
		}

		if len(toDelete) > 0 {
			slog.Info("database: gracefully pruned orphaned intelligence states", "server", serverName, "keys_deleted", len(toDelete))
		}
		return nil
	})

	if err == nil && purgedIntel > 0 {
		s.intelCount.Add(-purgedIntel)
	}
	return err
}

// PurgeServerIntelligence completely sweeps all LLM semantic states for a particular server namespace.
func (s *Store) PurgeServerIntelligence(serverName string) error {
	var purgedIntel int64

	err := s.UpdateWithRetry(func(txn *badger.Txn) error {
		purgedIntel = 0

		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("intel:" + serverName + ":")
		var toDelete [][]byte

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			toDelete = append(toDelete, item.KeyCopy(nil))
		}

		for _, key := range toDelete {
			purgedIntel++
			if err := txn.Delete(key); err != nil {
				slog.Warn("failed to drop server intelligence key", "key", string(key), "error", err)
			}
		}

		if len(toDelete) > 0 {
			slog.Info("database: wiped intelligence states for dropped server", "server", serverName, "keys_deleted", len(toDelete))
		}
		return nil
	})

	if err == nil && purgedIntel > 0 {
		s.intelCount.Add(-purgedIntel)
	}
	return err
}

// ReconcileMetrics forces full cross-namespace parity between tool: and intel: keys.
// It deletes orphaned intel: keys that have no corresponding tool: key, then recalibrates
// the atomic counters from actual database state. This is the authoritative consistency gate.
func (s *Store) ReconcileMetrics() (orphansDeleted int64, err error) {
	// Phase 1: Collect all valid tool URNs in a read-only scan.
	validTools := make(map[string]bool)
	err = s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			keyStr := string(it.Item().Key())
			urn := keyStr[5:] // strip "tool:" → "server:name"
			validTools[urn] = true
		}
		return nil
	})
	if err != nil {
		slog.Error("database: reconcile tool namespace scan failed", "op", "ReconcileOrphans", "error", err)
		return 0, fmt.Errorf("reconcile: failed to scan tool namespace: %w", err)
	}

	// Phase 2: Find orphaned intel: keys that have no matching tool: key.
	var orphanedIntelKeys [][]byte
	err = s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("intel:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			keyStr := string(it.Item().Key())
			urn := keyStr[6:] // strip "intel:" → "server:name"
			if !validTools[urn] {
				orphanedIntelKeys = append(orphanedIntelKeys, it.Item().KeyCopy(nil))
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("database: reconcile intel namespace scan failed", "op", "ReconcileOrphans", "error", err)
		return 0, fmt.Errorf("reconcile: failed to scan intel namespace: %w", err)
	}

	// Phase 3: Delete orphaned intel keys.
	if len(orphanedIntelKeys) > 0 {
		err = s.UpdateWithRetry(func(txn *badger.Txn) error {
			for _, key := range orphanedIntelKeys {
				if delErr := txn.Delete(key); delErr != nil {
					slog.Warn("reconcile: failed to delete orphaned intel key", "key", string(key), "error", delErr)
				}
			}
			return nil
		})
		if err != nil {
			return 0, fmt.Errorf("reconcile: failed to purge orphaned intel keys: %w", err)
		}
		orphansDeleted = int64(len(orphanedIntelKeys))
	}

	// Phase 4: Recalibrate atomic counters from actual key counts.
	toolCount := countKeysOrZero(s, "tool:")
	intelCount := countKeysOrZero(s, "intel:")
	s.toolsCount.Store(int64(toolCount))
	s.intelCount.Store(int64(intelCount))

	slog.Info("database: reconciliation complete",
		"orphans_deleted", orphansDeleted,
		"tool_count", toolCount,
		"intel_count", intelCount,
	)
	return orphansDeleted, nil
}

// HasServerTools checks if any tools exist for the given server using a prefix search.
func (s *Store) HasServerTools(serverName string) bool {
	found := false
	err := s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:" + serverName + ":")
		it.Seek(prefix)
		if it.ValidForPrefix(prefix) {
			found = true
		}
		return nil
	})
	if err != nil {
		slog.Error("Failed to check for server tools existence", "server", serverName, "error", err)
	}
	return found
}

// GetServerToolCount returns the number of tools indexed for a specific server.
func (s *Store) GetServerToolCount(serverName string) int {
	count := 0
	err := s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:" + serverName + ":")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			count++
		}
		return nil
	})
	if err != nil {
		slog.Error("Failed to count server tools", "server", serverName, "error", err)
	}
	return count
}

// GetServerToolsNatively directly scans the LSM tree for a server's tools, bypassing Bleve entirely.
func (s *Store) GetServerToolsNatively(serverName string, limit int) ([]*ToolRecord, error) {
	var tools []*ToolRecord
	err := s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:" + serverName + ":")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			err := it.Item().Value(func(val []byte) error {
				var r ToolRecord
				if err := json.Unmarshal(val, &r); err != nil {
					return err
				}
				tools = append(tools, &r)
				return nil
			})
			if err != nil {
				return err
			}
			if limit > 0 && len(tools) >= limit {
				break
			}
		}
		return nil
	})
	return tools, err
}

// GetAllToolURNs returns a map of all tool URNs currently stored in BadgerDB.
// Uses key-only iteration (PrefetchValues=false) for minimal overhead.
func (s *Store) GetAllToolURNs() map[string]bool {
	urns := make(map[string]bool)
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			key := string(it.Item().Key())
			urn := key[5:] // strip "tool:" prefix
			urns[urn] = true
		}
		return nil
	})
	return urns
}

// SaveLog persists a log entry with a TTL for self-cleaning.
func (s *Store) SaveLog(entry []byte, ttl time.Duration) error {
	// Use UnixNano for chronological sorting (ascending by default in Badger)
	timestamp := time.Now().UnixNano()
	key := fmt.Appendf(nil, "log:%020d", timestamp)
	return s.DB.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry(key, entry).WithTTL(ttl)
		return txn.SetEntry(e)
	})
}

// GetLogs retrieves the most recent log entries up to maxLines.
func (s *Store) GetLogs(maxLines int) ([]string, error) {
	var logs []string
	err := s.DB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Reverse = true // Latest logs first
		it := txn.NewIterator(opts)
		defer it.Close()

		prefix := []byte("log:")
		// Seek to the very end of the log prefix range
		it.Seek([]byte("log:\xff"))

		for ; it.ValidForPrefix(prefix) && len(logs) < maxLines; it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				logs = append(logs, string(val))
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	// Reverse the slice back to chronological order (Ascending)
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	return logs, err
}

// DiagnosticStats is undocumented but satisfies standard structural requirements.
type DiagnosticStats struct {
	TotalKeys       int    `json:"total_keys"`
	LSMSize         int64  `json:"lsm_size_bytes"`
	VLogSize        int64  `json:"vlog_size_bytes"`
	TTLKeysTotal    int    `json:"ttl_keys_total"`
	TTLKeysUnder1H  int    `json:"ttl_keys_under_1h"`
	TTLKeysUnder24H int    `json:"ttl_keys_under_24h"`
	SyncState       string `json:"sync_state"`
}

// GetExtendedDiagnostics streams the Badger DB evaluating TTLs and Bleve index parity
func (s *Store) GetExtendedDiagnostics() (*DiagnosticStats, error) {
	stats := &DiagnosticStats{
		SyncState: "SYNCED",
	}

	// 1. Get raw disk size sizes
	lsm, vlog := s.DB.Size()
	stats.LSMSize = lsm
	stats.VLogSize = vlog

	// 2. Iterate keys for TTLs and sync parity
	now := safeUint64FromInt(int(time.Now().Unix()))
	oneHour := now + 3600
	oneDay := now + 86400

	err := s.DB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false // High speed iteration
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()
			key := string(item.Key())

			if strings.HasPrefix(key, "tool:") {
				stats.TotalKeys++
			}

			// TTL analysis
			if expires := item.ExpiresAt(); expires > 0 {
				stats.TTLKeysTotal++
				if expires <= oneHour {
					stats.TTLKeysUnder1H++
				}
				if expires <= oneDay {
					stats.TTLKeysUnder24H++
				}
			}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	// 3. Bleve DocCount Parity
	if s.Index != nil {
		toolCount, err := s.Index.ToolCount()
		if err == nil {
			if toolCount != safeUint64FromInt(stats.TotalKeys) {
				stats.SyncState = "OUT_OF_SYNC"
			}
		} else {
			stats.SyncState = "INDEX_ERROR"
		}
	} else {
		stats.SyncState = "INDEX_UNAVAILABLE"
	}

	// Update telemetry global sync state
	switch stats.SyncState {
	case "OUT_OF_SYNC":
		telemetry.SyncOutOfSync.Store(true)
	case "SYNCED":
		telemetry.SyncOutOfSync.Store(false)
	}

	return stats, nil
}

// SaveTrigger persists a keyword-to-server mapping for predictive discovery.
func (s *Store) SaveTrigger(keyword, server string) error {
	return s.DB.Update(func(txn *badger.Txn) error {
		return txn.Set([]byte("trigger:"+keyword), []byte(server))
	})
}

// SaveWithTTL saves a key-value pair with a time-to-live.
func (s *Store) SaveWithTTL(key string, val []byte, ttl time.Duration) error {
	return s.DB.Update(func(txn *badger.Txn) error {
		e := badger.NewEntry([]byte(key), val).WithTTL(ttl)
		return txn.SetEntry(e)
	})
}

// PopulateDefaultTriggers seeds the trigger DB with keyword→server mappings.
// This replaces the hardcoded isPreferred() function with data-driven steering.
// Safe to call multiple times — triggers are upserted (last write wins).
func (s *Store) PopulateDefaultTriggers() {
	defaults := map[string]string{
		// CI/CD & Deployment
		"deploy":               serverGlab,
		triggerKeywordPipeline: serverGlab,
		"ci":                   serverGlab,
		"cd":                   serverGlab,
		"release":              serverGlab,
		// Debugging & Maintenance
		"fix":              serverGoModernizer,
		"bug":              serverGoModernizer,
		"debug":            serverGoModernizer,
		triggerKeywordTest: serverGoModernizer,
		// Refactoring & Clean Code
		"refactor":  serverGoModernizer,
		"modernize": serverGoModernizer,
		"clean":     serverGoModernizer,
		// External Knowledge & Research
		actionVerbSearch: serverDdgSearch,
		"web":            serverDdgSearch,
		"lookup":         serverDdgSearch,
		"research":       serverDdgSearch,
		// Architecture & Planning
		"design":       serverBrainstorm,
		"architecture": serverBrainstorm,
		"plan":         serverBrainstorm,
		"critique":     serverBrainstorm,
		// Sequential Thinking
		"think":      serverSeqThinking,
		"thinking":   serverSeqThinking,
		"reason":     serverSeqThinking,
		"reasoning":  serverSeqThinking,
		"analyze":    serverSeqThinking,
		"sequential": serverSeqThinking,
		// Skills & Workflow
		"skill":     serverMagicskills,
		"workflow":  serverMagicskills,
		"bootstrap": serverMagicskills,
		// Context Preservation & Memory
		triggerKeywordMemory: serverRecall,
		serverRecall:         serverRecall,
		"remember":           serverRecall,
		"context":            serverRecall,
		// File Operations
		"file":             serverFilesystem,
		"directory":        serverFilesystem,
		"folder":           serverFilesystem,
		triggerKeywordPath: serverFilesystem,
		// Version Control
		"git":    serverGit,
		"commit": serverGit,
		"branch": serverGit,
		"merge":  serverGit,
		"diff":   serverGit,
		// ADR-0016: Niche Server Coverage
		"madr":       serverEvolvePlan,
		"evolve":     serverEvolvePlan,
		"adr":        serverEvolvePlan,
		"socratic":   serverSocraticThinker,
		"dialectic":  serverSocraticThinker,
		"debate":     serverSocraticThinker,
		"threat":     serverBrainstorm,
		"stride":     serverBrainstorm,
		"github":     serverGithub,
		"gitlab":     serverGlab,
		"duckduckgo": serverDdgSearch,
		"internet":   serverDdgSearch,
	}
	err := s.DB.Update(func(txn *badger.Txn) error {
		for keyword, server := range defaults {
			if err := txn.Set([]byte("trigger:"+keyword), []byte(server)); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.Warn("database: failed to populate default triggers", "error", err)
	} else {
		slog.Info("database: populated default triggers", "count", len(defaults))
	}
}

// GetTriggers returns all keyword-to-server mappings.
func (s *Store) GetTriggers() (map[string]string, error) {
	triggers := make(map[string]string)
	err := s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("trigger:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := string(item.Key()[8:])
			err := item.Value(func(val []byte) error {
				triggers[key] = string(val)
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return triggers, err
}

// intentRegexCache caches compiled trigger regexps to avoid O(N) compilations per search.
var intentRegexCache sync.Map // map[string]*regexp.Regexp

// AnalyzeIntent performs a fast regex-based scan of a message against the trigger map.
func (s *Store) AnalyzeIntent(ctx context.Context, msg string) []string {
	triggers, err := s.GetTriggers()
	if err != nil || len(triggers) == 0 {
		return nil
	}

	matches := make(map[string]struct{})
	msgLower := strings.ToLower(msg)

	for kw, server := range triggers {
		// Use cached regex for word-boundary matching to prevent false positives (e.g., 'edit' in 'edition')
		var re *regexp.Regexp
		if cached, ok := intentRegexCache.Load(kw); ok {
			re, _ = regexpFromCache(cached)
		}
		if re == nil {
			pattern := fmt.Sprintf(`(?i)\b%s\b`, regexp.QuoteMeta(kw))
			compiled, compErr := regexp.Compile(pattern)
			if compErr != nil {
				// Fallback to simple contains
				if strings.Contains(msgLower, strings.ToLower(kw)) {
					matches[server] = struct{}{}
				}
				continue
			}
			intentRegexCache.Store(kw, compiled)
			re = compiled
		}
		if re.MatchString(msgLower) {
			matches[server] = struct{}{}
		}
	}

	var result []string
	for k := range matches {
		result = append(result, k)
	}
	return result
}

// GetTopToolsForServer retrieves full mcp.Tool schemas for the top utilized tools of a server.
func (s *Store) GetTopToolsForServer(server string, limit int) ([]mcp.Tool, error) {
	var records []*ToolRecord
	err := s.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:" + server + ":")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			err := item.Value(func(val []byte) error {
				var r ToolRecord
				if err := json.Unmarshal(val, &r); err == nil {
					overlayUsageStat(txn, &r) // BDG-3: live usage lives under usage:<urn>
					records = append(records, &r)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	// Sort by usage count descending
	sortRecords(records)

	if len(records) > limit {
		records = records[:limit]
	}

	var tools []mcp.Tool
	for _, r := range records {
		schema, schemaErr := s.GetSchema(r.SchemaHash)
		if schemaErr != nil {
			slog.Debug("db: schema lookup failed for top tool", "urn", r.URN, "error", schemaErr)
		}
		tools = append(tools, mcp.Tool{
			Name:        r.Name,
			Description: r.Description,
			InputSchema: schema,
		})
	}
	return tools, nil
}

func sortRecords(records []*ToolRecord) {
	// Simple bubble sort for usage count descending since result sets are small (per-server tools)
	for i := range records {
		for j := i + 1; j < len(records); j++ {
			if records[i].UsageCount < records[j].UsageCount {
				records[i], records[j] = records[j], records[i]
			}
		}
	}
}

// WipeAll clears EVERYTHING from the persistent store (Badger) and search index (Bleve).
// This is used for hard-resets or when the database becomes corrupted.
func (s *Store) WipeAll() error {
	slog.Warn("database: initiating COMPLETE data wipe")

	// 1. Drop all data from Badger
	if err := s.DB.DropAll(); err != nil {
		return fmt.Errorf("failed to drop badger data: %w", err)
	}

	// 2. Re-initialize Search Index
	if s.Index != nil {
		if err := s.Index.Close(); err != nil {
			slog.Error("database: wipe failed to close search index", "error", err)
		}
		indexPath := filepath.Join(s.Path, "bleve_index")
		if err := os.RemoveAll(indexPath); err != nil {
			slog.Warn("database: wipe failed to remove search index directory", "path", indexPath, "error", err)
		}

		newIndex, err := NewSearchIndex(s.Path)
		if err != nil {
			return fmt.Errorf("failed to re-initialize search index: %w", err)
		}
		s.Index = newIndex
	}

	// 3. Clear in-memory cache to prevent stale GetTool hits
	s.Cache.Clear()

	return nil
}

// ComputeScoreBoard builds tool score cards from live GlobalToolTracker data,
// overlays intel baselines, and merges pre-computed trending deltas.
// Called on every health tick for real-time scores.
func (s *Store) ComputeScoreBoard(trending map[string]map[string]float64) map[string]any {
	scores := buildLiveToolScoreCards(trending)
	s.overlayIntelBaselines(scores)
	capScoreBoardEntries(scores, scoreBoardCap)
	return scores
}

// ComputeTrending scans BadgerDB telemetry:tool:* history to compute trending
// deltas for 30m, 4h, and all-time windows. Called on flush ticks only (every 1 min).
func (s *Store) ComputeTrending() map[string]map[string]float64 {
	return s.computeTrendingFromHistory()
}

// WipeDatabase permanently drops all tools, intelligence, and resets searches
func (s *Store) WipeDatabase() error {
	// 1. Drop Badger KV space
	if err := s.DB.DropAll(); err != nil {
		return fmt.Errorf("failed to drop KV store: %w", err)
	}

	// 2. Drop Bleve search
	if s.Index != nil {
		closeIndexOrWarn(s.Index)
		idxPath := filepath.Join(s.Path, "bleve_index")
		removeAllOrWarn(idxPath)
		newIndex, err := NewSearchIndex(s.Path)
		if err != nil {
			return fmt.Errorf("failed to re-initialize search index: %w", err)
		}
		s.Index = newIndex
	}

	// 3. Clear Caches
	s.Cache.Clear()

	slog.Info("database: COMPLETE data wipe successful")
	return nil
}

// ── Databases TUI Trending ─────────────────────────────────────────────────

// ComputeDatabaseTrending retrieves exact historic database snapshots for 5m, 15m, 1h windows
// directly from the memory-mapped badger flush buckets to allow the TUI to render velocity rates natively
func (s *Store) ComputeDatabaseTrending() map[string]any {
	trending := make(map[string]any)
	if s == nil || s.DB == nil {
		return trending
	}

	now := time.Now().Unix()
	windows := []struct {
		Name   string
		Target int64
	}{
		{"5m", now - (5 * 60)},
		{"15m", now - (15 * 60)},
		{"1h", now - (60 * 60)},
	}

	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = true
		opts.Reverse = true // Scan backwards from the target

		for _, w := range windows {
			it := txn.NewIterator(opts)
			seekKey := fmt.Sprintf("telemetry:bucket:%d", w.Target)
			it.Seek([]byte(seekKey))

			if it.ValidForPrefix([]byte("telemetry:bucket:")) {
				item := it.Item()
				itemValueOrWarn(item, func(val []byte) error {
					var snapshot map[string]any
					if json.Unmarshal(val, &snapshot) == nil {
						payload := make(map[string]any)
						if dbs, ok := snapshot["databases"]; ok {
							payload["databases"] = dbs
						}
						if llm, ok := snapshot["llm_backplane"]; ok {
							payload["llm_backplane"] = llm
						}
						trending[w.Name] = payload
					}
					return nil
				})
			}
			it.Close()
		}
		return nil
	})

	return trending
}
