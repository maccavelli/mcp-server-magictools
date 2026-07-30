package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func (h *OrchestratorHandler) registerSyncTools(s *mcp.Server) {
	// Descriptions sourced from inventory.go via addTool().
	h.addTool(s, &mcp.Tool{Name: toolSyncServers}, h.SyncServers)
}

// SyncServers handles targeted or global server synchronization.
func (h *OrchestratorHandler) SyncServers(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	defer h.alignGen.Add(1) // ALIGN-2: invalidate align cache — tool schemas may have changed
	var args struct {
		Names string `json:"names"`
	}
	if req != nil && req.Params != nil && len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("failed to parse sync_servers arguments: %w", err)
		}
	}

	names := strings.Fields(args.Names)

	if len(names) == 0 {
		// Global sync
		result, err := h.Registry.SyncEcosystem(ctx)
		if err != nil {
			return nil, err
		}

		// 🛡️ NATIVE TOOL INJECTION: Treat magictools as a synthetic sub-server
		var listRes mcp.ListToolsResult
		if parseErr := json.Unmarshal(InternalToolsInventoryJSON, &listRes.Tools); parseErr == nil {
			if _, syncErr := h.Registry.SyncNativeTools(ctx, &listRes); syncErr != nil {
				slog.Error("SyncServers: failed to sync native tools", keyError, syncErr)
				result.Failed = append(result.Failed, serverMagictools)
			} else {
				result.Connected = append(result.Connected, serverMagictools)
			}
		} else {
			slog.Error("SyncServers: failed to parse native tools directory", keyError, parseErr)
		}

		var msg strings.Builder
		fmt.Fprintf(&msg, "Ecosystem synchronized. Connected: %d/%d servers.\n",
			len(result.Connected), len(result.Connected)+len(result.Failed))

		// 🛡️ VECTOR RECONCILIATION: Cross-reference HNSW graph against BadgerDB
		// to purge stale nodes persisted on disk from previously removed servers.
		if e := vector.GetEngine(); e != nil && e.VectorEnabled() {
			validURNs := h.Store.GetAllToolURNs()
			if len(validURNs) > 0 {
				if pruned := e.PruneOrphanedNodes(validURNs); pruned > 0 {
					fmt.Fprintf(&msg, "  Vector alignment: Pruned %d orphaned HNSW graph nodes.\n", pruned)
				}
			}
		}

		// 🛡️ METRIC RECONCILIATION: Force cross-namespace parity between tool: and intel: keys.
		// Deletes orphaned intel records and recalibrates atomic counters from actual DB state.
		if orphans, reconcileErr := h.Store.ReconcileMetrics(); reconcileErr == nil && orphans > 0 {
			fmt.Fprintf(&msg, "  Metric reconciliation: Purged %d orphaned intel records.\n", orphans)
		}

		if len(result.Connected) > 0 {
			fmt.Fprintf(&msg, "  Online: %s\n", strings.Join(result.Connected, ", "))
		}
		if len(result.Failed) > 0 {
			fmt.Fprintf(&msg, "  Failed: %s\n", strings.Join(result.Failed, ", "))
		}

		// 🛡️ HYDRATOR HOOK: Send non-blocking pulse to wake the hydrator daemon
		select {
		case h.HydratorSignal <- struct{}{}:
			slog.Debug("SyncServers: signaled hydrator daemon to wake")
		default:
			slog.Debug("SyncServers: hydrator daemon already evaluating limits, signal deduplicated")
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{
					Text: msg.String(),
				},
			},
		}, nil
	}

	// Targeted sync
	var (
		synced []string
		failed []string
		mu     sync.Mutex
		wg     sync.WaitGroup
	)

	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			var err error
			if n == serverMagictools {
				var listRes mcp.ListToolsResult
				if parseErr := json.Unmarshal(InternalToolsInventoryJSON, &listRes.Tools); parseErr == nil {
					_, err = h.Registry.SyncNativeTools(ctx, &listRes)
				} else {
					err = parseErr
				}
			} else {
				err = h.Registry.SyncServer(ctx, n)
			}

			mu.Lock()
			if err != nil {
				failed = append(failed, n)
			} else {
				synced = append(synced, n)
			}
			mu.Unlock()
		}(name)
	}

	wg.Wait()

	// 🛡️ HYDRATOR HOOK
	select {
	case h.HydratorSignal <- struct{}{}:
		slog.Debug("SyncServers: signaled hydrator daemon to wake")
	default:
		slog.Debug("SyncServers: hydrator daemon already evaluating limits, signal deduplicated")
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "Selective sync complete. %d/%d servers processed.\n", len(synced), len(names))
	if len(synced) > 0 {
		fmt.Fprintf(&msg, "  Online: %s\n", strings.Join(synced, ", "))
	}
	if len(failed) > 0 {
		fmt.Fprintf(&msg, "  Offline/Failed: %s\n", strings.Join(failed, ", "))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg.String()}},
	}, nil
}

func (h *OrchestratorHandler) registerMaintenanceTools(s *mcp.Server) {
	// Descriptions sourced from inventory.go via addTool().
	h.addTool(s, &mcp.Tool{Name: "wake_servers"}, h.WakeServers)
	h.addTool(s, &mcp.Tool{Name: toolReloadServers}, h.ReloadServers)
}

