# Session Stats Expansion Implementation Plan

> **For Antigravity:** REQUIRED WORKFLOW: Use `.agent/workflows/execute-plan.md` to execute this plan in single-flow mode.

**Goal:** Expand the `get_session_stats` internal tooling to monitor proxy performance (bytes optimized, ping telemetry, connection tracking) segmented on a per-subserver basis.

**Architecture:** Create a new `internal/telemetry` package to house a concurrent-safe tracking engine leveraging `sync.Map` and `atomic.Int64`. Replace the simplistic `SessionStats` struct in `OrchestratorHandler` with this designated registry and update the `get_session_stats` diagnostic tool to render it as JSON.

**Tech Stack:** Go `sync`, `sync/atomic`

---

### Task 1: Create the Telemetry Package

**Files:**
- Create: `internal/telemetry/telemetry.go`
- Create: `internal/telemetry/telemetry_test.go`

**Step 1: Write the failing test**

```go
// internal/telemetry/telemetry_test.go
package telemetry

import (
	"testing"
)

func TestMetrics_Record(t *testing.T) {
	tm := NewTracker()
	tm.AddLatency("devops", 500)
	tm.AddBytes("devops", 1000, 50)
	tm.RecordFault("devops")
	
	m := tm.GetMetrics("devops")
	if m == nil {
		t.Fatal("expected metrics")
	}
	if m.Calls != 1 || m.TotalSpinupMs != 500 || m.BytesRaw != 1000 || m.BytesMinified != 50 || m.Faults != 1 {
		t.Errorf("metrics did not record properly: %+v", m)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/telemetry -v`
Expected: FAIL due to missing package or missing methods.

**Step 3: Write minimal implementation**

```go
// internal/telemetry/telemetry.go
package telemetry

import (
	"sync"
	"sync/atomic"
)

type ServerMetrics struct {
	Calls          int64 `json:"calls"`
	TotalSpinupMs  int64 `json:"total_spinup_ms"`
	BytesRaw       int64 `json:"bytes_raw"`
	BytesMinified  int64 `json:"bytes_minified"`
	Faults         int64 `json:"faults"`
}

type Tracker struct {
	servers sync.Map // maps string (serverName) -> *serverNode
}

type serverNode struct {
	calls         atomic.Int64
	totalSpinupMs atomic.Int64
	bytesRaw      atomic.Int64
	bytesMin      atomic.Int64
	faults        atomic.Int64
}

func NewTracker() *Tracker {
	return &Tracker{}
}

func (t *Tracker) getOrAdd(server string) *serverNode {
	node, _ := t.servers.LoadOrStore(server, &serverNode{})
	return node.(*serverNode)
}

func (t *Tracker) AddLatency(server string, ms int64) {
	node := t.getOrAdd(server)
	node.calls.Add(1)
	node.totalSpinupMs.Add(ms)
}

func (t *Tracker) AddBytes(server string, raw, minified int64) {
	node := t.getOrAdd(server)
	node.bytesRaw.Add(raw)
	node.bytesMin.Add(minified)
}

func (t *Tracker) RecordFault(server string) {
	node := t.getOrAdd(server)
	node.faults.Add(1)
}

func (t *Tracker) GetMetrics(server string) *ServerMetrics {
	v, ok := t.servers.Load(server)
	if !ok {
		return nil
	}
	node := v.(*serverNode)
	return &ServerMetrics{
		Calls:         node.calls.Load(),
		TotalSpinupMs: node.totalSpinupMs.Load(),
		BytesRaw:      node.bytesRaw.Load(),
		BytesMinified: node.bytesMin.Load(),
		Faults:        node.faults.Load(),
	}
}

func (t *Tracker) GetAll() map[string]*ServerMetrics {
	res := make(map[string]*ServerMetrics)
	t.servers.Range(func(key, value any) bool {
		node := value.(*serverNode)
		res[key.(string)] = &ServerMetrics{
			Calls:         node.calls.Load(),
			TotalSpinupMs: node.totalSpinupMs.Load(),
			BytesRaw:      node.bytesRaw.Load(),
			BytesMinified: node.bytesMin.Load(),
			Faults:        node.faults.Load(),
		}
		return true
	})
	return res
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/telemetry -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/telemetry/telemetry.go internal/telemetry/telemetry_test.go
GIT_EDITOR=true git commit
```

### Task 2: Inject Telemetry into Handlers

