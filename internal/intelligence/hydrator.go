package intelligence

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/dgraph-io/badger/v4"

	"github.com/maccavelli/mcplib/llmprovider"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/llm"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

// LLMResponse is undocumented but satisfies standard structural requirements.
type LLMResponse struct {
	SyntheticIntents []string `json:"synthetic_intents"`
	LexicalTokens    []string `json:"lexical_tokens"`
	NegativeTriggers []string `json:"negative_triggers"`
}

// RunSweep executes the synchronous LLM hydration and vector embedding process.
// Bounded natively by maxPerSweep. Returns true if there are more tools left pending.
// It is intended to be invoked iteratively by the daemon continuation loop.
func RunSweep(ctx context.Context, store *db.Store, cfg *config.Config, providers map[string]llm.Provider) bool {
	if store == nil || cfg == nil {
		return false
	}

	llmAvailable := cfg.Intelligence.Provider != "" && cfg.Intelligence.APIKey != ""
	if !llmAvailable {
		return runVectorBackfillSweep(ctx, store, cfg)
	}

	if err := probeLLMAvailability(ctx, cfg); err != nil {
		slog.Warn("llm provider is NOT REACHABLE. hydration sweep aborted.",
			"component", hydratorComponent,
			"provider", cfg.Intelligence.Provider,
			"error", err)
		return false
	}

	if !shouldRunHydrationSweep(store) {
		return false
	}

	scan := scanHydrationTargets(store, cfg)
	runHNSWBackfillIfNeeded(ctx, store, cfg, scan.hnswBackfill)

	if len(scan.targets) == 0 {
		store.PendingHydrations.Store(0)
		return false
	}

	targets, hasMore := capHydrationBatch(store, scan.targets)
	slog.Info("hydration sweep starting", "component", hydratorComponent,
		"tools", len(targets), "estimated_seconds", len(targets)*2)

	sweepStart := time.Now()
	succeeded, failed := executeHydrationSweep(ctx, store, cfg, providers, targets)

	normalizeProxyScores(store)
	recoverHNSWCall(func() { hydrateVectorGraph(ctx, store, cfg, targets) }, "post-sweep HNSW hydration")

	slog.Info("sweep complete",
		"component", hydratorComponent,
		"total", len(targets),
		"succeeded", succeeded,
		"failed", failed,
		"duration", time.Since(sweepStart).String())

	return hasMore
}

// runVectorBackfillSweep embeds tools into HNSW when the generative LLM is unavailable
// but the embedding engine is configured and online.
func runVectorBackfillSweep(ctx context.Context, store *db.Store, cfg *config.Config) bool {
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return false
	}

	targets := collectVectorBackfillTargets(store, cfg, e)
	if len(targets) == 0 {
		return false
	}

	const maxPerSweep = 10
	total := len(targets)
	hasMore := total > maxPerSweep
	if hasMore {
		targets = targets[:maxPerSweep]
	}

	slog.Info("hydrator: vector-only backfill sweep", "component", hydratorComponent, "tools", len(targets), "deferred", total-len(targets))
	hydrateVectorGraph(ctx, store, cfg, targets)
	return hasMore
}

func collectVectorBackfillTargets(store *db.Store, cfg *config.Config, e *vector.Engine) []*db.ToolRecord {
	providerModel := embeddingProviderModel(cfg)
	var targets []*db.ToolRecord

	viewOrWarn(store.DB, func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			itemValueOrWarn(item, func(val []byte) error {
				var r db.ToolRecord
				if err := json.Unmarshal(val, &r); err != nil {
					return err
				}
				if intelItem, err := txn.Get([]byte("intel:" + r.URN)); err == nil {
					itemValueOrWarn(intelItem, func(v []byte) error {
						var intel db.ToolIntelligence
						if err := json.Unmarshal(v, &intel); err != nil {
							return err
						}
						r.OverlayIntelligence(&intel)
						return nil
					})
				}
				text := db.BuildEmbeddingText(&r)
				hash := db.ComputeEmbeddingHash(providerModel, text)
				if !e.HasDocument(r.URN) || r.EmbeddingHash != hash {
					rec := r
					targets = append(targets, &rec)
				}
				return nil
			})
		}
		return nil
	})
	return targets
}

func embeddingProviderModel(cfg *config.Config) string {
	if cfg == nil {
		return "bm25"
	}
	return cfg.Intelligence.EmbeddingProvider + ":" + cfg.Intelligence.EmbeddingModel
}

func updateToolStatus(store *db.Store, tool *db.ToolRecord, status string) {
	intel, err := store.GetIntelligence(tool.URN)
	if err != nil || intel == nil {
		intel = &db.ToolIntelligence{}
	}
	intel.AnalysisStatus = status
	intel.SchemaHash = tool.SchemaHash
	if err := store.SaveIntelligence(tool.URN, intel); err != nil {
		slog.Warn("failed to save updated tool state to badger", "component", hydratorComponent, "error", err)
	} else {
		slog.Info("status transition committed", "component", hydratorComponent, "tool", tool.URN, "new_status", status)
	}
}

