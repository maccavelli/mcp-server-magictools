package intelligence

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/dgraph-io/badger/v4"
	"golang.org/x/sync/errgroup"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/llm"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

type hydrationScanResult struct {
	targets      []*db.ToolRecord
	hnswBackfill []*db.ToolRecord
}

func shouldRunHydrationSweep(store *db.Store) bool {
	e := vector.GetEngine()
	vectorMissing := e != nil && e.VectorEnabled() && e.RequiresHydration()

	hnswIncomplete := false
	if !vectorMissing && e != nil && e.VectorEnabled() {
		if expectedCount, err := store.Index.ToolCount(); err == nil && safeUint64FromInt(e.Len()) < expectedCount {
			hnswIncomplete = true
		}
	}

	if store.PendingHydrations.Load() == 0 && !vectorMissing && !hnswIncomplete {
		return false
	}
	return true
}

func scanHydrationTargets(store *db.Store, cfg *config.Config) hydrationScanResult {
	var result hydrationScanResult
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
				intelPtr, iErr := loadToolIntel(txn, r.URN)
				if iErr != nil || intelPtr == nil || intelPtr.AnalysisStatus == analysisStatusPending || intelPtr.AnalysisStatus == "" || r.SchemaHash != intelPtr.SchemaHash {
					logHydrationTrigger(r, intelPtr, iErr)
					result.targets = append(result.targets, &r)
					return nil
				}
				collectHNSWBackfillCandidate(cfg, &r, intelPtr, &result.hnswBackfill)
				return nil
			})
		}
		return nil
	})
	return result
}

func loadToolIntel(txn *badger.Txn, urn string) (*db.ToolIntelligence, error) {
	intelItem, err := txn.Get([]byte("intel:" + urn))
	if err != nil {
		return nil, err
	}
	var intel db.ToolIntelligence
	var loaded bool
	itemValueOrWarn(intelItem, func(v []byte) error {
		if err := json.Unmarshal(v, &intel); err != nil {
			return err
		}
		loaded = true
		return nil
	})
	if !loaded {
		return nil, badger.ErrKeyNotFound
	}
	return &intel, nil
}

func logHydrationTrigger(r db.ToolRecord, intelPtr *db.ToolIntelligence, iErr error) {
	if intelPtr != nil {
		slog.Debug("hydrator trigger debug", "urn", r.URN, "status", intelPtr.AnalysisStatus, "r_hash", r.SchemaHash, "intel_hash", intelPtr.SchemaHash, "iErr", iErr)
	} else {
		slog.Debug("hydrator trigger debug", "urn", r.URN, "nil_intel", true, "iErr", iErr)
	}
}

func collectHNSWBackfillCandidate(cfg *config.Config, r *db.ToolRecord, intelPtr *db.ToolIntelligence, backfill *[]*db.ToolRecord) {
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return
	}
	overlay := *r
	if intelPtr != nil {
		overlay.OverlayIntelligence(intelPtr)
	}
	text := db.BuildEmbeddingText(&overlay)
	hash := db.ComputeEmbeddingHash(embeddingProviderModel(cfg), text)
	if !e.HasDocument(r.URN) || r.EmbeddingHash != hash {
		rec := overlay
		*backfill = append(*backfill, &rec)
	}
}

func runHNSWBackfillIfNeeded(ctx context.Context, store *db.Store, cfg *config.Config, hnswBackfill []*db.ToolRecord) {
	if len(hnswBackfill) == 0 {
		return
	}
	slog.Info("hydrator: HNSW graph missing known tools, triggering native graph backfill", "count", len(hnswBackfill))
	recoverHNSWCall(func() { hydrateVectorGraph(ctx, store, cfg, hnswBackfill) }, "post-sweep HNSW backfill")
}

func capHydrationBatch(store *db.Store, targets []*db.ToolRecord) ([]*db.ToolRecord, bool) {
	const maxPerSweep = 5
	hasMore := len(targets) > maxPerSweep
	if hasMore {
		remaining := int64(len(targets) - maxPerSweep)
		slog.Info("sweep batch capped to prevent boot stall", "component", hydratorComponent,
			"total_pending", len(targets), "processing", maxPerSweep, "deferred", remaining)
		store.PendingHydrations.Store(remaining)
		return targets[:maxPerSweep], true
	}
	store.PendingHydrations.Store(0)
	return targets, false
}

func executeHydrationSweep(ctx context.Context, store *db.Store, cfg *config.Config, providers map[string]llm.Provider, targets []*db.ToolRecord) (succeeded, failed int64) {
	var succCount, failCount atomic.Int64
	const maxConcurrency = 3
	sem := make(chan struct{}, maxConcurrency)
	var eg errgroup.Group

	for i, tool := range targets {
		if i > 0 {
			paceHydrationLaunch(ctx)
		}
		if ctx.Err() != nil {
			break
		}
		tool := tool
		eg.Go(func() error {
			if !acquireHydrationSlot(ctx, sem) {
				return nil
			}
			defer func() { <-sem }()
			processHydrationTarget(ctx, store, cfg, providers, tool, &succCount, &failCount)
			return nil
		})
	}
	waitGroupOrWarn(&eg)
	return succCount.Load(), failCount.Load()
}