**Files:**
- Modify: `internal/handler/handlers.go:31`

**Step 1: Write the failing test**

*(No specific unit test setup required as this is purely struct bridging; verification happens via compilation Check)*

**Step 2: Run test to verify it fails**

Run: `go build ./...`
Expected: FAIL because we are breaking existing `h.Stats.ProxyCalls` usages.

**Step 3: Write minimal implementation**

```go
// In internal/handler/handlers.go

import "mcp-server-magictools/internal/telemetry"

// Replace the SessionStats usage in OrchestratorHandler:
type OrchestratorHandler struct {
    // ...
	Telemetry     *telemetry.Tracker
    // ...
}

func NewHandler(store *db.Store, registry *client.WarmRegistry, cfg *config.Config) *OrchestratorHandler {
	h := &OrchestratorHandler{
        // ...
		Telemetry:     telemetry.NewTracker(),
        // ...
	}
}

// REMOVE the definition of SessionStats completely. 
```

**Step 4: Run test to verify it passes**

Run: `go build ./...`
Expected: FAIL due to `get_session_stats` using the old properties natively. (Handled in next step)

**Step 5: No Commit yet**

### Task 3: Update `get_session_stats` Diagnostics

**Files:**
- Modify: `internal/handler/diagnostic_handlers.go`

**Step 1: Write the failing test**

*(No formal unit testing required for rendering endpoints without massive mocking. Validated by standard build resolution).*

**Step 2: Run test to verify it fails**

*(Carried over from Task 2)*

**Step 3: Write minimal implementation**

Modify `get_session_stats` execution in `internal/handler/diagnostic_handlers.go`:

```go
	h.addTool(s, &mcp.Tool{Name: "get_session_stats"}, func(ctx context.Context, req *mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		
        stats := h.Telemetry.GetAll()
        data, err := json.MarshalIndent(stats, "", "  ")
        if err != nil {
            return nil, fmt.Errorf("failed to marshal telemetry: %w", err)
        }

		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(data)}}}, nil
	})
```

**Step 4: Run test to verify it passes**

Run: `go build ./...`
Expected: PASS (if all old `h.Stats` usages are purged). Note: Fix `h.Stats.ActuallyLoaded` references in `get_tool_definition` to use telemetry bytes optionally, or remove them.

**Step 5: Commit**

```bash
git add internal/handler/handlers.go internal/handler/diagnostic_handlers.go
GIT_EDITOR=true git commit
```

### Task 4: Hook into `call_proxy` Execution

**Files:**
- Modify: `internal/handler/proxy_handlers.go`

**Step 1: Write the failing test**

*(This task bridges operational telemetry. Verification via successful compilation and functionality check).*

**Step 2: Run test to verify it fails**

*(Skipping testing for inline modification)*

**Step 3: Write minimal implementation**

In `internal/handler/proxy_handlers.go` around line `340` (Lazy Activation Strategy):

```go
        // Around the JIT boot sequence
        startBoot := time.Now()
		if srv, ok := h.Registry.GetServer(server); !ok || srv.Session == nil {
			for _, sc := range h.Config.GetManagedServers() {
				if sc.Name == server {
					if err := h.Registry.Connect(ctx, sc.Name, sc.Command, sc.Args, sc.Env, sc.Hash()); err != nil {
						slog.Error("gateway: lazy activation failed", "server", sc.Name, "error", err)
                        h.Telemetry.RecordFault(server)
					}
					break
				}
			}
		}
        h.Telemetry.AddLatency(server, time.Since(startBoot).Milliseconds())
```

And around line `400` (Data Hardening Layer / Squeezing):

```go
            // Replace line: h.Stats.ProxyCalls.Add(1) with:
            // (Removed inside the handler loop, letting AddLatency track calls implicitly)
			
            // Inside the bypass Minification conditional:
			rawBytes, _ := json.Marshal(res.StructuredContent)
            preLen := int64(len(rawBytes))
            
            // ... (squeezing and truncation block) ...

			minifiedData, _ := json.MarshalIndent(res.StructuredContent, "", "  ")
			md := transformToHybrid(minifiedData, h.Config.MaxResponseTokens)
			
            postLen := int64(len(md))
            h.Telemetry.AddBytes(server, preLen, postLen)
```

**Step 4: Run test to verify it passes**

Run: `go test ./...`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/handler/proxy_handlers.go
GIT_EDITOR=true git commit
```
