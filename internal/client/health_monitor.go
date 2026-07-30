// Package client provides functionality for the client subsystem.
package client

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"

	"path/filepath"

	"github.com/maccavelli/mcp-server-magictools/internal/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ServerStatus contains health info
type ServerStatus struct {
	Name              string `json:"name"`
	Running           bool   `json:"running"`
	Uptime            string `json:"uptime,omitempty,omitzero"`
	TotalCalls        int64  `json:"total_calls"`
	LastLatency       string `json:"last_latency,omitempty,omitzero"`
	PingLatency       string `json:"ping_latency,omitempty,omitzero"`
	ConsecutiveErrors int    `json:"consecutive_errors"`
	LastPing          string `json:"last_ping,omitempty,omitzero"`
	LastUsed          string `json:"last_used"`
	MemoryRSS         string `json:"memory_rss,omitempty,omitzero"`
	CPUUsage          string `json:"cpu_usage,omitempty,omitzero"`
}

// StartHealthMonitor runs a background check loop
func (m *WarmRegistry) StartHealthMonitor(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastFlush time.Time
	var cachedTrending map[string]map[string]float64
	var cachedDBTrending map[string]any

	runTick := func(now time.Time) {
		m.PollConfigChanges()
		m.MonitorResources()
		m.PingAll(ctx)
		m.EvictInactive(1 * time.Hour)
		m.PruneOrphans()

		flush := false
		if now.Sub(lastFlush) >= 1*time.Minute {
			flush = true
			lastFlush = now
			// Refresh trending from BadgerDB on flush ticks only
			if m.Store != nil {
				cachedTrending = m.Store.ComputeTrending()
				cachedDBTrending = m.Store.ComputeDatabaseTrending()
			}
		}

		// Compute scores on EVERY tick for real-time updates
		var dashboardScores map[string]any
		if m.Store != nil {
			dashboardScores = m.Store.ComputeScoreBoard(cachedTrending)
		}
		if dashboardScores == nil {
			dashboardScores = make(map[string]any)
		}

		m.WriteSnapshot(flush, dashboardScores, cachedDBTrending)
	}

	// 🛡️ COLD START ELIMINATION: Trigger the initial check/snapshot immediately on boot
	runTick(time.Now())

	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			runTick(now)
		}
	}
}

// PingAll is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) PingAll(ctx context.Context) {
	// Snapshot servers and their sessions under RLock.
	// After the lock is released, session pointers remain valid (GC keeps them alive).
	type pingTarget struct {
		srv  *SubServer
		sess *mcp.ClientSession
	}
	m.mu.RLock()
	var targets []pingTarget
	for _, s := range m.Servers {
		if s.Session != nil {
			targets = append(targets, pingTarget{srv: s, sess: s.Session})
		}
	}
	m.mu.RUnlock()

	var wg sync.WaitGroup
	sem := make(chan struct{}, 10)
	for _, t := range targets {
		wg.Add(1)
		go func(target pingTarget) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				m.executePingWithSession(ctx, target.srv, target.sess)
			case <-ctx.Done():
			}
		}(t)
	}
	wg.Wait()
}

// executePing was removed — it read srv.Session without lock (data race).
// All callers use executePingWithSession with a pre-snapshotted session instead.