func paceHydrationLaunch(ctx context.Context) {
	timer := time.NewTimer(1 * time.Second)
	select {
	case <-ctx.Done():
		timer.Stop()
	case <-timer.C:
	}
}

func acquireHydrationSlot(ctx context.Context, sem chan struct{}) bool {
	select {
	case sem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

func processHydrationTarget(ctx context.Context, store *db.Store, cfg *config.Config, providers map[string]llm.Provider, tool *db.ToolRecord, succeeded, failed *atomic.Int64) {
	util.TraceFunc(ctx, "event", "hydrator_start", "tool", tool.URN)
	if tool.IsNative {
		if hydrateNativeTool(store, tool) {
			succeeded.Add(1)
		} else {
			failed.Add(1)
		}
		return
	}
	if hydrateSemanticTool(ctx, store, cfg, providers, tool) {
		succeeded.Add(1)
	} else {
		failed.Add(1)
	}
}

func hydrateNativeTool(store *db.Store, tool *db.ToolRecord) bool {
	slog.Info("applying static maximum trust score to native proxy tool", "component", hydratorComponent, "tool", tool.URN)
	intents, tokens := nativeToolSemantics(tool.Name)
	intel := &db.ToolIntelligence{
		SyntheticIntents: intents,
		LexicalTokens:    tokens,
		AnalysisStatus:   analysisStatusHydrated,
		SchemaHash:       tool.SchemaHash,
	}
	intel.Metrics.ProxyReliability = 2.0
	if err := store.SaveIntelligence(tool.URN, intel); err != nil {
		slog.Warn("failed to save static proxy state", "component", hydratorComponent, "error", err)
		updateToolStatus(store, tool, analysisStatusFailed)
		return false
	}
	slog.Debug("static proxy state committed", "component", hydratorComponent, "tool", tool.URN)
	return true
}

const toolAlignTools = "align_tools"

func nativeToolSemantics(name string) (intents, tokens []string) {
	switch name {
	case toolAlignTools:
		return []string{"search tools", "find tool", "discover", "lookup URN"}, []string{"discovery", "search", "directory", "URN"}
	case "call_proxy":
		return []string{"execute tool", "run", "invoke", "proxy call"}, []string{"execute", "dispatch", "proxy", "run"}
	case toolStageExecutePipeline:
		return []string{"DAG", "pipeline", "execution plan", "analysis graph"}, []string{"DAG", "pipeline", serverBrainstorm, serverGoModernizer, "sequence"}
	case "sync_ecosystem":
		return []string{"synchronize", "refresh all", "update local cache", "resync"}, []string{"sync", "ecosystem", "refresh", "index"}
	case "list_tools":
		return []string{"inventory", "available tools", "show tools", "enumerate"}, []string{"list", "inventory", "sub-servers", "tools"}
	case "get_health_report":
		return []string{"health", "status", "alive", "ping", "availability"}, []string{"health", "ping", "status", "state"}
	default:
		return []string{"manage orchestrator", "system tools", "execute", name}, []string{"orchestrator", "admin", "magictools", name}
	}
}

func hydrateSemanticTool(ctx context.Context, store *db.Store, cfg *config.Config, providers map[string]llm.Provider, tool *db.ToolRecord) bool {
	slog.Info("fetching semantic augmentation", "component", hydratorComponent, "tool", tool.URN)
	startTime := time.Now()
	toolCtx, toolCancel := context.WithTimeout(ctx, time.Duration(cfg.Intelligence.TimeoutSeconds)*time.Second)
	result, err := applySemanticAugmentation(toolCtx, tool, cfg, providers)
	toolCancel()
	elapsed := time.Since(startTime)
	if err != nil {
		slog.Warn("remote augmentation failed", "component", hydratorComponent, "tool", tool.URN, "duration", elapsed.String(), "error", err)
		updateToolStatus(store, tool, analysisStatusFailed)
		return false
	}
	slog.Debug("parsing completed successfully", "component", hydratorComponent, "tool", tool.URN, "duration", elapsed.String())
	return saveSemanticHydration(store, tool, result)
}

func saveSemanticHydration(store *db.Store, tool *db.ToolRecord, result *LLMResponse) bool {
	existingIntel, err := store.GetIntelligence(tool.URN)
	if err != nil {
		existingIntel = nil
	}
	preservedReliability := 1.0
	if existingIntel != nil && existingIntel.Metrics.ProxyReliability > 0 {
		preservedReliability = existingIntel.Metrics.ProxyReliability
	}
	intel := &db.ToolIntelligence{
		SyntheticIntents: result.SyntheticIntents,
		LexicalTokens:    result.LexicalTokens,
		NegativeTriggers: result.NegativeTriggers,
		AnalysisStatus:   analysisStatusHydrated,
		SchemaHash:       tool.SchemaHash,
	}
	intel.Metrics.ProxyReliability = preservedReliability
	if err := store.SaveIntelligence(tool.URN, intel); err != nil {
		slog.Warn("failed to save updated tool state to badger", "component", hydratorComponent, "error", err)
		return false
	}
	slog.Debug("status transition committed", "component", hydratorComponent, "tool", tool.URN, "new_status", analysisStatusHydrated)
	return true
}

func recoverHNSWCall(fn func(), label string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Warn(label+" panicked (recovered)", "component", hydratorComponent, "panic", r)
		}
	}()
	fn()
}
