// Package config provides functionality for the config subsystem.
package config

import (
	"crypto/sha256"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// ConfigChangeHandler is called when the managed server set changes
type ConfigChangeHandler interface {
	// OnServerPromoted is called when a server transitions from disabled->enabled
	// (magictools loses ownership). Its tools should be purged from the index.
	OnServerPromoted(name string)

	// OnServerDemoted is called when a server transitions from enabled->disabled
	// (magictools gains ownership). Available for next sync_ecosystem.
	OnServerDemoted(name string)

	// OnServerUpdated is called when a server's configuration parameters change.
	OnServerUpdated(name string)

	// OnConfigReloaded is called when the configuration file has been re-read.
	OnConfigReloaded(cfg *Config)

	// OnMCPLogLevelChanged is called when the global MCPLogLevel configuration changes.
	OnMCPLogLevelChanged(oldLevel, newLevel string)

	// OnOverridesUpdated is called when tool_overrides.yaml changes.
	OnOverridesUpdated(changedServers []string)
}

// Watcher monitors config.yaml (via Viper) and servers.yaml (via fsnotify)
type Watcher struct {
	v          *viper.Viper
	liveConfig *Config
	handler    ConfigChangeHandler
	current    map[string]bool // current managed server names
	mu         sync.Mutex
	stop       chan struct{}
	lastHash   [32]byte // config.yaml hash
	srvHash    [32]byte // servers.yaml hash
	ovrHash    [32]byte // tool_overrides.yaml hash
}

// NewWatcher creates a config file watcher
func NewWatcher(v *viper.Viper, cfg *Config, handler ConfigChangeHandler) *Watcher {
	return &Watcher{
		v:          v,
		liveConfig: cfg,
		handler:    handler,
		current:    cfg.GetManagedServerNames(),
		stop:       make(chan struct{}),
	}
}

// Start begins watching config.yaml and servers.yaml for changes.
func (w *Watcher) Start() {
	// Watch config.yaml via Viper (orchestrator settings)
	w.v.OnConfigChange(func(e fsnotify.Event) {
		slog.Info("file changed", "component", "config", "path", e.Name, "op", e.Op.String())
		w.handleChange()
	})
	w.v.WatchConfig()
	slog.Info("watcher started", "component", "config", "path", w.v.ConfigFileUsed())

	// Watch servers.yaml via fsnotify (sub-server registry)
	serversPath := filepath.Join(DefaultConfigDir(), ServersConfigFile)
	go w.watchServersFile(serversPath)

	// Watch tool_overrides.yaml via fsnotify
	overridesPath := filepath.Join(DefaultConfigDir(), "tool_overrides.yaml")
	go w.watchOverridesFile(overridesPath)

	// Hardening: Fallback polling for Bastion hosts where fsnotify might fail
	go func(stop chan struct{}) {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				w.handleChange()
				w.handleServersChange()
				w.handleOverridesChange()
			}
		}
	}(w.stop)
}

// Reload forces an immediate re-read of config.yaml, servers.yaml and
// tool_overrides.yaml, equivalent to a file-change event. It is used by the
// service to handle SIGHUP (graceful config-reload) without restarting. All
// three handlers are hash-gated internally, so a no-op reload is cheap.
func (w *Watcher) Reload() {
	slog.Info("manual config reload requested (SIGHUP)", "component", "config")
	w.handleChange()
	w.handleServersChange()
	w.handleOverridesChange()
}

// watchServersFile uses fsnotify to watch servers.yaml for changes.
func (w *Watcher) watchServersFile(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("servers.yaml: fsnotify unavailable, relying on polling", "error", err)
		return
	}
	defer func() {
		if closeErr := watcher.Close(); closeErr != nil {
			slog.Warn("servers.yaml: watcher close failed", "error", closeErr)
		}
	}()

	if err := watcher.Add(filepath.Dir(path)); err != nil {
		slog.Warn("servers.yaml: failed to watch directory, relying on polling", "error", err)
		return
	}

	slog.Info("servers.yaml watcher started", "component", "config", "path", path)
	for {
		select {
		case <-w.stop:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) == ServersConfigFile && (event.Op&(fsnotify.Write|fsnotify.Create)) != 0 {
				slog.Info("servers.yaml changed", "component", "config", "op", event.Op.String())
				w.handleServersChange()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("servers.yaml: fsnotify error", "error", err)
		}
	}
}

// Stop signals the watcher to shut down
func (w *Watcher) Stop() {
	select {
	case <-w.stop:
		// already closed
	default:
		close(w.stop)
	}
}

// UpdateManaged replaces the current managed set (called after sync_ecosystem)
func (w *Watcher) UpdateManaged(managed map[string]bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.current = managed
}