// hydrateVectorGraph populates the HNSW vector index using canonical BuildEmbeddingText.
func hydrateVectorGraph(ctx context.Context, store *db.Store, cfg *config.Config, tools []*db.ToolRecord) {
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() || store == nil {
		return
	}

	providerModel := embeddingProviderModel(cfg)
	vecCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	count := 0
	for _, tool := range tools {
		if tool == nil {
			continue
		}

		rec, err := store.GetTool(tool.URN)
		if err != nil {
			slog.Warn("vector hydration skipped: tool missing from badger", "component", hydratorComponent, "urn", tool.URN)
			continue
		}
		if intel, intelErr := store.GetIntelligence(tool.URN); intelErr == nil && intel != nil {
			rec.OverlayIntelligence(intel)
		}

		embeddingText := db.BuildEmbeddingText(rec)
		contentHash := db.ComputeEmbeddingHash(providerModel, embeddingText)

		// INT-2 warm-start: when the cached embedding is still valid for the current
		// content (persisted hash matches) and a vector is cached under vec:, inject
		// it directly instead of re-embedding through the (paid) embedder API. This
		// covers the cold-rebuild case — Badger survived but the HNSW blob was wiped
		// (sentinel mismatch / first boot) — so a full ecosystem re-embed is avoided.
		cachedVec := rec.Vector
		if len(cachedVec) == 0 {
			cachedVec = store.GetToolVector(rec.URN)
		}
		warmStart := rec.EmbeddingHash == contentHash && len(cachedVec) > 0

		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Warn("HNSW graph.Add panicked for tool", "component", hydratorComponent, "urn", rec.URN, "panic", r)
				}
			}()
			if warmStart {
				if err := e.AddVector(vecCtx, rec.URN, cachedVec); err != nil {
					slog.Warn("warm-start vector injection failed; falling back to embed", "component", hydratorComponent, "urn", rec.URN, "error", err)
				} else {
					telemetry.SearchMetrics.CacheHits.Add(1)
					count++
					return
				}
			}
			if err := e.UpsertDocument(vecCtx, rec.URN, embeddingText, contentHash); err != nil {
				slog.Warn("post-sweep HNSW hydration failed", "component", hydratorComponent, "urn", rec.URN, "error", err)
			} else {
				rec.EmbeddingHash = contentHash
				if vec, ok := e.GetVector(rec.URN); ok {
					rec.Vector = vec
				}
				if saveErr := store.SaveTool(rec); saveErr != nil {
					slog.Warn("failed to persist embedding hash", "component", hydratorComponent, "urn", rec.URN, "error", saveErr)
				}
				count++
			}
		}()
	}
	if count > 0 {
		slog.Info("post-sweep HNSW vectors committed", "component", hydratorComponent, "tools", count)
		if err := e.Save(); err != nil {
			slog.Warn("post-sweep HNSW save failed", "component", hydratorComponent, "error", err)
		}
	}
	db.RecordSearchGraphCompleteness(store)
}

