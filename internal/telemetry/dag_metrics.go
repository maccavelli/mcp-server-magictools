package telemetry

import (
	"sync"
	"time"
)

// DAGTracker manages real-time observability of orchestrator DAG states.
type DAGTracker struct {
	mu sync.RWMutex

	sessionID  string
	status     string
	startTime  time.Time
	entropy    float64
	totalEdges int64
	treeDepth  int64

	currentNode   int64
	mutationDepth int64
	nodes         []DAGNode
	activeNode    DAGActiveNode
}

type DAGNode struct {
	Name      string
	State     string
	StartTime time.Time
	LatencyMs int64
}

type DAGActiveNode struct {
	Name             string
	BytesRaw         int64
	BytesMinified    int64
	Tokens           int64
	CacheAction      string
	CSSAHash         string
	Faults           int64
	RollbackStrategy string
	RetryCount       int64
	RetryLimit       int64
	FallbackURN      string
}

// GlobalDAGTracker provides centralized pipeline tracking bounds safely.
var GlobalDAGTracker = &DAGTracker{
	status: dagStatusWaiting,
}

// InitializePipeline resets the tracker for a new compose_pipeline run.
func (t *DAGTracker) InitializePipeline(sessionID string, nodeNames []string, entropy float64, edges, depth int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.sessionID = sessionID
	t.status = dagStatusExecuting
	t.startTime = time.Now()
	t.entropy = entropy
	t.totalEdges = edges
	t.treeDepth = depth
	t.currentNode = 0

	t.nodes = make([]DAGNode, len(nodeNames))
	for i, n := range nodeNames {
		t.nodes[i] = DAGNode{
			Name:  n,
			State: dagStatusWaiting,
		}
	}
	t.activeNode = DAGActiveNode{}
	t.mutationDepth = 0
}

// IncrementMutationDepth safely increments and returns the TTL bound for topological evolution.
func (t *DAGTracker) IncrementMutationDepth() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.mutationDepth++
	return t.mutationDepth
}

// SpliceNodes securely injects new nodes directly after the active URN, gracefully tail-appending on miss.
func (t *DAGTracker) SpliceNodes(activeUrn string, newNodes []string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	idx := -1
	for i := range t.nodes {
		if t.nodes[i].Name == activeUrn {
			idx = i
			break
		}
	}

	// Robustness: Graceful fallback to tail-append if activeUrn is stale or missing
	if idx == -1 {
		idx = max(len(t.nodes)-1,
			// empty dag fallback
			0)
	}

	var spliced []DAGNode
	for _, n := range newNodes {
		spliced = append(spliced, DAGNode{
			Name:  n,
			State: dagStatusWaiting,
		})
	}

	// Safely reconstruct the slice to prevent concurrent dashboard SSE read panics
	var head []DAGNode
	if len(t.nodes) > 0 {
		head = append([]DAGNode(nil), t.nodes[:idx+1]...)
	}
	var tail []DAGNode
	if len(t.nodes) > idx+1 {
		tail = append([]DAGNode(nil), t.nodes[idx+1:]...)
	}

	t.nodes = head
	t.nodes = append(t.nodes, spliced...)
	t.nodes = append(t.nodes, tail...)
}

// UpdateActiveNode hooks into CallProxy to track active execution payload bounds dynamically.
func (t *DAGTracker) UpdateActiveNode(urn string, tokens, bytesRaw, bytesMin int64, cacheAction, cssaHash string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.activeNode.Name = urn
	t.activeNode.Tokens = tokens
	t.activeNode.BytesRaw = bytesRaw
	t.activeNode.BytesMinified = bytesMin
	t.activeNode.CacheAction = cacheAction
	t.activeNode.CSSAHash = cssaHash

	for i := range t.nodes {
		if t.nodes[i].Name == urn && t.nodes[i].State == dagStatusWaiting {
			t.nodes[i].State = dagStatusExecuting
			t.nodes[i].StartTime = time.Now()
			t.currentNode = int64(i + 1)
			break
		}
	}
}