// handleChange processes config.yaml changes (orchestrator settings only).
func (w *Watcher) handleChange() {
	// 🛡️ HASH CHECK: Skip redundant reloads when the config file hasn't changed.
	raw, err := os.ReadFile(w.v.ConfigFileUsed())
	if err != nil {
		slog.Error("failed to read config file", "component", "config", "error", err)
		return
	}
	newHash := sha256.Sum256(raw)
	w.mu.Lock()
	if newHash == w.lastHash {
		w.mu.Unlock()
		return
	}
	w.lastHash = newHash
	w.mu.Unlock()

	cfg, err := LoadFromViper(w.v)
	if err != nil {
		slog.Error("failed to reload config", "component", "config", "error", err)
		return
	}

	// Update orchestrator settings (NOT managed servers — those come from servers.yaml)
	w.liveConfig.mu.Lock()
	oldMCPLogLevel := w.liveConfig.MCPLogLevel
	w.liveConfig.MaxResponseTokens = cfg.MaxResponseTokens
	w.liveConfig.LogLevel = cfg.LogLevel
	w.liveConfig.MCPLogLevel = cfg.MCPLogLevel
	w.liveConfig.LogFormat = cfg.LogFormat
	w.liveConfig.SqueezeLevelState = cfg.SqueezeLevelState
	w.liveConfig.ScoreThreshold = cfg.ScoreThreshold
	w.liveConfig.StrictGates = cfg.StrictGates
	w.liveConfig.VectorMinCosine = cfg.VectorMinCosine
	w.liveConfig.BM25MinNormalized = cfg.BM25MinNormalized
	w.liveConfig.DisableSearchFallback = cfg.DisableSearchFallback
	w.liveConfig.ValidateProxyCalls = cfg.ValidateProxyCalls
	w.liveConfig.SqueezeBypass = cfg.SqueezeBypass
	w.liveConfig.RingBufferTargets = cfg.RingBufferTargets
	w.liveConfig.PinnedServers = cfg.PinnedServers
	w.liveConfig.TrustServers = cfg.TrustServers
	w.liveConfig.TokenSpendThresh = cfg.TokenSpendThresh
	w.liveConfig.LRULimit = cfg.LRULimit
	w.liveConfig.SynthesisBiasVector = cfg.SynthesisBiasVector
	w.liveConfig.SynthesisBiasSynergy = cfg.SynthesisBiasSynergy
	w.liveConfig.SynthesisBiasRole = cfg.SynthesisBiasRole
	w.liveConfig.ScoreFusionAlpha = cfg.ScoreFusionAlpha
	w.liveConfig.CorroborationBonus = cfg.CorroborationBonus
	w.liveConfig.ReliabilityBoost = cfg.ReliabilityBoost
	w.liveConfig.UsageBoost = cfg.UsageBoost
	w.liveConfig.NativeBoost = cfg.NativeBoost
	w.liveConfig.ConfidenceGap = cfg.ConfidenceGap
	w.liveConfig.Intelligence = cfg.Intelligence
	w.liveConfig.mu.Unlock()

	if cfg.StrictGates && cfg.DisableSearchFallback && cfg.ScoreThreshold >= 0.5 {
		slog.Warn("search config may block natural-language align_tools queries",
			"component", "config",
			"strictGates", cfg.StrictGates,
			"disableSearchFallback", cfg.DisableSearchFallback,
			"scoreThreshold", cfg.ScoreThreshold,
			"hint", "use scoreThreshold ~0.30, strictGates false, disableSearchFallback false for NL-friendly routing")
	}

	w.handler.OnConfigReloaded(w.liveConfig)
	// 🛡️ TRANSIENT STATE VALIDATION: sed -i creates a new file and moves it, triggering multiple
	// fsnotify events. Viper can occasionally read the file during the microsecond it is empty or half-written,
	// causing MCPLogLevel to unmarshal as "". If we accept this, it triggers a massive false-positive ecosystem
	// restart. We must explicitly require that BOTH the old and new levels are fully populated to prevent this.
	if cfg.MCPLogLevel != "" && oldMCPLogLevel != "" && oldMCPLogLevel != cfg.MCPLogLevel {
		slog.Warn("sub-server log level mutated; notifying fleet controller", "component", "config", "old", oldMCPLogLevel, "new", cfg.MCPLogLevel)
		w.handler.OnMCPLogLevelChanged(oldMCPLogLevel, cfg.MCPLogLevel)
	} else if cfg.MCPLogLevel == "" && oldMCPLogLevel != "" {
		// Transient empty read detected. Do not trigger a reload. The next fully-written config event
		// will overwrite the in-memory value back to its stable state.
		slog.Debug("transient empty config read filtered to prevent false-positive reload cascade", "component", "config")
	}
}