// normalizeProxyScores applies z-score normalization across all non-native tool
// ProxyReliability values after a hydration sweep. This ensures relative
// differentiation is preserved even when raw scores cluster together.
//
// Native tools (IsNative) are excluded from normalization to preserve their
// static 1.5 override. Requires ≥3 non-native tools with non-zero stdev.
//
// Formula: normalized = 0.8 + ((raw - mean) / stdev) × 0.25
// Clamped to [0.5, 1.3].
func normalizeProxyScores(store *db.Store) {
	type entry struct {
		urn         string
		reliability float64
	}
	var entries []entry

	viewOrWarn(store.DB, func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			itemValueOrWarn(item, func(val []byte) error {
				var t db.ToolRecord
				if err := json.Unmarshal(val, &t); err != nil {
					return err
				}
				if t.IsNative {
					return nil
				}
				if intelItem, extractErr := txn.Get([]byte("intel:" + t.URN)); extractErr == nil {
					itemValueOrWarn(intelItem, func(iv []byte) error {
						var intel db.ToolIntelligence
						if err := json.Unmarshal(iv, &intel); err != nil {
							return err
						}
						if intel.Metrics.ProxyReliability > 0 {
							entries = append(entries, entry{urn: t.URN, reliability: intel.Metrics.ProxyReliability})
						}
						return nil
					})
				}
				return nil
			})
		}
		return nil
	})

	if len(entries) < 3 {
		slog.Debug("normalization skipped: insufficient non-native tools", "component", hydratorComponent, "count", len(entries))
		return
	}

	// Compute mean
	var sum float64
	for _, e := range entries {
		sum += e.reliability
	}
	mean := sum / float64(len(entries))

	// Compute standard deviation
	var variance float64
	for _, e := range entries {
		d := e.reliability - mean
		variance += d * d
	}
	stdev := math.Sqrt(variance / float64(len(entries)))

	if stdev < 0.001 {
		slog.Warn("normalization skipped: near-zero standard deviation",
			"component", hydratorComponent,
			"mean", mean, "stdev", stdev, "count", len(entries))
		return
	}

	slog.Info("normalizing proxyreliability scores",
		"component", hydratorComponent,
		"tools", len(entries), "mean", fmt.Sprintf("%.4f", mean), "stdev", fmt.Sprintf("%.4f", stdev))

	var normalized int
	for _, e := range entries {
		z := (e.reliability - mean) / stdev
		newScore := 0.8 + (z * 0.25)

		// Clamp to [0.5, 1.3]
		if newScore < 0.5 {
			newScore = 0.5
		}
		if newScore > 1.3 {
			newScore = 1.3
		}

		intel, err := store.GetIntelligence(e.urn)
		if err != nil || intel == nil {
			continue
		}
		oldScore := intel.Metrics.ProxyReliability
		intel.Metrics.ProxyReliability = newScore
		if err := store.SaveIntelligence(e.urn, intel); err != nil {
			slog.Warn("failed to save normalized score", "component", hydratorComponent, "urn", e.urn, "error", err)
		} else {
			slog.Debug("score normalized", "component", hydratorComponent, "urn", e.urn,
				"old", fmt.Sprintf("%.4f", oldScore), "new", fmt.Sprintf("%.4f", newScore))
			normalized++
		}
	}

	slog.Info("normalization complete", "component", hydratorComponent, "normalized", normalized)
}

// probeLLMAvailability performs a lightweight connectivity check against the
// configured LLM provider. Returns nil if reachable, error otherwise.
func probeLLMAvailability(ctx context.Context, cfg *config.Config) error {
	probeCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	p, err := llmprovider.NewProvider(cfg.Intelligence.Provider, cfg.Intelligence.APIKey, cfg.Intelligence.Model)
	if err != nil {
		return fmt.Errorf("provider initialization failed: %w", err)
	}

	// Lightweight probe: request a single-token response
	_, err = p.Generate(probeCtx, "Respond with exactly: OK")
	if err != nil {
		return fmt.Errorf("LLM probe failed: %w", err)
	}
	return nil
}

// initProviders pre-initializes LLM providers for the model cascade.
// Providers are created once per sweep and reused across all tools.
// When pool is non-nil, uses the pool's rate-limited provider for isolation.
func initProviders(_ context.Context, cfg *config.Config, pool *llm.Pool) map[string]llm.Provider {
	if pool != nil {
		return map[string]llm.Provider{
			cfg.Intelligence.Model: pool.NewRateLimitedProvider(),
		}
	}

	models := append([]string{cfg.Intelligence.Model}, cfg.Intelligence.FallbackModels...)
	providers := make(map[string]llm.Provider, len(models))

	for _, model := range models {
		p, err := llmprovider.NewProvider(cfg.Intelligence.Provider, cfg.Intelligence.APIKey, model)
		if err != nil {
			slog.Warn("model init failed", "component", hydratorComponent, "model", model, "error", err)
			continue
		}
		providers[model] = p
	}
	return providers
}

// LLMToolPayload is a trimmed struct sent to the LLM to reduce API token waste.
// Excludes internal fields like usage_count, last_synced_at, schema_hash.
type LLMToolPayload struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Server      string `json:"server"`
}

