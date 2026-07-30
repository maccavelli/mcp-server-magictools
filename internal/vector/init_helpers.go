package vector

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/coder/hnsw"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func vectorInitDisabledEngine() {
	slog.Info("vector engine boot: disabled via config (vector_enabled=false). falling back to BM25.", "component", "vector")
	GlobalEngine = &Engine{initialized: true, embedder: nil}
}

func vectorResolveEmbedder(cfg *config.Config) (Embedder, error) {
	slog.Info("vector engine boot: STAGE 1 — resolving embedding configuration",
		"component", "vector",
		"embedding_provider", cfg.Intelligence.EmbeddingProvider,
		"embedding_model", cfg.Intelligence.EmbeddingModel,
		"embedded_dimensionality", cfg.Intelligence.EmbeddingDimensionality)
	embedder := NewEmbedderFromConfig(cfg)
	if embedder == nil && cfg.Intelligence.VectorEnabled {
		return nil, fmt.Errorf("vector engine boot: failed to construct embedder for provider %q", cfg.Intelligence.EmbeddingProvider)
	}
	if embedder == nil {
		slog.Warn("vector engine boot: DISABLED — no valid embedder could be constructed. falling back to BM25.", "component", "vector")
	} else {
		slog.Info("vector engine boot: STAGE 2 — embedder constructed successfully", "component", "vector", "provider", embedder.Provider())
	}
	return embedder, nil
}

func vectorCheckSentinel(cfg *config.Config, metaPath, dbPath string) (needsRebuild bool, tombstones []string, err error) {
	currentHash := computeSentinelHash(
		cfg.Intelligence.EmbeddingProvider, cfg.Intelligence.EmbeddingModel,
		cfg.Intelligence.EmbeddingDimensionality, cfg.Intelligence.EmbeddingAPIURL,
	)
	slog.Info("vector engine boot: STAGE 3 — checking dimension sentinel",
		"component", "vector", "sentinel_hash", currentHash[:12]+"...", "meta_path", metaPath)
	var stored dimensionSentinel
	metaData, readErr := os.ReadFile(filepath.Clean(metaPath)) //nolint:gosec // G304: path from controlled db directory
	storedValid := readErr == nil && json.Unmarshal(metaData, &stored) == nil
	switch {
	case readErr != nil:
		slog.Warn("vector engine boot: SENTINEL MISSING — first boot or corrupted meta, wiping stale graph", "component", "vector", "error", readErr)
		needsRebuild = true
		removeOrWarn(dbPath)
	case storedValid && stored.Hash != currentHash:
		slog.Warn("vector engine boot: SENTINEL MISMATCH — wiping HNSW graph for full rebuild",
			"component", "vector",
			"old_provider", stored.Provider, "new_provider", cfg.Intelligence.EmbeddingProvider,
			"old_model", stored.Model, "new_model", cfg.Intelligence.EmbeddingModel,
			"old_dims", stored.Dims, "new_dims", cfg.Intelligence.EmbeddingDimensionality)
		needsRebuild = true
		removeOrWarn(dbPath)
	case storedValid && stored.Hash == currentHash:
		slog.Info("vector engine boot: SENTINEL MATCH — config unchanged, reusing existing graph",
			"component", "vector", "provider", stored.Provider, "model", stored.Model, "dims", stored.Dims)
		tombstones = stored.Tombstones
	}
	sentinel := dimensionSentinel{
		Provider: cfg.Intelligence.EmbeddingProvider, Model: cfg.Intelligence.EmbeddingModel,
		Dims: cfg.Intelligence.EmbeddingDimensionality, APIURL: cfg.Intelligence.EmbeddingAPIURL,
		Hash: currentHash, Tombstones: tombstones,
	}
	if data, mErr := json.MarshalIndent(sentinel, "", "  "); mErr == nil {
		mkdirAllOrWarn(filepath.Dir(metaPath), 0o750)                         //nolint:gosec // G301: user-local vector metadata directory
		if writeErr := os.WriteFile(metaPath, data, 0o600); writeErr != nil { //nolint:gosec // G306: user-local vector metadata
			return needsRebuild, tombstones, fmt.Errorf("vector engine boot: failed to write sentinel file: %w", writeErr)
		}
		slog.Debug("vector engine boot: sentinel file written", "component", "vector", "path", metaPath)
	}
	return needsRebuild, tombstones, nil
}

