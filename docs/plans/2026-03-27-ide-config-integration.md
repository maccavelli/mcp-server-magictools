# IDE Config Integration Implementation Plan

> **For Antigravity:** REQUIRED WORKFLOW: Use `.agent/workflows/execute-plan.md` to execute this plan in single-flow mode.

**Goal:** Refactor mcp-server-magictools to read the IDE's `mcp_config.json` directly (inverted ownership model), add fsnotify config watching, ephemeral sync lifecycle, LRU process eviction for proxy calls, and cross-platform signal handling.

**Architecture:** The config layer parses the IDE's `{mcpServers: {...}}` format and filters for `disabled: true` entries (magictools-managed). A file watcher detects runtime config changes. Sync spawns sub-servers ephemerally with `context.WithTimeout`. Proxy calls lazy-spawn servers with LRU eviction (cap at 10 concurrent processes). Build-tagged signal files handle Unix vs Windows differences.

**Tech Stack:** Go 1.26.1, fsnotify/fsnotify, badger/v4, go-sdk MCP, errgroup

**Ref:** [Design Doc](file:///home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools/docs/plans/2026-03-27-ide-config-integration-design.md)

---

### Task 1: Add fsnotify Dependency

**Files:**
- Modify: `go.mod`
- Modify: `go.sum` (auto-generated)

**Step 1: Add fsnotify to go.mod**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go get github.com/fsnotify/fsnotify@latest
```
Expected: go.mod updated with `github.com/fsnotify/fsnotify` in require block.

**Step 2: Vendor the dependency**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go mod vendor
```
Expected: `vendor/github.com/fsnotify/` directory created.

**Step 3: Verify build**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build ./...
```
Expected: No errors. Existing code still compiles.

**Step 4: Stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/go.mod scripts/go/mcp-server-magictools/go.sum scripts/go/mcp-server-magictools/vendor/
```

---

### Task 2: Rewrite Config Layer to Parse IDE Format

**Files:**
- Rewrite: `internal/config/config.go`
- Delete: `internal/config/meta-servers.json`

**Step 1: Write the new `config.go`**

Replace the entire file. The new config parser reads the IDE `mcp_config.json` format (`{mcpServers: {name: {command, args, env, disabled, disabledTools}}}`). It exposes:

- `IDEConfig` / `IDEServerEntry` structs matching the IDE JSON format
- `Config` struct with `ManagedServers []ServerConfig` (only `disabled: true` entries, excluding `magictools` itself)
- `ServerConfig` now includes `Env map[string]string` for passthrough
- `Load(path string)` parses the file and filters
- `New(version string)` does path discovery (flag → env → default `~/.gemini/antigravity/mcp_config.json`)
- `GetConfigPath()` returns the resolved path (needed by watcher)

```go
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	Name          = "mcp-server-magictools"
	DefaultDBPath = ".mcp_magictools"
	EnvDBPath     = "MCP_MAGIC_TOOLS_DB_PATH"
	EnvConfigPath = "MCP_MAGIC_TOOLS_CONFIG"
	SelfName      = "magictools"
)

// IDEConfig matches the IDE's mcp_config.json top-level structure
type IDEConfig struct {
	McpServers map[string]IDEServerEntry `json:"mcpServers"`
}

// IDEServerEntry matches a single server entry in the IDE config
type IDEServerEntry struct {
	Command       string            `json:"command"`
	Args          []string          `json:"args"`
	Env           map[string]string `json:"env,omitempty"`
	Disabled      bool              `json:"disabled"`
	DisabledTools []string          `json:"disabledTools,omitempty"`
}

// ServerConfig defines a magictools-managed sub-server (derived from IDE entries with disabled: true)
type ServerConfig struct {
	Name          string
	Command       string
	Args          []string
	Env           map[string]string
	DisabledTools []string
}

// Config holds the application configuration
type Config struct {
	Name           string
	Version        string
	DBPath         string
	ConfigPath     string
	ManagedServers []ServerConfig
	ideConfig      *IDEConfig // raw parsed config for diffing
}

// New initializes configuration with path discovery: flag → env → default
func New(version, flagPath string) (*Config, error) {
	dbPath := os.Getenv(EnvDBPath)
	if dbPath == "" {
		home, _ := os.UserHomeDir()
		dbPath = filepath.Join(home, DefaultDBPath)
	}

	configPath := flagPath
	if configPath == "" {
		configPath = os.Getenv(EnvConfigPath)
	}
	if configPath == "" {
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".gemini", "antigravity", "mcp_config.json")
	}

	cfg, err := Load(configPath)
	if err != nil {
		return nil, err
	}
	cfg.Name = Name
	cfg.Version = version
	cfg.DBPath = dbPath
	return cfg, nil
}

// Load parses the IDE config file and extracts managed servers
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config load (%s): %w", path, err)
	}

	var ide IDEConfig
	if err := json.Unmarshal(data, &ide); err != nil {
		return nil, fmt.Errorf("config parse (%s): %w", path, err)
	}

	managed := extractManaged(&ide)

	return &Config{
		ConfigPath:     path,
		ManagedServers: managed,
		ideConfig:      &ide,
	}, nil
}

// Reload re-reads the config file and returns a new Config
func (c *Config) Reload() (*Config, error) {
	return Load(c.ConfigPath)
}

// GetManagedServerNames returns just the names of managed servers
func (c *Config) GetManagedServerNames() map[string]bool {
	names := make(map[string]bool, len(c.ManagedServers))
	for _, s := range c.ManagedServers {
		names[s.Name] = true
	}
	return names
}

// extractManaged filters IDE config for disabled: true entries, excluding self
func extractManaged(ide *IDEConfig) []ServerConfig {
	var servers []ServerConfig
	for name, entry := range ide.McpServers {
		if name == SelfName {
			continue // never manage ourselves
		}
		if !entry.Disabled {
			continue // IDE manages enabled servers
		}
		servers = append(servers, ServerConfig{
			Name:          name,
			Command:       entry.Command,
			Args:          entry.Args,
			Env:           entry.Env,
			DisabledTools: entry.DisabledTools,
		})
	}
	return servers
}
```

**Step 2: Delete `meta-servers.json`**

Run:
```bash
rm /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools/internal/config/meta-servers.json
```

**Step 3: Verify build compiles**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build ./internal/config/
```
Expected: Compiles cleanly. Note that `main.go` and `engine/sync.go` will NOT compile yet (they use old API). That's expected — we fix them in later tasks.

**Step 4: Stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/internal/config/
```

---

### Task 3: Create Config File Watcher

**Files:**
- Create: `internal/config/watcher.go`

**Step 1: Write the watcher**

The watcher uses `fsnotify` to monitor the IDE config file. On change, it debounces (200ms), re-parses, diffs old vs new managed sets, and calls the handler interface.

```go
package config

import (
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// ConfigChangeHandler is called when the managed server set changes
type ConfigChangeHandler interface {
	// OnServerPromoted is called when a server transitions from disabled→enabled
	// (magictools loses ownership). Its tools should be purged from the index.
	OnServerPromoted(name string)

	// OnServerDemoted is called when a server transitions from enabled→disabled
	// (magictools gains ownership). Available for next sync_ecosystem.
	OnServerDemoted(name string)
}

// Watcher monitors the IDE config file for changes
type Watcher struct {
	configPath string
	handler    ConfigChangeHandler
	current    map[string]bool // current managed server names
	mu         sync.Mutex
	stop       chan struct{}
}

// NewWatcher creates a config file watcher
func NewWatcher(configPath string, initialManaged map[string]bool, handler ConfigChangeHandler) *Watcher {
	return &Watcher{
		configPath: configPath,
		handler:    handler,
		current:    initialManaged,
		stop:       make(chan struct{}),
	}
}

// Start begins watching the config file. Blocks until Stop is called or ctx is done.
func (w *Watcher) Start() error {
	fsw, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	defer fsw.Close()

	if err := fsw.Add(w.configPath); err != nil {
		return err
	}

	slog.Info("config watcher started", "path", w.configPath)

	var debounceTimer *time.Timer
	for {
		select {
		case <-w.stop:
			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			slog.Info("config watcher stopped")
			return nil

		case event, ok := <-fsw.Events:
			if !ok {
				return nil
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				// Debounce: wait 200ms for rapid successive writes
				if debounceTimer != nil {
					debounceTimer.Stop()
				}
				debounceTimer = time.AfterFunc(200*time.Millisecond, func() {
					w.handleChange()
				})
			}

		case err, ok := <-fsw.Errors:
			if !ok {
				return nil
			}
			slog.Error("config watcher error", "error", err)
		}
	}
}

// Stop signals the watcher to shut down
func (w *Watcher) Stop() {
	close(w.stop)
}

// UpdateManaged replaces the current managed set (called after sync_ecosystem)
func (w *Watcher) UpdateManaged(managed map[string]bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.current = managed
}

func (w *Watcher) handleChange() {
	cfg, err := Load(w.configPath)
	if err != nil {
		slog.Error("config watcher: failed to reload config", "error", err)
		return
	}

	newManaged := cfg.GetManagedServerNames()

	w.mu.Lock()
	oldManaged := w.current
	w.current = newManaged
	w.mu.Unlock()

	// Detect promotions: was managed (disabled:true), now NOT managed (disabled:false)
	for name := range oldManaged {
		if !newManaged[name] {
			slog.Info("config watcher: server promoted (IDE now manages)", "server", name)
			w.handler.OnServerPromoted(name)
		}
	}

	// Detect demotions: was NOT managed, now IS managed (disabled:true)
	for name := range newManaged {
		if !oldManaged[name] {
			slog.Info("config watcher: server demoted (magictools now manages)", "server", name)
			w.handler.OnServerDemoted(name)
		}
	}
}
```

**Step 2: Verify build**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build ./internal/config/
```
Expected: Compiles cleanly.

**Step 3: Stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/internal/config/watcher.go
```

---

### Task 4: Refactor Client Manager (LRU, Env Passthrough, DisconnectServer)

**Files:**
- Rewrite: `internal/client/manager.go`

**Step 1: Rewrite the manager**

Key changes from current:
- `SubServer.LastUsed` field for LRU tracking
- `Connect()` accepts `env map[string]string` for IDE config passthrough
- `DisconnectServer(name)` for single-server cleanup
- `DisconnectAll()` replaces `CloseAll()` (same behavior, clearer name, also keeps `CloseAll` as alias)
- `EvictLRU()` kills least recently used server when count exceeds threshold (10)
- Remove `Monitor()` goroutine (no persistent connections needed)
- `RunningCount()` returns the number of active sub-server processes
- `CallProxy()` now updates `LastUsed` and triggers LRU eviction

```go
package client

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const MaxRunningServers = 10

// SubServer instance
type SubServer struct {
	Name              string
	Session           *mcp.ClientSession
	Process           *exec.Cmd
	LastUsed          time.Time
	ConsecutiveErrors int
	LastFailure       time.Time
}

// Manager handles a pool of sub-servers
type Manager struct {
	Servers     map[string]*SubServer
	serverLocks map[string]*sync.Mutex
	mu          sync.RWMutex
}

// NewManager creates a manager
func NewManager() *Manager {
	return &Manager{
		Servers:     make(map[string]*SubServer),
		serverLocks: make(map[string]*sync.Mutex),
	}
}

// Connect starts a sub-server and attaches an MCP client.
// env is passed through from the IDE config. If nil, uses a safe whitelist.
func (m *Manager) Connect(ctx context.Context, name, command string, args []string, env map[string]string) error {
	// Ensure per-server lock exists
	m.mu.Lock()
	if _, ok := m.serverLocks[name]; !ok {
		m.serverLocks[name] = &sync.Mutex{}
	}
	serverMu := m.serverLocks[name]
	m.mu.Unlock()

	serverMu.Lock()
	defer serverMu.Unlock()

	// Circuit Breaker Cooldown (1-minute after 3 failures)
	m.mu.RLock()
	s, exists := m.Servers[name]
	m.mu.RUnlock()
	if exists {
		if s.Session != nil {
			s.LastUsed = time.Now()
			return nil
		}
		if s.ConsecutiveErrors >= 3 && time.Since(s.LastFailure) < 1*time.Minute {
			return fmt.Errorf("server %s is in circuit-breaker cooldown", name)
		}
	}

	// Build environment: use IDE-provided env if available, else whitelist
	var cmdEnv []string
	if len(env) > 0 {
		for k, v := range env {
			cmdEnv = append(cmdEnv, k+"="+v)
		}
		// Also pass through MCP_ prefixed vars from host
		for _, e := range os.Environ() {
			parts := strings.SplitN(e, "=", 2)
			if strings.HasPrefix(parts[0], "MCP_") {
				cmdEnv = append(cmdEnv, e)
			}
		}
	} else {
		whitelist := map[string]bool{"PATH": true, "HOME": true, "USER": true, "LANG": true, "SHELL": true}
		for _, e := range os.Environ() {
			parts := strings.SplitN(e, "=", 2)
			if whitelist[parts[0]] || strings.HasPrefix(parts[0], "MCP_") {
				cmdEnv = append(cmdEnv, e)
			}
		}
	}

	cmd := exec.CommandContext(ctx, command, args...)
	cmd.Env = cmdEnv
	if filepath.IsAbs(command) {
		cmd.Dir = filepath.Dir(filepath.Dir(command))
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.markFailure(name)
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		m.markFailure(name)
		return err
	}

	if err := cmd.Start(); err != nil {
		m.markFailure(name)
		return err
	}

	transport := NewProcessTransport(stdin, stdout)
	client := mcp.NewClient(
		&mcp.Implementation{Name: "magictools-proxy", Version: "1.0.0"},
		&mcp.ClientOptions{},
	)

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		_ = cmd.Process.Kill()
		return err
	}

	m.mu.Lock()
	m.Servers[name] = &SubServer{
		Name:     name,
		Session:  session,
		Process:  cmd,
		LastUsed: time.Now(),
	}
	m.mu.Unlock()
	m.resetFailure(name)

	slog.Info("connected to sub-server", "name", name)
	return nil
}

func (m *Manager) markFailure(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.Servers[name]; ok {
		s.ConsecutiveErrors++
		s.LastFailure = time.Now()
	} else {
		m.Servers[name] = &SubServer{
			Name:              name,
			ConsecutiveErrors: 1,
			LastFailure:       time.Now(),
		}
	}
}

func (m *Manager) resetFailure(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.Servers[name]; ok {
		s.ConsecutiveErrors = 0
	}
}

// DisconnectServer stops a single sub-server and removes it from the pool
func (m *Manager) DisconnectServer(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.Servers[name]
	if !ok {
		return
	}
	if s.Session != nil {
		s.Session.Close()
	}
	if s.Process != nil && s.Process.Process != nil {
		_ = s.Process.Process.Kill()
	}
	delete(m.Servers, name)
	slog.Info("disconnected sub-server", "name", name)
}

// DisconnectAll stops all sub-servers
func (m *Manager) DisconnectAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for name, s := range m.Servers {
		if s.Session != nil {
			s.Session.Close()
		}
		if s.Process != nil && s.Process.Process != nil {
			_ = s.Process.Process.Kill()
		}
		delete(m.Servers, name)
		slog.Info("disconnected sub-server", "name", name)
	}
}

// CloseAll is an alias for DisconnectAll (backward compatibility)
func (m *Manager) CloseAll() {
	m.DisconnectAll()
}

// RunningCount returns the number of active sub-servers with live sessions
func (m *Manager) RunningCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	count := 0
	for _, s := range m.Servers {
		if s.Session != nil {
			count++
		}
	}
	return count
}

// EvictLRU kills the least recently used sub-server(s) to stay under the max limit.
// excludeName is the server that was just used (exempt from eviction).
func (m *Manager) EvictLRU(excludeName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Count active servers
	var active []*SubServer
	for _, s := range m.Servers {
		if s.Session != nil {
			active = append(active, s)
		}
	}

	if len(active) <= MaxRunningServers {
		return
	}

	// Sort by LastUsed ascending (oldest first)
	sort.Slice(active, func(i, j int) bool {
		return active[i].LastUsed.Before(active[j].LastUsed)
	})

	// Evict oldest until we're at the limit
	evictCount := len(active) - MaxRunningServers
	for _, s := range active {
		if evictCount <= 0 {
			break
		}
		if s.Name == excludeName {
			continue
		}
		if s.Session != nil {
			s.Session.Close()
		}
		if s.Process != nil && s.Process.Process != nil {
			_ = s.Process.Process.Kill()
		}
		slog.Info("LRU evicted sub-server", "name", s.Name)
		delete(m.Servers, s.Name)
		evictCount--
	}
}

// CallProxy executes a tool call on a sub-server and updates LRU tracking
func (m *Manager) CallProxy(ctx context.Context, serverName, toolName string, arguments map[string]interface{}) (*mcp.CallToolResult, error) {
	m.mu.RLock()
	s, ok := m.Servers[serverName]
	m.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("server %s not connected", serverName)
	}

	// Update LRU timestamp
	m.mu.Lock()
	s.LastUsed = time.Now()
	m.mu.Unlock()

	params := &mcp.CallToolParams{
		Name:      toolName,
		Arguments: arguments,
	}

	result, err := s.Session.CallTool(ctx, params)
	if err != nil {
		return nil, err
	}

	// Trigger LRU eviction after successful call
	m.EvictLRU(serverName)

	return result, nil
}
```

**Step 2: Verify build**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build ./internal/client/
```
Expected: Compiles cleanly.

**Step 3: Stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/internal/client/manager.go
```

---

### Task 5: Refactor Sync Engine (Ephemeral + Timeout + disabledTools)

**Files:**
- Rewrite: `internal/engine/sync.go`

**Step 1: Rewrite the sync engine**

Key changes:
- Uses `Config.ManagedServers` instead of `Config.Servers`
- `context.WithTimeout(ctx, 30*time.Second)` wraps each sub-server spawn
- After indexing, calls `Manager.DisconnectServer(name)` to kill the process
- Respects `disabledTools` filter: skips tools listed in `ServerConfig.DisabledTools`
- `SyncEcosystem` calls `Manager.DisconnectAll()` first (clean slate)
- Passes `ServerConfig.Env` to `Manager.Connect()`

```go
package engine

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"golang.org/x/sync/errgroup"

	"mcp-server-magictools/internal/client"
	"mcp-server-magictools/internal/config"
	"mcp-server-magictools/internal/db"
)

// Syncer handles ecosystem synchronization
type Syncer struct {
	Config  *config.Config
	Store   *db.Store
	Manager *client.Manager
}

// NewSyncer initializes the ecosystem sync engine
func NewSyncer(mgr *client.Manager, store *db.Store) *Syncer {
	return &Syncer{
		Manager: mgr,
		Store:   store,
	}
}

// SyncResult holds connection status for reporting
type SyncResult struct {
	TotalPotential int64
	Connected      []string
	Failed         []string
}

func (s *Syncer) SyncEcosystem(ctx context.Context) (*SyncResult, error) {
	// Clean slate: stop all running sub-servers before re-indexing
	s.Manager.DisconnectAll()

	// Re-read the config to get the latest managed server list
	if s.Config.ConfigPath != "" {
		freshCfg, err := s.Config.Reload()
		if err != nil {
			slog.Warn("sync: failed to reload config, using cached", "error", err)
		} else {
			s.Config.ManagedServers = freshCfg.ManagedServers
		}
	}

	g, gCtx := errgroup.WithContext(ctx)
	sem := make(chan struct{}, 10) // concurrency limiter
	var totalPotential int64

	result := &SyncResult{}
	var mu sync.Mutex

	for _, sc := range s.Config.ManagedServers {
		sc := sc // re-bind for closure
		g.Go(func() error {
			sem <- struct{}{}
			defer func() { <-sem }()

			// Timeout per sub-server: 30 seconds to connect + list tools
			timeoutCtx, cancel := context.WithTimeout(gCtx, 30*time.Second)
			defer cancel()

			// Connect with env passthrough from IDE config
			err := s.Manager.Connect(timeoutCtx, sc.Name, sc.Command, sc.Args, sc.Env)
			if err != nil {
				slog.Error("sync: failed to connect", "server", sc.Name, "error", err)
				mu.Lock()
				result.Failed = append(result.Failed, sc.Name)
				mu.Unlock()
				return nil
			}

			// List tools
			s.Manager.mu.RLock()
			srv, ok := s.Manager.Servers[sc.Name]
			s.Manager.mu.RUnlock()
			if !ok {
				mu.Lock()
				result.Failed = append(result.Failed, sc.Name)
				mu.Unlock()
				return nil
			}

			tools, err := srv.Session.ListTools(timeoutCtx, nil)
			if err != nil {
				slog.Error("sync: failed to list_tools", "server", sc.Name, "error", err)
				mu.Lock()
				result.Failed = append(result.Failed, sc.Name)
				mu.Unlock()
				s.Manager.DisconnectServer(sc.Name)
				return nil
			}

			// Build disabledTools lookup
			disabled := make(map[string]bool, len(sc.DisabledTools))
			for _, t := range sc.DisabledTools {
				disabled[t] = true
			}

			// Index each tool
			for _, t := range tools.Tools {
				if disabled[t.Name] {
					continue // skip tools disabled in IDE config
				}

				h := hashSchema(t)
				schemaJSON, _ := json.Marshal(t)
				atomic.AddInt64(&totalPotential, int64(len(schemaJSON)/4))

				record := &db.ToolRecord{
					URN:          fmt.Sprintf("%s:%s", sc.Name, t.Name),
					Name:         t.Name,
					Server:       sc.Name,
					Description:  t.Description,
					InputSchema:  toSchemaMap(t.InputSchema),
					LiteSummary:  t.Description,
					SchemaHash:   h,
					LastSyncedAt: time.Now().Unix(),
					Category:     deriveCategory(sc.Name),
				}

				if err := s.Store.SaveTool(record); err != nil {
					slog.Error("sync: failed to save tool", "urn", record.URN, "error", err)
				}
			}

			// Ephemeral: kill the sub-server after indexing
			s.Manager.DisconnectServer(sc.Name)

			mu.Lock()
			result.Connected = append(result.Connected, sc.Name)
			mu.Unlock()
			slog.Info("synced sub-server", "name", sc.Name, "tools", len(tools.Tools))
			return nil
		})
	}

	_ = g.Wait()
	result.TotalPotential = totalPotential
	return result, nil
}

func hashSchema(t *mcp.Tool) string {
	data, _ := json.Marshal(t)
	return fmt.Sprintf("%x", sha256.Sum256(data))
}

func deriveCategory(server string) string {
	switch server {
	case "recall", "brainstorm":
		return "personal"
	case "go-refactor", "magicskills":
		return "code"
	case "duckduckgo", "ddg-search":
		return "web"
	case "filesystem":
		return "system"
	case "git", "github", "glab":
		return "vcs"
	case "sequential-thinking":
		return "reasoning"
	default:
		return "general"
	}
}

func toSchemaMap(schema any) map[string]interface{} {
	if schema == nil {
		return nil
	}
	if m, ok := schema.(map[string]interface{}); ok {
		return m
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}
```

> **Note:** The `s.Manager.mu.RLock()` direct access is a design smell. In the real implementation, consider adding a `Manager.GetSession(name)` method instead. For now, this matches the existing pattern. If the `mu` field is unexported, add a `GetServer(name) (*SubServer, bool)` method to the manager.

**Step 2: Fix the field access issue**

The `Manager.mu` field is unexported. We need to add a `GetServer` accessor to the manager. Add this to `internal/client/manager.go`:

```go
// GetServer returns a sub-server by name (thread-safe)
func (m *Manager) GetServer(name string) (*SubServer, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.Servers[name]
	return s, ok
}
```

Then update the sync engine to use `s.Manager.GetServer(sc.Name)` instead of the direct lock access.

**Step 3: Verify build**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build ./internal/...
```
Expected: `internal/config/`, `internal/client/`, `internal/engine/` all compile. `main.go` and `handler/` may not compile yet.

**Step 4: Stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/internal/engine/sync.go scripts/go/mcp-server-magictools/internal/client/manager.go
```

---

### Task 6: Add DB Method for Server Tool Purging

**Files:**
- Modify: `internal/db/badger.go`

**Step 1: Add `PurgeServerTools` method**

This scans all tool records and deletes those belonging to a specific server. Used by the config watcher when a server transitions from disabled→enabled.

Add to `internal/db/badger.go`:

```go
// PurgeServerTools removes all tool records for a specific server
func (s *Store) PurgeServerTools(serverName string) error {
	return s.DB.Update(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()

		prefix := []byte("tool:")
		var toDelete [][]byte

		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := item.KeyCopy(nil)

			err := item.Value(func(val []byte) error {
				var r ToolRecord
				if err := json.Unmarshal(val, &r); err != nil {
					return nil // skip corrupt records
				}
				if r.Server == serverName {
					toDelete = append(toDelete, key)
					// Also queue category index key for deletion
					catKey := []byte("cat:" + r.Category + ":" + r.URN)
					toDelete = append(toDelete, catKey)
				}
				return nil
			})
			if err != nil {
				return err
			}
		}

		for _, key := range toDelete {
			if err := txn.Delete(key); err != nil {
				slog.Warn("failed to delete key during purge", "key", string(key), "error", err)
			}
		}

		slog.Info("purged tools for server", "server", serverName, "count", len(toDelete)/2)
		return nil
	})
}
```

**Step 2: Verify build**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build ./internal/db/
```
Expected: Compiles cleanly.

**Step 3: Stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/internal/db/badger.go
```

---

### Task 7: Add Cross-Platform Signal Files

**Files:**
- Create: `signals_unix.go`
- Create: `signals_windows.go`

**Step 1: Write `signals_unix.go`**

```go
//go:build !windows

package main

import (
	"os"
	"syscall"
)

var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}
```

**Step 2: Write `signals_windows.go`**

```go
//go:build windows

package main

import "os"

var shutdownSignals = []os.Signal{os.Interrupt}
```

**Step 3: Verify build (Linux)**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build -v .
```
Expected: Uses `signals_unix.go`, compiles cleanly. Note: `main.go` won't compile yet until Task 8.

**Step 4: Stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/signals_unix.go scripts/go/mcp-server-magictools/signals_windows.go
```

---

### Task 8: Refactor Handlers (Proxy Config Awareness + Cleanup Wiring)

**Files:**
- Modify: `internal/handler/handlers.go`

**Step 1: Update the handler**

Key changes:
- `OrchestratorHandler` holds a reference to `*config.Config` for proxy lazy-connect
- `call_proxy`: looks up server config → lazy-connect with env passthrough if not running → call
- `sync_ecosystem`: behavior stays (calls Syncer), but now the syncer handles clean slate internally
- `unload_tools`: additionally calls `Manager.DisconnectAll()` for process cleanup
- Add `OnServerPromoted` / `OnServerDemoted` methods (implements `config.ConfigChangeHandler`)

The full replacement for `handlers.go` is complex. The key diffs are (apply to existing file):

In the struct definition, add Config:
```go
type OrchestratorHandler struct {
	Store   *db.Store
	Syncer  *engine.Syncer
	Manager *client.Manager
	Config  *config.Config
	Stats   *SessionStats
}
```

In `NewHandler`, add config parameter:
```go
func NewHandler(store *db.Store, syncer *engine.Syncer, manager *client.Manager, cfg *config.Config) *OrchestratorHandler {
	return &OrchestratorHandler{
		Store:   store,
		Syncer:  syncer,
		Manager: manager,
		Config:  cfg,
		Stats:   &SessionStats{},
	}
}
```

In the `call_proxy` handler, before the existing `CallProxy` call, add lazy-connect logic:
```go
// Lazy-connect: if server is not running, spawn it from config
if _, ok := h.Manager.GetServer(server); !ok {
	// Find server config
	for _, sc := range h.Config.ManagedServers {
		if sc.Name == server {
			if err := h.Manager.Connect(ctx, sc.Name, sc.Command, sc.Args, sc.Env); err != nil {
				return nil, fmt.Errorf("failed to lazy-connect server %s: %w", server, err)
			}
			break
		}
	}
}
```

In the `unload_tools` handler, add process cleanup:
```go
h.Manager.DisconnectAll()
h.Stats.ActuallyLoaded = 0
h.Stats.ProxyCalls = 0
```

Add ConfigChangeHandler implementation:
```go
// OnServerPromoted handles a server transitioning from magictools-managed to IDE-managed
func (h *OrchestratorHandler) OnServerPromoted(name string) {
	h.Manager.DisconnectServer(name)
	_ = h.Store.PurgeServerTools(name)
}

// OnServerDemoted handles a server transitioning from IDE-managed to magictools-managed
func (h *OrchestratorHandler) OnServerDemoted(name string) {
	// No-op: will be indexed on next sync_ecosystem call
	slog.Info("server available for magictools management", "server", name)
}
```

**Step 2: Verify build**

At this point, `main.go` still uses the old API. Don't compile the full project yet. Compile just the handler:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build ./internal/handler/
```
Expected: May not compile if import for `config` and `slog` aren't resolved. That's fine — will fix with main.go.

**Step 3: Stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/internal/handler/handlers.go
```

---

### Task 9: Refactor main.go (Wire Everything Together)

**Files:**
- Rewrite: `main.go`

**Step 1: Rewrite main.go**

Key changes:
- Use `shutdownSignals` from build-tagged files instead of inline `syscall.SIGHUP`
- Use `config.New(Version, configPath)` new signature
- Wire the config watcher goroutine
- Remove the `Monitor()` goroutine
- Handler uses new `NewHandler(store, syncer, mgr, cfg)` signature
- `CloseAll()` in defer stays (aliased to `DisconnectAll()`)

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"mcp-server-magictools/internal/client"
	"mcp-server-magictools/internal/config"
	"mcp-server-magictools/internal/db"
	"mcp-server-magictools/internal/engine"
	"mcp-server-magictools/internal/handler"
)