func applySemanticAugmentation(ctx context.Context, tool *db.ToolRecord, cfg *config.Config, providers map[string]llm.Provider) (*LLMResponse, error) {
	prompt := `You are the Semantic Intelligence Engine for the MagicTools Orchestrator.
Task: Analyze the attached raw JSON tool schema and "hydrate" it into a searchable, weighted intent matrix.
Return ONLY valid JSON matching this schema:
{
  "synthetic_intents": ["phrase a user would say", ... 12 max],
  "lexical_tokens": ["technical_keyword", ... 8 max],
  "negative_triggers": ["phrase that sounds identical but is unrelated", ... 5 max]
}

Rules:
- synthetic_intents: natural language phrases a user would type to trigger this tool. Be specific and diverse.
- synthetic_intents MUST include the server name (from the "server" field) in at least 3 of the 12 phrases.
- lexical_tokens: precise technical keywords unique to this tool's domain. Avoid generic terms.
- lexical_tokens MUST NOT use generic terms shared by 5+ tools (e.g. "search", "analyze", "debug", "logs"). Use server-specific or domain-specific terms instead.
- negative_triggers: phrases that SOUND related but should NOT trigger this tool. Focus on cross-category confusion.
- negative_triggers MUST include the names of competing servers that have similar tools. For example, if this is a "git" server tool, include "github" and "gitlab" as negative triggers.
- For tools named "get_internal_logs": every synthetic intent MUST reference the specific server's problem domain, not just generic "get logs" phrases.

Do not use markdown blocks, just return the raw JSON object string.`

	// 🛡️ TRIMMED PAYLOAD: Only send semantically relevant fields to the LLM
	payload := LLMToolPayload{
		Name:        tool.Name,
		Description: tool.Description,
		Category:    tool.Category,
		Server:      tool.Server,
	}
	schemaBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal tool payload: %w", err)
	}
	fullPrompt := prompt + "\n\nSCHEMA:\n" + string(schemaBytes)

	// 🔄 MODEL CASCADE: Try primary model first, then fallbacks
	modelsToTry := append([]string{cfg.Intelligence.Model}, cfg.Intelligence.FallbackModels...)

	retryCount := cfg.Intelligence.RetryCount
	if retryCount <= 0 {
		retryCount = 2
	}
	retryDelay := time.Duration(cfg.Intelligence.RetryDelay) * time.Second
	if retryDelay <= 0 {
		retryDelay = 5 * time.Second
	}

	var lastErr error
	for _, model := range modelsToTry {
		p, ok := providers[model]
		if !ok {
			lastErr = fmt.Errorf("provider for model %s not initialized", model)
			continue
		}

		rawText, err := llm.GenerateWithRetry(ctx, p, fullPrompt, retryCount, retryDelay)
		if err != nil {
			slog.Warn("model generation failed, trying next", "component", hydratorComponent, "model", model, "error", err)
			lastErr = err
			continue
		}

		// Robust strip if the gateway returned markdown bounding box
		rawText = strings.TrimSpace(rawText)
		lower := strings.ToLower(rawText)
		if strings.HasPrefix(lower, "```json") {
			rawText = rawText[7:]
		} else if strings.HasPrefix(lower, "```") {
			rawText = rawText[3:]
		}
		rawText = strings.TrimSuffix(strings.TrimSpace(rawText), "```")
		rawText = strings.TrimSpace(rawText)

		var result LLMResponse
		if err := json.Unmarshal([]byte(rawText), &result); err != nil {
			slog.Warn("json parse failed, trying next model", "component", hydratorComponent, "model", model, "error", err, "raw", rawText)
			lastErr = err
			continue
		}

		slog.Debug("augmentation succeeded", "component", hydratorComponent, "model", model)
		return &result, nil
	}

	return nil, fmt.Errorf("all models failed for %s, last error: %w", cfg.Intelligence.Provider, lastErr)
}

// RecallMiner abstracts the recall client APIs needed by the intelligence
// package. This prevents a direct dependency on the external package.
type RecallMiner interface {
	RecallEnabled() bool
	AggregateSessionFromRecall(ctx context.Context, serverID, projectID string) (map[string]any, error)
	ListSessionsByFilter(ctx context.Context, projectID, serverID, outcome string, limit int) string
}

// MineRecallPatterns queries recall for historical session data and feeds
// empirically validated tool-to-intent mappings into the Ghost Index.
// This creates a feedback loop: successful pipeline executions stored in
// recall feed back into compose_pipeline's scoring, making future DAG
// compositions grounded in real usage patterns.
func MineRecallPatterns(ctx context.Context, rc RecallMiner, store *db.Store) {
	if rc == nil || !rc.RecallEnabled() || store == nil {
		return
	}

	var totalIndexed int
	for _, serverID := range recallMinerServers {
		totalIndexed += mineServerRecallPatterns(ctx, rc, store, serverID)
	}

	if totalIndexed > 0 {
		slog.Info("recall_miner: empirical patterns indexed into Ghost Index", "count", totalIndexed)
	}
}

// CalibrateFromRecall mines recall session outcomes to empirically adjust
// ProxyReliability scores. Tools with high real-world success rates get
// boosted, while tools that consistently error get penalized.
// Final score = (z_normalized * 0.6) + (empirical_rate * 0.4)
func CalibrateFromRecall(ctx context.Context, rc RecallMiner, store *db.Store) {
	if rc == nil || !rc.RecallEnabled() || store == nil {
		return
	}

	stats := collectRecallCalibrationStats(ctx, rc)
	if len(stats) == 0 {
		return
	}

	calibrated := applyRecallCalibration(store, stats)
	if calibrated > 0 {
		slog.Info("recall_calibration: ProxyReliability adjusted from empirical data",
			"tools_calibrated", calibrated, "total_tracked", len(stats))
	}
}