func vectorLoadGraph(dbPath string, needsRebuild bool) *hnsw.Graph[string] {
	slog.Info("vector engine boot: STAGE 4 — loading HNSW graph", "component", "vector", "needs_rebuild", needsRebuild, "blob_path", dbPath)
	if needsRebuild {
		slog.Info("vector engine boot: creating fresh HNSW graph (rebuild required)", "component", "vector")
		return createHNSWGraph()
	}
	f, ferr := os.Open(filepath.Clean(dbPath)) //nolint:gosec // G304: path from controlled db directory
	if ferr != nil {
		slog.Info("vector engine boot: no existing graph file found, creating empty graph", "component", "vector")
		return createHNSWGraph()
	}
	defer closeFileOrWarn(f, "vector-import")
	graph := createHNSWGraph()
	func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("vector engine boot: HNSW graph import panic intercepted, rebuilding index seamlessly.", "component", "vector", "panic", r)
				graph = createHNSWGraph()
				removeOrWarn(dbPath)
			}
		}()
		if ierr := graph.Import(bufio.NewReader(f)); ierr != nil {
			slog.Warn("vector engine boot: HNSW graph import failed, starting fresh", "component", "vector", "error", ierr)
			graph = createHNSWGraph()
			return
		}
		graph.Distance = hnsw.CosineDistance
		graph.M = hnswM
		graph.EfSearch = hnswEfSearch
		slog.Info("vector engine boot: HNSW graph imported successfully from disk", "component", "vector")
	}()
	return graph
}

func vectorApplyTombstones(engine *Engine, tombstones []string) {
	for _, t := range tombstones {
		engine.tombstones.Store(t, true)
	}
	if len(tombstones) > 0 {
		engine.corrupt.Store(true)
		engine.healIfCorrupt()
	}
}

func vectorLogReady(embedder Embedder, cfg *config.Config, needsRebuild bool, tombstoneCount int) {
	if embedder == nil {
		slog.Info("vector engine boot: STAGE 5 — READY ✓ semantic search OFFLINE (BM25 fallback active)", "component", "vector")
		return
	}
	if cfg.Intelligence.EmbeddingModel != "" && strings.Contains(strings.ToLower(cfg.Intelligence.EmbeddingModel), "preview") {
		slog.Warn("vector engine boot: WARNING — embedding model contains 'preview' tag, may be deprecated without notice",
			"component", "vector", "model", cfg.Intelligence.EmbeddingModel)
	}
	slog.Info("vector engine boot: STAGE 5 — READY ✓ semantic search ONLINE",
		"component", "vector", "provider", embedder.Provider(), "model", cfg.Intelligence.EmbeddingModel,
		"dims", cfg.Intelligence.EmbeddingDimensionality, "needs_hydration", needsRebuild || tombstoneCount > 0)
}

func vectorBootEngine(dbDir string, cfg *config.Config) error {
	if !cfg.Intelligence.VectorEnabled {
		vectorInitDisabledEngine()
		return nil
	}
	embedder, err := vectorResolveEmbedder(cfg)
	if err != nil {
		return err
	}
	dbPath := filepath.Join(dbDir, "magictools_vector.blob")
	metaPath := filepath.Join(dbDir, "magictools_vector.meta")
	needsRebuild := false
	var loadedTombstones []string
	if embedder != nil {
		needsRebuild, loadedTombstones, err = vectorCheckSentinel(cfg, metaPath, dbPath)
		if err != nil {
			return err
		}
	}
	GlobalEngine = &Engine{
		graph: vectorLoadGraph(dbPath, needsRebuild), embedder: embedder,
		dbPath: dbPath, metaPath: metaPath, initialized: true,
		expectedDims: cfg.Intelligence.EmbeddingDimensionality,
	}
	if embedder != nil {
		vectorApplyTombstones(GlobalEngine, loadedTombstones)
	}
	vectorLogReady(embedder, cfg, needsRebuild, len(loadedTombstones))
	return nil
}