func main() {
	var (
		configPath string
		dbPath     string
		vFlag      bool
	)

	flag.StringVar(&configPath, "config", "", "Path to IDE mcp_config.json (optional, uses MCP_MAGIC_TOOLS_CONFIG or default)")
	flag.StringVar(&dbPath, "db", os.Getenv("HOME")+"/.mcp_magictools", "Path to BadgerDB")
	flag.BoolVar(&vFlag, "version", false, "Print version and exit")
	flag.Parse()

	if vFlag {
		fmt.Printf("mcp-server-magictools %s\n", Version)
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	ctx, stop := signal.NotifyContext(ctx, shutdownSignals...)
	defer stop()

	// Start the parent process watchdog
	go watchdog(ctx, cancel)

	// Initialize Manager (no persistent Monitor needed)
	mgr := client.NewManager()
	defer func() {
		slog.Info("shutting down: cleaning up sub-processes")
		mgr.CloseAll()
	}()

	// Initialize Storage
	store, err := db.NewStore(dbPath)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	// Initialize Config (reads IDE mcp_config.json)
	cfg, err := config.New(Version, configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	// Initialize Syncer
	syncer := engine.NewSyncer(mgr, store)
	syncer.Config = cfg

	// Initialize Handlers
	h := handler.NewHandler(store, syncer, mgr, cfg)

	// Start Config Watcher
	watcher := config.NewWatcher(cfg.ConfigPath, cfg.GetManagedServerNames(), h)
	go func() {
		if err := watcher.Start(); err != nil {
			slog.Error("config watcher failed", "error", err)
		}
	}()
	defer watcher.Stop()

	// Startup Server
	s := mcp.NewServer(
		&mcp.Implementation{
			Name:    "mcp-server-magictools",
			Version: Version,
		},
		&mcp.ServerOptions{
			Logger: slog.Default(),
		},
	)

	h.Register(s)

	slog.Info("MagicTools Orchestrator starting",
		"version", Version,
		"db", dbPath,
		"config", cfg.ConfigPath,
		"managed_servers", len(cfg.ManagedServers),
	)

	if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func watchdog(ctx context.Context, cancel context.CancelFunc) {
	initialPPID := os.Getppid()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			currentPPID := os.Getppid()
			if currentPPID != initialPPID {
				slog.Info("Parent process died or changed; initiating shutdown",
					"initial_ppid", initialPPID, "current_ppid", currentPPID)
				cancel()
				return
			}
		}
	}
}
```

**Step 2: Fix the cross-platform `badger.go` stale process cleanup**

The `cleanupStaleProcess` function in `internal/db/badger.go` uses `syscall.Signal(0)` and `syscall.SIGTERM` which are Linux-specific. For now, wrap it with a build tag or make it a no-op on Windows. Create `internal/db/cleanup_unix.go` and `internal/db/cleanup_windows.go` and move the `cleanupStaleProcess` function there. **This is a stretch goal** — skip if time-constrained, since the current target is Linux.

**Step 3: Full project build**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build -v .
```
Expected: Clean compile. Binary produced.

**Step 4: Stage all remaining changes**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/
```

---

### Task 10: Smoke Test

**Step 1: Build the binary**

Run:
```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools && go build -o dist/mcp-server-magictools-linux-amd64 .
```

**Step 2: Test version flag**

Run:
```bash
/home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools/dist/mcp-server-magictools-linux-amd64 --version
```
Expected: Prints version string.

**Step 3: Test config loading**

Run:
```bash
echo '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1.0"}}}' | /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools/dist/mcp-server-magictools-linux-amd64 --config /home/adm_saxsmith/.gemini/antigravity/mcp_config.json 2>/dev/null | head -1
```
Expected: JSON-RPC response with server capabilities. Check stderr logs for "managed_servers" count.

**Step 4: Deploy to .local/bin**

Run:
```bash
cp /home/adm_saxsmith/gitrepos/saxsmith-global-context/scripts/go/mcp-server-magictools/dist/mcp-server-magictools-linux-amd64 /home/adm_saxsmith/.local/bin/mcp-server-magictools
```

**Step 5: Final stage**

```bash
cd /home/adm_saxsmith/gitrepos/saxsmith-global-context && /home/adm_saxsmith/.local/bin/git-wrapper.sh add scripts/go/mcp-server-magictools/
```