// executePingWithSession pings using a pre-snapshotted session pointer,
// avoiding a racy read of srv.Session after the registry lock is released.
func (m *WarmRegistry) executePingWithSession(ctx context.Context, srv *SubServer, sess *mcp.ClientSession) {
	if sess == nil {
		return
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	start := time.Now()
	err := sess.Ping(pingCtx, nil)
	latency := time.Since(start)

	m.mu.Lock()
	srv.LastPing = time.Now()
	shouldRestart := false
	if err != nil {
		srv.ConsecutivePingFailures++
		if srv.ConsecutivePingFailures >= 3 {
			shouldRestart = true
		}
	} else {
		srv.PingLatency = latency
		srv.ConsecutivePingFailures = 0
		// Don't update LastUsed on pings — only real tool calls should count.
		// Otherwise idle eviction (EvictInactive) never fires.
	}
	m.mu.Unlock()

	if shouldRestart {
		m.orchestrateRestart(srv)
	}
}

// MonitorResources reads RSS and CPU for each running sub-server.
// NOTE: The PID snapshot taken under RLock can become stale if the process exits
// before GetRSS/GetProcessCPU are called. This is intentionally tolerated because
// those functions gracefully handle missing /proc entries by returning errors,
// which we silently skip. This avoids holding the lock during slow /proc reads.
func (m *WarmRegistry) MonitorResources() {
	type pidSnapshot struct {
		name string
		pid  int
		srv  *SubServer
	}
	var snapshots []pidSnapshot

	m.mu.RLock()
	for _, s := range m.Servers {
		if s.Process != nil && s.Process.Process != nil {
			snapshots = append(snapshots, pidSnapshot{s.Name, s.Process.Process.Pid, s})
		}
	}
	m.mu.RUnlock()

	// 🛡️ SELF-WATCHDOG: Always monitor the orchestrator's own process footprint
	snapshots = append(snapshots, pidSnapshot{name: "magictools (orchestrator)", pid: os.Getpid(), srv: nil})

	for _, snap := range snapshots {
		rss, err := util.GetRSS(snap.pid)
		if err == nil {
			m.mu.Lock()
			if snap.srv != nil {
				snap.srv.MemoryRSS = rss
			}
			m.mu.Unlock()

			limitBytes := uint64(2048 * 1024 * 1024)
			if snap.srv != nil && snap.srv.MemoryLimitMB > 0 {
				limitBytes = uint64(snap.srv.MemoryLimitMB) * 1024 * 1024
			}
			if rss > limitBytes {
				slog.Warn("memory limit exceeded, restarting", "component", "watchdog", "server", snap.name, "rss", rss, "limit", limitBytes)
				if snap.srv != nil {
					// 🛡️ ACTIVE-CALL GUARD: Defer restart if the server has in-flight proxy calls.
					// This prevents killing go-modernizer mid-flight during long-running tools
					// like go_test_validation, which is the root cause of proxy EOF errors.
					if active := snap.srv.ActiveCalls.Load(); active > 0 {
						slog.Warn("memory limit exceeded but server has active calls — deferring restart",
							"component", "watchdog",
							"server", snap.name,
							"rss", rss,
							"limit", limitBytes,
							"active_calls", active,
						)
						continue
					}
					m.orchestrateRestart(snap.srv)
				} else {
					// This is the orchestrator itself. Panic to trigger the IDE's auto-restart
					// and prevent runaway resource exhaustion.
					panic(fmt.Sprintf("Orchestrator self-watchdog: memory limit exceeded: %d bytes", rss))
				}
				continue
			}
		}

		cpu, err := util.GetProcessCPU(snap.pid)
		if err == nil {
			m.mu.Lock()
			if snap.srv != nil {
				snap.srv.CPUUsage = cpu
			}
			m.mu.Unlock()

			if cpu > 95.0 {
				slog.Warn("high cpu detected", "component", "watchdog", "server", snap.name, "cpu", cpu)
			}
		}
	}
}

// EvictInactive disconnects servers that have been idle longer than ttl,
// unless they are pinned or have in-flight proxy calls.
func (m *WarmRegistry) EvictInactive(ttl time.Duration) {
	pinned := m.pinnedSet()
	type evictionCandidate struct {
		name     string
		idleTime time.Duration
	}
	var toEvict []evictionCandidate
	m.mu.RLock()
	now := time.Now()
	for name, s := range m.Servers {
		if pinned[name] {
			continue
		}
		// 🛡️ HARDEN-1: Skip servers with in-flight proxy calls to prevent
		// killing a process mid-execution and causing EOF errors for callers.
		if s.ActiveCalls.Load() > 0 {
			continue
		}
		if s.Session != nil && now.Sub(s.LastUsed) > ttl {
			toEvict = append(toEvict, evictionCandidate{name: name, idleTime: now.Sub(s.LastUsed)})
		}
	}
	m.mu.RUnlock()

	for _, c := range toEvict {
		// 🛡️ HARDEN-2: Log which server is being evicted and why for diagnostics.
		slog.Info("lifecycle: evicting idle server",
			"server", c.name,
			"idle_duration", c.idleTime.Round(time.Second).String(),
			"reason", "idle_ttl_exceeded",
		)
		telemetry.LifecycleEvents.EvictionsEviction.Add(1)
		m.DisconnectServer(c.name, true)
	}
}

// GetStatusReport is undocumented but satisfies standard structural requirements.
func (m *WarmRegistry) GetStatusReport(managed []string) []ServerStatus {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var report []ServerStatus
	for _, name := range managed {
		s, running := m.Servers[name]
		status := ServerStatus{
			Name:    name,
			Running: running && s.Session != nil,
		}
		if running && s.Session != nil {
			status.Uptime = time.Since(s.StartTime).Round(time.Second).String()
			status.TotalCalls = s.TotalCalls
			status.LastLatency = s.LastLatency.Round(time.Millisecond).String()
			status.PingLatency = s.PingLatency.Round(time.Millisecond).String()
			if !s.LastPing.IsZero() {
				status.LastPing = time.Since(s.LastPing).Round(time.Second).String() + " ago"
			}
			status.LastUsed = s.LastUsed.Format(time.Kitchen)
			status.ConsecutiveErrors = s.ConsecutiveErrors
			if s.MemoryRSS > 0 {
				status.MemoryRSS = fmt.Sprintf("%.2f MB", float64(s.MemoryRSS)/1024/1024)
			}
			if s.CPUUsage > 0 {
				status.CPUUsage = fmt.Sprintf("%.1f%%", s.CPUUsage)
			}
		} else {
			status.LastUsed = "Disconnected"
		}
		report = append(report, status)
	}
	return report
}

// EvictLRU evicts the least-recently-used server if the active count exceeds MaxRunningServers.
// LOCK CONTRACT: This method acquires m.mu.Lock internally, then releases it BEFORE calling
// DisconnectServer (which also acquires m.mu.Lock). This ordering prevents deadlocks.
// Do NOT refactor to hold the lock across the DisconnectServer call.
func (m *WarmRegistry) EvictLRU(excludeName string) {
	// PROXY-M5: cheap pre-check before the write-lock + full O(n) scan that
	// previously ran on every successful proxy call. If the total server count
	// can't exceed the cap, there is nothing to evict.
	m.mu.RLock()
	total := len(m.Servers)
	m.mu.RUnlock()
	if total <= MaxRunningServers {
		return
	}

	pinned := m.pinnedSet()
	m.mu.Lock()
	var active []*SubServer
	for _, s := range m.Servers {
		if s.Session != nil && s.Name != excludeName && !pinned[s.Name] && s.ActiveCalls.Load() == 0 {
			active = append(active, s)
		}
	}

	if len(active) <= MaxRunningServers {
		m.mu.Unlock()
		return
	}

	// O(n) min-scan: find the oldest server by LastUsed
	oldest := active[0]
	for _, s := range active[1:] {
		if s.LastUsed.Before(oldest.LastUsed) {
			oldest = s
		}
	}

	m.mu.Unlock()
	telemetry.LifecycleEvents.EvictionsEviction.Add(1)
	m.DisconnectServer(oldest.Name, true)
}

// pinnedSet returns a set of server names that are exempt from eviction.
func (m *WarmRegistry) pinnedSet() map[string]bool {
	var names []string
	if m.Config != nil {
		names = m.Config.GetPinnedServers()
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set
}

// PollConfigChanges hot-reloads servers.yaml and adjusts sub-server bounds on the fly
func (m *WarmRegistry) PollConfigChanges() {
	path := filepath.Join(config.DefaultConfigDir(), config.ServersConfigFile)
	info, err := os.Stat(path)
	if err != nil {
		return
	}

	m.mu.Lock()
	if !info.ModTime().After(m.lastConfigModTime) {
		m.mu.Unlock()
		return
	}
	m.lastConfigModTime = info.ModTime()
	m.mu.Unlock()

	newServers, err := config.LoadManagedServers()
	if err != nil {
		slog.Error("PollConfigChanges: failed to hot-reload servers.yaml", "error", err)
		return
	}

	m.Config.UpdateManagedServers(newServers)

	for _, ns := range newServers {
		m.mu.Lock()
		s, ok := m.Servers[ns.Name]
		if ok && s.ConfigHash != ns.Hash() {
			slog.Info("detected limit changes; orchestrating restart", "component", "watchdog", "server", ns.Name)
			s.Command = ns.Command
			s.Args = ns.Args
			s.Env = ns.Env
			s.MemoryLimitMB = ns.MemoryLimitMB
			s.GoMemLimitMB = ns.GoMemLimitMB
			s.MaxCPULimit = ns.MaxCPULimit
			s.ConfigHash = ns.Hash()
			m.mu.Unlock()
			m.orchestrateRestart(s)
		} else {
			m.mu.Unlock()
		}
	}
}

// WriteSnapshot gathers global observability metrics and writes them to the memory-mapped ring buffer.
func (m *WarmRegistry) WriteSnapshot(flush bool, dashboardScores map[string]any, databasesHistory map[string]any) {
	if telemetry.GlobalRingBuffer == nil && !flush {
		return
	}

	ctx := m.collectSnapshotContext(dashboardScores)
	recallDBMetrics := fetchRecallDBMetrics(ctx.recallSess)
	snapshot := buildBaseSnapshot(ctx, recallDBMetrics, databasesHistory, m.Store.GetMetrics())

	attachConfigSnapshot(snapshot, m, ctx.managedServers)
	attachProxySnapshot(snapshot)
	attachRuntimeSnapshots(snapshot)
	attachRegistrySnapshot(snapshot, m, ctx.managedServers)

	factors, volatility := buildScoringTelemetry(ctx.scoresPayload)
	snapshot["scoring_factors"] = factors
	snapshot["volatility_index"] = volatility

	attachNetworkDynamics(snapshot)
	attachSearchSnapshots(snapshot, m)
	attachDistributedTracing(snapshot, m)
	attachIPCSessions(snapshot)
	snapshot["proxy_mux"] = telemetry.StdioMux.Snapshot()
	m.attachFirewallSnapshot(snapshot)
	attachUDPTelemetry(snapshot)

	persistSnapshot(snapshot, flush, m)
}

// getHNSWGraphSize returns the current HNSW graph node count, or 0 if the engine is disabled.
func getHNSWGraphSize() int {
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return 0
	}
	return e.Len()
}

// getFusionModeLabel returns a human-readable label for the active search fusion mode.
func getFusionModeLabel(cfg *config.Config) string {
	if cfg == nil {
		return "Lexical-Only"
	}
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return "Lexical-Only (BM25)"
	}
	return fmt.Sprintf("Hybrid (α=%.2f)", cfg.ScoreFusionAlpha)
}

// calculateDirSizeMB calculates the total size of a directory in MB.
func calculateDirSizeMB(path string) float64 {
	var size int64
	err := filepath.Walk(path, func(_ string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !info.IsDir() {
			size += info.Size()
		}
		return nil
	})
	if err != nil {
		return 0
	}
	return float64(size) / (1024 * 1024)
}