// WakeServers is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) WakeServers(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	servers := h.Config.GetManagedServers()

	var (
		alreadyOnline []string
		woken         []string
		failed        []string
		disabledCount int
		mu            sync.Mutex
		wg            sync.WaitGroup
	)

	for _, sc := range servers {
		if sc.Disabled {
			disabledCount++
			continue
		}
		if _, ok := h.Registry.GetServerSession(sc.Name); ok {
			alreadyOnline = append(alreadyOnline, sc.Name)
			continue
		}

		wg.Add(1)
		go func(srv config.ServerConfig) {
			defer wg.Done()
			if err := h.Registry.Connect(ctx, srv.Name, srv.Command, srv.Args, srv.Env, srv.Hash()); err != nil {
				slog.Error("wake_servers: JIT activation failed", "server", srv.Name, keyError, err)
				mu.Lock()
				failed = append(failed, srv.Name)
				mu.Unlock()
			} else {
				mu.Lock()
				woken = append(woken, srv.Name)
				mu.Unlock()
			}
		}(sc)
	}

	wg.Wait()

	// Verify all servers are responsive
	h.Registry.PingAll(ctx)

	var msg strings.Builder
	fmt.Fprintf(&msg, "Wake complete. %d/%d servers online.\n", len(alreadyOnline)+len(woken), len(servers)-disabledCount)
	if len(alreadyOnline) > 0 {
		fmt.Fprintf(&msg, "  Already online: %s\n", strings.Join(alreadyOnline, ", "))
	}
	if len(woken) > 0 {
		fmt.Fprintf(&msg, "  Woken: %s\n", strings.Join(woken, ", "))
	}
	if len(failed) > 0 {
		fmt.Fprintf(&msg, "  Failed: %s\n", strings.Join(failed, ", "))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg.String()}},
	}, nil
}

// ReloadServers is undocumented but satisfies standard structural requirements.
func (h *OrchestratorHandler) ReloadServers(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	defer h.alignGen.Add(1) // ALIGN-2: invalidate align cache — tool schemas may have changed
	var args struct {
		Names string `json:"names"`
	}
	if req.Params != nil && len(req.Params.Arguments) > 0 {
		unmarshalArgsOrWarn(req.Params.Arguments, &args)
	}

	// Case 1: Full Ecosystem Reload
	if args.Names == "" {
		slog.Info("reload_servers: initiating full ecosystem restart")
		h.Registry.DisconnectAll()
		result, err := h.Registry.SyncEcosystem(ctx)
		if err != nil {
			return nil, err
		}

		var msg strings.Builder
		fmt.Fprintf(&msg, "Ecosystem reloaded and synchronized. Connected: %d/%d servers.\n",
			len(result.Connected), len(result.Connected)+len(result.Failed))

		if len(result.Connected) > 0 {
			fmt.Fprintf(&msg, "  Online: %s\n", strings.Join(result.Connected, ", "))
		}
		if len(result.Failed) > 0 {
			fmt.Fprintf(&msg, "  Failed: %s\n", strings.Join(result.Failed, ", "))
		}

		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: msg.String()}},
		}, nil
	}

	// Case 2: Selective Parallel Reload
	names := strings.Fields(args.Names)
	slog.Info("reload_servers: initiating selective restart", "targets", names)
	return h.executeSelectiveReload(ctx, names), nil
}

func (h *OrchestratorHandler) executeSelectiveReload(ctx context.Context, names []string) *mcp.CallToolResult {
	var (
		restarted []string
		failed    []string
		mu        sync.Mutex
		wg        sync.WaitGroup
	)

	for _, name := range names {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			h.Registry.DisconnectServer(n, false)
			if err := h.Registry.SyncServer(ctx, n); err != nil {
				mu.Lock()
				failed = append(failed, n)
				mu.Unlock()
			} else {
				mu.Lock()
				restarted = append(restarted, n)
				mu.Unlock()
			}
		}(name)
	}

	wg.Wait()

	var msg strings.Builder
	fmt.Fprintf(&msg, "Selective reload complete. %d/%d servers processed.\n", len(restarted), len(names))
	if len(restarted) > 0 {
		fmt.Fprintf(&msg, "  Online: %s\n", strings.Join(restarted, ", "))
	}
	if len(failed) > 0 {
		fmt.Fprintf(&msg, "  Offline/Failed: %s\n", strings.Join(failed, ", "))
	}

	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: msg.String()}},
	}
}

// OnServerPromoted handles a server transitioning from magictools-managed to IDE-managed
func (h *OrchestratorHandler) OnServerPromoted(name string) {
	h.Registry.DisconnectServer(name, true)
	if err := h.Store.PurgeServerTools(name); err != nil {
		slog.Warn("Failed to purge server tools on promotion", "server", name, keyError, err)
	}
	h.alignGen.Add(1) // ALIGN-2: purged tools must not be served from the align cache
}

// OnServerDemoted handles a server transitioning from IDE-managed to magictools-managed
func (h *OrchestratorHandler) OnServerDemoted(name string) {
	slog.Info("server available for magictools management", "server", name)
}

// OnServerUpdated seamlessly reloads the sub-server process to apply new config parameters
func (h *OrchestratorHandler) OnServerUpdated(name string) {
	slog.Info("hot-reloading server due to parameter changes", "server", name)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	selectiveReloadOrWarn(ctx, h, []string{name})
}

// OnOverridesUpdated handles tool description override modifications via a targeted sync
func (h *OrchestratorHandler) OnOverridesUpdated(changedServers []string) {
	slog.Info("hot-reloading server tools due to override changes", "servers", changedServers)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	selectiveReloadOrWarn(ctx, h, changedServers)
}
