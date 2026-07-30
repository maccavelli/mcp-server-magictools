# Design: MagicTools Orchestrator Communication & Handshake Fix

## Problem Statement

The MagicTools orchestrator is currently failing handshakes (returning `EOF` during `initialize`) for multiple sub-servers (e.g., `brainstorm`, `filesystem`, `ddg-search`). This is primarily caused by two recent regressions:
1.  **Premature Stdin Closure**: Standard input of sub-servers was being closed *before* the data bridge (copying information from the orchestrator) could actually start.
2.  **Brittle JSON Filtering**: The `jsonFilterReader` was discarding valid MCP messages that didn't contain the literal substring `"jsonrpc"`, causing valid responses and notifications to be lost.

## Proposed Architecture

### 1. Handshake & IO Stabilization
We will update the `Manager.setupIOBridges` logic in `internal/client/manager.go` to correctly manage the lifecycle of sub-server pipes.

- **Non-blocking Stdin**: Move `realStdin.Close()` to a `defer` in the goroutine that performs the data copy. This ensures the pipe remains open for the full transaction.
- **Improved Error Visibility**: Upgrade `slog.Debug` to `slog.Error` for critical pipe interruptions, as these are almost always fatal to the session.

### 2. Robust MCP Message Detection
The `jsonFilterReader` in `internal/client/filter.go` will be refined to correctly identify and pass through all valid MCP messages without compromising the ability to filter noise (logs).

- **Heuristic Upgrade**: Replace strict `"jsonrpc"` string matching with a set of mandatory MCP/JSON-RPC keywords:
    - `"jsonrpc"`
    - `"method"`
    - `"result"`
    - `"error"`
- **Balanced Object Guarantee**: Maintain existing brace/bracket depth tracking to ensure only complete objects are analyzed and passed to the primary JSON-RPC decoder.

### 3. Efficiency & Noise Handling
- **Character-set Optimization**: Replace `j.logSink.Write([]byte{c})` with buffered writes or broader shunting logic to reduce system call overhead when sub-servers output large volumes of logs during initialization.

## Success Criteria

1.  **10/10 Connectivity**: All 10 servers in the standard ecosystem must successfully synchronize (`SyncEcosystem`).
2.  **No `EOF` handshakes**:Handshake failures during initialization must be eliminated for all supported runtimes (Go, Node.js, Python).
3.  **Clean Logs**: Sub-server logs must still be correctly shunted to `/tmp/mcp-magic-v3.log` without polluting the JSON-RPC streams.

## Testing Plan

1.  **Unit Tests**: Update `filter_test.go` (if exists) or create it to verify that objects with valid headers are passed, and objects without are shunted.
2.  **Integration Test**: Perform a full `magictools:sync_ecosystem` and verify that `brainstorm` and `ddg-search` are online.
3.  **Noise Test**: Verify that a sub-server sending non-JSON text intermingled with JSON (like `sequential-thinking`) still connects correctly.
