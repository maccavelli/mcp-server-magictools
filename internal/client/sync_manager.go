// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"fmt"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/logging"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

// SyncResult is undocumented but satisfies standard structural requirements.
type SyncResult struct {
	TotalPotential int64
	Connected      []string
	Failed         []string
}

// SyncNativeTools routes internal orchestrator tools (magictools) through the standard
// DB parse-and-save pipeline to acquire semantic hydrator metadata.
func (m *WarmRegistry) SyncNativeTools(ctx context.Context, tools *mcp.ListToolsResult) (int, error) {
	// Treat magictools as a synthetic sub-server for DB routing purposes
	sc := config.ServerConfig{
		Name: serverMagictools,
		// Provide a non-nil array to avoid panics when accessing array properties later
		DisabledTools: []string{},
	}

	// Create a synthetic SubServer wrapper to pass nil checks in parseAndSaveTools
	srv := &SubServer{
		Status: StatusReady,
	}

	indexed, err := m.parseAndSaveTools(ctx, sc, srv, tools)
	if err != nil {
		return 0, fmt.Errorf("failed to sync native tools: %w", err)
	}
	return indexed, nil
}

// SyncEcosystem is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) SyncEcosystem(ctx context.Context) (*SyncResult, error) {
	if m.Config.ConfigPath != "" {
		freshCfg, err := m.Config.Reload()
		if err == nil {
			m.Config.UpdateManagedServers(freshCfg.ManagedServers)
		}
	}

	managed := m.Config.GetManagedServers()
	result := &SyncResult{}
	var mu sync.Mutex

	// 🛡️ BACKGROUND ORPHAN SWEEP: Remove ghost servers/tools from BadgerDB that are no longer managed
	var activeNames []string
	for _, sc := range managed {
		activeNames = append(activeNames, sc.Name)
	}
	// 🛡️ EXEMPT NATIVE ORCHESTRATOR: 'magictools' itself is not listed in GetManagedServers()
	activeNames = append(activeNames, serverMagictools)

	go func(c context.Context, names []string) {
		// INT-8: honor the lifecycle ctx so a sweep doesn't start after shutdown.
		if c.Err() != nil {
			return
		}
		if err := m.Store.PurgeOrphanedServers(names); err != nil {
			slog.Warn("sync: background sweep of orphaned servers failed", "error", err)
		}
	}(ctx, activeNames)

	// Seed the trigger DB with default keyword→server mappings for data-driven steering.
	m.Store.PopulateDefaultTriggers()

	// All servers sync concurrently — no ordering needed.

	maxConcurrency := min(runtime.NumCPU()*2, 10)
	sem := make(chan struct{}, maxConcurrency)
	var wg sync.WaitGroup

	for _, sc := range managed {
		wg.Add(1)
		go func(c context.Context, serverConfig config.ServerConfig) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-c.Done():
				return
			}

			timeoutCtx, cancel := context.WithTimeout(c, 60*time.Second)
			defer cancel()

			err := m.indexServer(timeoutCtx, serverConfig)

			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				m.logSyncError(serverConfig.Name, err)
				result.Failed = append(result.Failed, serverConfig.Name)
				return
			}

			result.Connected = append(result.Connected, serverConfig.Name)
		}(ctx, sc)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.IsSynced.Store(true)
		result.TotalPotential = int64(len(result.Connected))
		return result, nil
	case <-ctx.Done():
		return result, ctx.Err()
	}
}

// SyncServer is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) SyncServer(ctx context.Context, name string) error {
	var sc *config.ServerConfig
	for _, c := range m.Config.GetManagedServers() {
		if c.Name == name {
			sc = &c
			break
		}
	}

	if sc == nil {
		return fmt.Errorf("server %s not found in managed config", name)
	}

	// Timeout ownership belongs to the caller (executeBootSequence: 60s).
	// No nested timeout here — stacking identical 60s contexts adds no
	// protection and misleads developers during timeout debugging.
	return m.indexServer(ctx, *sc)
}