// RecordFault registers a structural fault with fallback trajectory bounds.
func (t *DAGTracker) RecordFault(urn string, strategy string, retries, limit int64, fallback string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.activeNode.Name == urn {
		t.activeNode.Faults++
		t.activeNode.RollbackStrategy = strategy
		t.activeNode.RetryCount = retries
		t.activeNode.RetryLimit = limit
		t.activeNode.FallbackURN = fallback
	}
}

// ClosePipeline forcefully sweeps remaining nodes and terminates the tracker.
func (t *DAGTracker) ClosePipeline(finalStatus string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.status = finalStatus
	for i := range t.nodes {
		if t.nodes[i].State == dagStatusWaiting {
			t.nodes[i].State = "SKIPPED"
		} else if t.nodes[i].State == dagStatusExecuting && finalStatus == "COMPLETED" {
			// Sweep any dangling executing nodes safely if closed out early
			t.nodes[i].State = "SKIPPED"
		}
	}
}

// CompleteNode marks a URN as finished and calculates latency.
func (t *DAGTracker) CompleteNode(urn string, success bool) {
	t.mu.Lock()
	defer t.mu.Unlock()

	state := "DONE"
	if !success {
		state = "FAILED"
		t.status = "FAILED"
	}

	for i := range t.nodes {
		if t.nodes[i].Name == urn && t.nodes[i].State == dagStatusExecuting {
			t.nodes[i].State = state
			if !t.nodes[i].StartTime.IsZero() {
				t.nodes[i].LatencyMs = time.Since(t.nodes[i].StartTime).Milliseconds()
			}
			break
		}
	}

	// If it's the last node and we succeeded, mark pipeline complete.
	if success && t.currentNode == int64(len(t.nodes)) {
		t.status = "COMPLETED"
	}
}

// Snapshot structurally maps the exact telemetry block for the dashboard natively.
func (t *DAGTracker) Snapshot() map[string]any {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if t.sessionID == "" {
		return nil
	}

	nodes := make([]any, len(t.nodes))
	for i, n := range t.nodes {
		latStr := "..."
		if n.LatencyMs > 0 {
			latStr = time.Duration(n.LatencyMs * int64(time.Millisecond)).String()
		}
		nodes[i] = map[string]any{
			"name":    n.Name,
			"state":   n.State,
			"latency": latStr,
		}
	}

	active := map[string]any{
		"name":              t.activeNode.Name,
		"bytes_raw":         t.activeNode.BytesRaw,
		"bytes_minified":    t.activeNode.BytesMinified,
		"tokens":            t.activeNode.Tokens,
		"cache_action":      t.activeNode.CacheAction,
		"cssa_hash":         t.activeNode.CSSAHash,
		"faults":            t.activeNode.Faults,
		"rollback_strategy": t.activeNode.RollbackStrategy,
		"retry_count":       t.activeNode.RetryCount,
		"retry_limit":       t.activeNode.RetryLimit,
		"fallback_urn":      t.activeNode.FallbackURN,
	}

	globalLat := "..."
	if !t.startTime.IsZero() {
		switch t.status {
		case dagStatusExecuting:
			globalLat = time.Since(t.startTime).Round(time.Millisecond).String()
		case "AWAITING_APPROVAL":
			globalLat = time.Since(t.startTime).Round(time.Millisecond).String() + " (paused)"
		default:
			var sum int64
			for _, n := range t.nodes {
				sum += n.LatencyMs
			}
			globalLat = time.Duration(sum * int64(time.Millisecond)).String()
		}
	}

	return map[string]any{
		"session_id":         t.sessionID,
		"status":             t.status,
		"total_nodes":        int64(len(t.nodes)),
		"current_node_index": t.currentNode,
		"global_latency":     globalLat,
		"entropy_ratio":      t.entropy,
		"total_edges":        t.totalEdges,
		"tree_depth":         t.treeDepth,
		"mutation_depth":     t.mutationDepth,
		"nodes":              nodes,
		"active_node":        active,
	}
}
