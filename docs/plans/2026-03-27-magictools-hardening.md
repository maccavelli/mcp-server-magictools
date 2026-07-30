# MagicTools Hardening & Optimization Plan

> **For Antigravity:** REQUIRED WORKFLOW: Use `.agent/workflows/execute-plan.md` to execute this plan in single-flow mode.

**Goal:** Improve stability, performance, and robustness of the `mcp-server-magictools` orchestrator by fixing stale process handling, optimizing synchronization logic, and adhering to Go 1.26.1+ standards.

**Architecture:** 
1. **Stability**: Implement process grouping (`Setpgid`) and recursive killing to prevent orphan sub-processes.
2. **Performance**: Replace full `DisconnectAll` in `SyncEcosystem` with incremental indexing based on config diffs.
3. **Robustness**: Implement a debounced watcher for `mcp_config.json` and a more resilient startup sequence.
4. **Maintenance**: Refactor `internal/handler/handlers.go` into domain-specific files (tools, resources, sync).

**Tech Stack:** Go 1.26.1, slog, BadgerDB v4, errgroup, mcp-sdk-go.

---

### Task 1: Process Group Management
Prevent orphan sub-processes when sub-servers (e.g. shells) are terminated.

**Files:**
- Modify: `internal/client/manager.go`

**Step 1: Update Connect to set PGID**
```go
// In Connect()
cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
```
*Note: Ensure syscall is imported with build constraints for Unix.*

**Step 2: Update DisconnectServer to kill process group**
```go
// In DisconnectServer()
if s.Process != nil && s.Process.Process != nil {
    // Kill the negative PID (entire process group)
    _ = syscall.Kill(-s.Process.Process.Pid, syscall.SIGKILL)
}
```

**Step 3: Verification**
Run `go test -v ./internal/client/...` and verify no zombie processes are left via `ps aux | grep mcp-server-magictools`.

---

### Task 2: Incremental Ecosystem Synchronization
Reduce latency and prevent session drops during re-indexing.

**Files:**
- Modify: `internal/engine/sync.go`

**Step 1: Remove DisconnectAll from SyncEcosystem**
Current logic calls `s.Manager.DisconnectAll()` at start. We should change this to only disconnect servers if they have changed.

**Step 2: Implement Config Hash Matching**
Store the `sc.Command + sc.Args + sc.Env` hash in `SubServer`. During `SyncEcosystem`, if a server is already alive and its config hash matches, skip re-indexing or only refresh the tool registry if it was forced.

---

### Task 3: Config Watcher Debouncing
Prevent rapid-fire re-syncs when the IDE or other tools modify `mcp_config.json`.

**Files:**
- Create: `internal/config/watcher.go`
- Modify: `main.go`

**Step 1: Implement Debounced Watcher**
Use a `time.AfterFunc` to debounce `fsnotify` events for 500ms before calling the actual reload logic.

---

### Task 4: Handler Layer Modularization
Split the large `handlers.go` file for better maintainability.

**Files:**
- Create: `internal/handler/sync_handlers.go`
- Create: `internal/handler/proxy_handlers.go`
- Modify: `internal/handler/handlers.go` (to only contain Registration logic)

---

### Task 5: Go 1.26.1+ Modernization
Use newer patterns for better performance and readability.

**Files:**
- Modify: `internal/handler/handlers.go` (use slog.Group for sub-server logs)
- Modify: `internal/engine/sync.go` (use iterators for maps where applicable)

---