func (m *WarmRegistry) indexServer(ctx context.Context, sc config.ServerConfig) error {
	// Always sync to warm up the server on startup or manual sync.
	// We no longer skip if tools are in the store to ensure handshakes complete.

	srv, ok := m.GetServer(sc.Name)
	if !ok || srv.Session == nil {
		slog.Log(ctx, util.LevelTrace, "sync: initiating server connection", "server", sc.Name)

		timeoutDur := 10 * time.Second
		if sc.Name == serverRecall {
			timeoutDur = 15 * time.Second // 🛡️ Recall on localhost should connect within 5s; 15s is generous
		}

		var err error
		// 🛡️ RACE FIX: Exponential backoff for initial connection handshake
		// Wait for sub-servers to fully initialize before dialing.
		for attempt := range 3 {
			connectCtx, cancelConnect := context.WithTimeout(ctx, timeoutDur)
			err = m.Connect(connectCtx, sc.Name, sc.Command, sc.Args, sc.Env, sc.Hash())
			cancelConnect()
			if err == nil {
				break
			}

			if !strings.Contains(err.Error(), "connection refused") && !strings.Contains(err.Error(), "EOF") {
				break // Only retry on transient socket errors
			}

			slog.Debug("sync: sub-server not ready, backing off", "server", sc.Name, "attempt", attempt+1, "error", err)

			backoff := time.Duration(1<<attempt) * 500 * time.Millisecond
			timer := time.NewTimer(backoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				err = ctx.Err()
			case <-timer.C:
			}
		}

		if err != nil {
			return fmt.Errorf("server %s connection failed: %w", sc.Name, err)
		}
		srv, _ = m.GetServer(sc.Name)
		slog.Log(ctx, util.LevelTrace, "sync: server connected", "server", sc.Name)
	} else {
		// Just ensure it's healthy if already connected
		s, sOk := m.GetServer(sc.Name)
		if sOk && s != nil {
			m.mu.Lock()
			s.DesiredState = StatusHealthy
			mailbox := s.Mailbox
			m.mu.Unlock()
			select {
			case mailbox <- cmdConnect:
			default:
			}
		}
	}

	// 🛡️ NIL GUARD + RACE FIX: Snapshot session under RLock to protect against
	// race where process crashes between Connect and ListTools.
	m.mu.RLock()
	if srv == nil || srv.Session == nil {
		m.mu.RUnlock()
		return fmt.Errorf("server %s: session lost before tool sync (process may have crashed)", sc.Name)
	}
	session := srv.Session // snapshot under lock
	m.mu.RUnlock()

	m.mu.Lock()
	srv.Status = StatusReady
	m.mu.Unlock()

	slog.Log(ctx, util.LevelTrace, "sync: server marked ready, executing ListTools synchronously", "server", sc.Name)

	tools, err := session.ListTools(ctx, nil)
	if err != nil || tools == nil || len(tools.Tools) == 0 {
		errMsg := "empty result"
		if err != nil {
			errMsg = err.Error()
		}

		var raw []byte
		if srv.Filter != nil {
			raw = srv.Filter.GetLastFrame()
		}
		slog.Error("Sync Failed / tools/list error",
			"component", "lifecycle",
			"server_id", sc.Name,
			"error", errMsg,
			"raw_payload", string(raw))

		m.Logger.Log(logging.WARNING, sc.Name, fmt.Sprintf("Sync Failed: %v", errMsg))
		return fmt.Errorf("tools/list failed for %s: %s", sc.Name, errMsg)
	}

	if _, parseErr := m.parseAndSaveTools(ctx, sc, srv, tools); parseErr != nil {
		slog.Error("Sync Failed / parse error", "server_id", sc.Name, "error", parseErr)
		return parseErr
	}

	// 🛡️ PROMPT PULL: Eagerly fetch prompts during ecosystem sync
	if m.OnPromptListChanged != nil {
		go m.OnPromptListChanged(sc.Name)
	}

	return nil
}

func (m *WarmRegistry) parseAndSaveTools(ctx context.Context, sc config.ServerConfig, srv *SubServer, tools *mcp.ListToolsResult) (int, error) {
	rawList := loadRawToolList(srv, sc.Name)
	m.normalizeToolsBeforeSave(sc, srv, tools, rawList)
	m.Logger.Log(logging.SYNC, sc.Name, fmt.Sprintf("%d Tools Indexed", len(tools.Tools)))

	disabled := disabledToolSet(sc)
	m.scheduleIntelligenceGC(ctx, sc, tools, disabled)

	batchRecords, batchSchemas, indexed := m.buildSyncBatchRecords(sc, tools, disabled)
	compositeHash := compositeSyncHash(batchRecords)
	if m.shouldSkipUnchangedSync(sc.Name, compositeHash, batchRecords) {
		return indexed, nil
	}
	if len(batchRecords) == 0 {
		return indexed, nil
	}
	if err := m.persistSyncedTools(ctx, sc, batchRecords, batchSchemas, compositeHash); err != nil {
		return 0, err
	}
	return indexed, nil
}

// Vector hydration (HNSW embedding of the tool ecosystem, including the warm-start
// cache-hit path) is owned exclusively by the hydrator daemon's post-sweep
// (intelligence.hydrateVectorGraph), which reads cached vectors from the "vec:"
// key and only calls the embedder API on a true miss. The previous inline
// WarmRegistry.HydrateToolGraph was unreachable and has been removed.