// handleServersChange processes servers.yaml changes (sub-server registry).
func (w *Watcher) handleServersChange() {
	path := filepath.Join(DefaultConfigDir(), ServersConfigFile)
	raw, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // path constructed from controlled config directory
	if err != nil {
		slog.Error("failed to read servers.yaml", "component", "config", "error", err)
		return
	}
	newHash := sha256.Sum256(raw)
	w.mu.Lock()
	if newHash == w.srvHash {
		w.mu.Unlock()
		return
	}
	w.srvHash = newHash
	w.mu.Unlock()

	servers, err := LoadManagedServers()
	if err != nil {
		slog.Error("failed to reload servers.yaml", "component", "config", "error", err)
		return
	}

	oldServersList := w.liveConfig.GetManagedServers()
	oldServersMap := make(map[string]string)
	for _, sc := range oldServersList {
		oldServersMap[sc.Name] = sc.Hash()
	}

	// Update the live config with new server list
	w.liveConfig.UpdateManagedServers(servers)
	newManaged := w.liveConfig.GetManagedServerNames()

	w.mu.Lock()
	oldManaged := w.current
	w.current = newManaged
	w.mu.Unlock()

	// Detect servers removed from servers.yaml
	for name := range oldManaged {
		if !newManaged[name] {
			slog.Info("server removed from servers.yaml", "component", "config", "server_id", name)
			w.handler.OnServerPromoted(name)
		}
	}

	// Detect servers added to servers.yaml or parameters changed
	for name := range newManaged {
		if !oldManaged[name] {
			slog.Info("server added to servers.yaml", "component", "config", "server_id", name)
			w.handler.OnServerDemoted(name)
		} else {
			// Check if parameters changed
			var newHash string
			for _, sc := range servers {
				if sc.Name == name {
					newHash = sc.Hash()
					break
				}
			}
			if oldHash, ok := oldServersMap[name]; ok && newHash != "" && oldHash != newHash {
				slog.Info("server config mutated in servers.yaml", "component", "config", "server_id", name)
				w.handler.OnServerUpdated(name)
			}
		}
	}

	slog.Info("servers.yaml reloaded", "component", "config", "servers", len(servers))
}

// watchOverridesFile uses fsnotify to watch tool_overrides.yaml for changes.
func (w *Watcher) watchOverridesFile(path string) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("tool_overrides.yaml: fsnotify unavailable, relying on polling", "error", err)
		return
	}
	defer func() {
		if closeErr := watcher.Close(); closeErr != nil {
			slog.Warn("tool_overrides.yaml: watcher close failed", "error", closeErr)
		}
	}()

	if err := watcher.Add(filepath.Dir(path)); err != nil {
		slog.Warn("tool_overrides.yaml: failed to watch directory, relying on polling", "error", err)
		return
	}

	slog.Info("tool_overrides.yaml watcher started", "component", "config", "path", path)
	for {
		select {
		case <-w.stop:
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if filepath.Base(event.Name) == "tool_overrides.yaml" && (event.Op&(fsnotify.Write|fsnotify.Create)) != 0 {
				slog.Info("tool_overrides.yaml changed", "component", "config", "op", event.Op.String())
				w.handleOverridesChange()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("tool_overrides.yaml: fsnotify error", "error", err)
		}
	}
}

// handleOverridesChange processes tool_overrides.yaml changes.
func (w *Watcher) handleOverridesChange() {
	path := filepath.Join(DefaultConfigDir(), "tool_overrides.yaml")
	raw, err := os.ReadFile(filepath.Clean(path)) //nolint:gosec // path constructed from controlled config directory
	if err != nil {
		if os.IsNotExist(err) {
			raw = []byte("")
		} else {
			slog.Error("failed to read tool_overrides.yaml", "component", "config", "error", err)
			return
		}
	}
	newHash := sha256.Sum256(raw)
	w.mu.Lock()
	if newHash == w.ovrHash {
		w.mu.Unlock()
		return
	}
	w.ovrHash = newHash
	w.mu.Unlock()

	var newOverrides ToolOverridesConfig
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &newOverrides); err != nil {
			slog.Error("failed to parse tool_overrides.yaml", "component", "config", "error", err)
			return
		}
	}

	w.liveConfig.mu.RLock()
	oldOverrides := w.liveConfig.ToolOverrides
	w.liveConfig.mu.RUnlock()

	// Compute diff
	changedServers := make(map[string]bool)
	for serverName, newTools := range newOverrides.Overrides {
		oldTools, ok := oldOverrides.Overrides[serverName]
		if !ok {
			changedServers[serverName] = true
			continue
		}
		for toolName, newOverride := range newTools {
			if oldOverride, ok := oldTools[toolName]; !ok || oldOverride.Description != newOverride.Description {
				changedServers[serverName] = true
				break
			}
		}
		for toolName := range oldTools {
			if _, ok := newTools[toolName]; !ok {
				changedServers[serverName] = true
				break
			}
		}
	}
	for serverName := range oldOverrides.Overrides {
		if _, ok := newOverrides.Overrides[serverName]; !ok {
			changedServers[serverName] = true
		}
	}

	w.liveConfig.UpdateToolOverrides(newOverrides)

	if len(changedServers) > 0 {
		var serversToSync []string
		for srv := range changedServers {
			serversToSync = append(serversToSync, srv)
		}
		slog.Info("tool overrides reloaded", "component", "config", "changed_servers", serversToSync)
		w.handler.OnOverridesUpdated(serversToSync)
	}
}
