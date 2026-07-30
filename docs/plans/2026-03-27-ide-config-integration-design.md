# MagicTools IDE Config Integration Design

**Date**: 2026-03-27
**Status**: Approved
**Goal**: Eliminate the duplicate `meta-servers.json` config by reading the IDE's `mcp_config.json` directly, adding runtime config watching, ephemeral sub-server lifecycle management, LRU process eviction, and cross-platform compatibility.

---

## 1. Inverted Ownership Model

The IDE config (`mcp_config.json`) is the single source of truth. Servers are categorized by their `disabled` field:

| IDE `disabled` | Owner | magictools behavior |
|---|---|---|
| `false` (enabled) | IDE | **Ignore** — IDE manages the process and exposes tools directly |
| `true` (disabled) | magictools | **Manage** — ephemeral spawn, index tools, LRU-managed proxy |

magictools skips itself (`magictools` entry) during parsing regardless of disabled state.

## 2. Config Layer

### IDE Config Format

```json
{
  "mcpServers": {
    "server-name": {
      "command": "/path/to/binary",
      "args": ["--flag"],
      "env": {"HOME": "/home/user", "PATH": "..."},
      "disabled": true,
      "disabledTools": ["tool_a", "tool_b"]
    }
  }
}
```

### Config Path Discovery

Priority order:
1. `--config` CLI flag
2. `MCP_MAGIC_TOOLS_CONFIG` environment variable
3. Default: `~/.gemini/antigravity/mcp_config.json`

### Parsed Structs

```go
type IDEConfig struct {
    McpServers map[string]IDEServerEntry `json:"mcpServers"`
}

type IDEServerEntry struct {
    Command       string            `json:"command"`
    Args          []string          `json:"args"`
    Env           map[string]string `json:"env"`
    Disabled      bool              `json:"disabled"`
    DisabledTools []string          `json:"disabledTools,omitempty"`
}
```

### GetManagedServers()

Returns `[]ServerConfig` containing only entries where `disabled: true`, excluding the `magictools` entry itself. Each `ServerConfig` carries the env map from the IDE config for passthrough to sub-processes.

## 3. File Watcher

Uses `github.com/fsnotify/fsnotify` (cross-platform: Linux inotify, macOS kqueue, Windows ReadDirectoryChangesW).

### Behavior

- Watches the resolved config file path
- Debounce: 200ms (handles rapid editor saves)
- On change: re-parse config, diff old managed set vs new managed set

### State Transitions

| Transition | Meaning | Action |
|---|---|---|
| `disabled:true` -> `disabled:false` | User enabled server in IDE | **Purge** server's tools from BadgerDB, disconnect if running |
| `disabled:false` -> `disabled:true` | User disabled server in IDE | **No-op** — available for next `sync_ecosystem` |
| Server removed from config | Server uninstalled | **Purge** server's tools from BadgerDB, disconnect if running |
| New server added with `disabled:true` | New managed server | Available for next `sync_ecosystem` |

### Callback Interface

```go
type ConfigChangeHandler interface {
    OnServerPromoted(name string)   // disabled->enabled: purge from index
    OnServerDemoted(name string)    // enabled->disabled: available for sync
}
```

## 4. Sync Lifecycle (Ephemeral)

When `sync_ecosystem` is called:

1. **Stop all running sub-servers** (clean slate)
2. Parse current config -> get managed servers (`disabled: true`)
3. For each (parallel, semaphore=10):
   a. `context.WithTimeout(ctx, 30*time.Second)` — reaps hanging processes
   b. Spawn sub-server via `os/exec` with env passthrough from IDE config
   c. MCP handshake -> `ListTools`
   d. Index tools to BadgerDB (respecting `disabledTools` filter)
   e. **Close connection + kill process immediately**
4. Return sync results

### disabledTools Support

If the IDE config specifies `disabledTools` for a managed server, those specific tools are excluded from the index. This allows fine-grained control without enabling the full server in the IDE.

## 5. Proxy Lifecycle (Lazy + LRU)

When `call_proxy` is called:

1. Check if target server is running -> reuse if yes
2. If not running -> lazy-spawn with env from IDE config
3. Execute the tool call
4. Record `LastUsed` timestamp on the sub-server
5. **LRU eviction check**: if running server count > 10, kill the least recently used server(s) until count <= 10

### LRU Implementation

```go
type SubServer struct {
    Name     string
    Session  *mcp.ClientSession
    Process  *exec.Cmd
    LastUsed time.Time  // Updated on every proxy call
    // ... circuit breaker fields
}
```

Eviction is checked after every successful proxy call. The eviction target excludes the server that was just used.

## 6. Session Lifecycle

### On `sync_ecosystem`

- Stop all running sub-servers (clean slate for resource management)
- Perform ephemeral index scan
- All processes killed after indexing

### On `unload_tools` (Janitor)

- Stop all running sub-servers (full process cleanup)
- Clear session stats
- Return context hygiene message

### On shutdown

- `CloseAll()` kills all running sub-server processes

## 7. Cross-Platform Compatibility

### Issues and Fixes

| Component | Issue | Fix |
|---|---|---|
| `os.Getppid()` watchdog | Unreliable on Windows | Build-tag: `watchdog_unix.go` (PPID check) + `watchdog_windows.go` (no-op) |
| `syscall.SIGHUP` | Doesn't exist on Windows | Build-tag signal lists: `signals_unix.go` / `signals_windows.go` |
| `cmd.Process.Kill()` | Already cross-platform | No change (SIGKILL on Unix, TerminateProcess on Windows) |
| `exec.CommandContext` | Already cross-platform | No change |
| File watching | N/A (new) | `fsnotify/fsnotify` handles all platforms |
| `filepath` operations | Already cross-platform | No change |
| `os.Environ()` | Already cross-platform | No change |

### Signal Handling (Build Tags)

```go
// signals_unix.go (//go:build !windows)
var shutdownSignals = []os.Signal{os.Interrupt, syscall.SIGTERM, syscall.SIGHUP}

// signals_windows.go (//go:build windows)
var shutdownSignals = []os.Signal{os.Interrupt}
```

## 8. File Inventory

| Action | File | Description |
|---|---|---|
| **Rewrite** | `internal/config/config.go` | IDE config parser, managed server extraction |
| **New** | `internal/config/watcher.go` | fsnotify watcher with debounce and diff |
| **Delete** | `internal/config/meta-servers.json` | No longer needed |
| **Modify** | `internal/engine/sync.go` | Ephemeral spawn + timeout + kill after index |
| **Modify** | `internal/client/manager.go` | LRU eviction, `DisconnectServer()`, env passthrough, remove `Monitor()` |
| **Modify** | `internal/handler/handlers.go` | Wire cleanup into sync/janitor, pass config to proxy |
| **Modify** | `main.go` | Wire watcher, update config init, build-tag signals |
| **New** | `signals_unix.go` | Unix signal list (package main) |
| **New** | `signals_windows.go` | Windows signal list (package main) |
| **Modify** | `go.mod` | Add `github.com/fsnotify/fsnotify` |

## 9. Dependencies

### New
- `github.com/fsnotify/fsnotify` — cross-platform file system notifications

### Existing (unchanged)
- `github.com/modelcontextprotocol/go-sdk` — MCP protocol
- `github.com/dgraph-io/badger/v4` — tool index storage
- `golang.org/x/sync/errgroup` — parallel sync
