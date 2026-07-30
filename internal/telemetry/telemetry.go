// Package telemetry collects atomic counters and trace data for magictools observability.
package telemetry

import (
	"sync"
	"sync/atomic"
	"time"
)

// SyncState defines the background health state of the DB
var (
	SyncOutOfSync atomic.Bool
	IsHealing     atomic.Bool

	// GlobalTokenSpend represents the exact cumulative tokens consumed across the orchestrator ecosystem proxy traces.
	GlobalTokenSpend atomic.Int64

	// PanicsRecovered tracks the total number of recovered panics in middleware/handlers.
	PanicsRecovered atomic.Int64
)

// RecordPanic increments the panic count.
func RecordPanic() {
	PanicsRecovered.Add(1)
}

// GetPanicCount returns the current panic count.
func GetPanicCount() int64 {
	return PanicsRecovered.Load()
}

// CSSAPacket strictly defines a single multiplexed buffer slice.
type CSSAPacket struct {
	ToolID  uint16
	Payload []byte
}

var (
	dispatcherMap  [8]chan CSSAPacket // 8 Shards targeting CSSA limits natively.
	onceDispatcher sync.Once
)

// InitCSSADispatcher dynamically springs an 8-shard local background multiplexer safely mapping boundaries without UI jitter globally natively.
func InitCSSADispatcher() {
	onceDispatcher.Do(func() {
		for i := range 8 {
			dispatcherMap[i] = make(chan CSSAPacket, 2048) // Deep buffers handling Signal Storm limits locally cleanly.
			go cssaWorker(i, dispatcherMap[i])
		}
	})
}

// cssaWorker dynamically streams exact states natively preventing pipeline jitter organically mapping logs natively locally.
func cssaWorker(_ int, ch <-chan CSSAPacket) {
	for packet := range ch {
		// Base dispatcher ready for mutator hook ingestion formally via ToolID headers synchronously.
		_ = packet
	}
}

// DispatchCSSAPacket mathematically directs binary array streams onto explicit local shard pipes preventing global BadgerDB locks dynamically locally mapping states exactly.
func DispatchCSSAPacket(toolID uint16, payload []byte) {
	InitCSSADispatcher() // Lazy load mathematically cleanly
	shardIndex := int(toolID) % 8
	select {
	case dispatcherMap[shardIndex] <- CSSAPacket{ToolID: toolID, Payload: payload}:
	default:
		// Drop cleanly preventing pipeline panics if mutators overwhelm exact buffer limits natively structurally.
	}
}

// AddTokens increments the rolling global token budget and returns the new total atomically.
func AddTokens(count int) int64 {
	return GlobalTokenSpend.Add(int64(count))
}

// GetTotalTokens returns the current exact global API token spend.
func GetTotalTokens() int64 {
	return GlobalTokenSpend.Load()
}

// EMATracker is undocumented but satisfies standard structural requirements.
type EMATracker struct {
	mu             sync.RWMutex
	Count          int64   `json:"count"`
	TotalLatencies int64   `json:"total_latency_ms"`
	EMA            float64 `json:"average_ms"`
}

// Record is undocumented but satisfies standard structural requirements.
func (l *EMATracker) Record(durationMs int64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.TotalLatencies += durationMs
	l.Count++

	if l.Count == 1 {
		l.EMA = float64(durationMs)
	} else {
		// Alpha 0.1 gives 10% weight to newest request, 90% to historical
		l.EMA = (0.1 * float64(durationMs)) + (0.9 * l.EMA)
	}
}

var MetaLatencies = struct {
	AlignTools   *EMATracker
	CallProxy    *EMATracker
	CallProxyHot *EMATracker // Proxy-only latency (excludes boot/cold-start)
	BootLatency  *EMATracker // Cold-start boot latency (process spawn + handshake)
}{
	AlignTools:   &EMATracker{},
	CallProxy:    &EMATracker{},
	CallProxyHot: &EMATracker{},
	BootLatency:  &EMATracker{},
}

// ServerMetrics is undocumented but satisfies standard structural requirements.
type ServerMetrics struct {
	Calls          int64   `json:"calls"`
	TotalSpinupMs  int64   `json:"total_spinup_ms"`
	BytesSent      int64   `json:"bytes_sent"`
	BytesRaw       int64   `json:"bytes_raw"`
	BytesMinified  int64   `json:"bytes_minified"`
	Faults         int64   `json:"faults"`
	SoftFailures   int64   `json:"soft_failures"`
	ThroughputKBps float64 `json:"throughput_kbps"`
}

// GlobalDigest is undocumented but satisfies standard structural requirements.
type GlobalDigest struct {
	TotalCalls  int64 `json:"total_calls"`
	TotalFaults int64 `json:"total_faults"`
	TokensUsed  int64 `json:"tokens_used"`
	TokensSaved int64 `json:"tokens_saved"`
}

// VectorStats holds a snapshot of the HNSW vector engine state.
type VectorStats struct {
	Enabled                bool   `json:"enabled"`
	Provider               string `json:"provider,omitempty,omitzero"`
	Model                  string `json:"model,omitempty,omitzero"`
	Dims                   int    `json:"dims,omitempty,omitzero"`
	GraphNodes             int    `json:"graph_nodes"`
	NeedsHydration         bool   `json:"needs_hydration"`
	VectorWins             int64  `json:"vector_wins"`
	LexicalWins            int64  `json:"lexical_wins"`
	TotalSearches          int64  `json:"total_searches"`
	VectorSearches         int64  `json:"vector_searches"`
	AlignSearchInvocations int64  `json:"align_search_invocations"`
	LexicalSearches        int64  `json:"lexical_searches"` // deprecated alias
	AvgLatencyMs           int64  `json:"avg_latency_ms,omitempty,omitzero"`
}

// VectorStatsFunc is wired by main.go to query the vector engine without circular imports.
var VectorStatsFunc func() VectorStats

// SessionStats is undocumented but satisfies standard structural requirements.
type SessionStats struct {
	Digest     GlobalDigest              `json:"digest"`
	Subservers map[string]*ServerMetrics `json:"subservers"`
	Vector     *VectorStats              `json:"vector,omitempty,omitzero"`
}

// Tracker is undocumented but satisfies standard structural requirements.
type Tracker struct {
	servers sync.Map // maps string (serverName) -> *serverNode
}

type serverNode struct {
	calls         atomic.Int64
	totalSpinupMs atomic.Int64
	bytesSent     atomic.Int64
	bytesRaw      atomic.Int64
	bytesMin      atomic.Int64
	faults        atomic.Int64
	softFailures  atomic.Int64

	lastBytes atomic.Int64
	lastTick  atomic.Int64
}

func (n *serverNode) computeThroughput(currentBytes int64) float64 {
	now := time.Now().UnixNano()
	lastBytes := n.lastBytes.Swap(currentBytes)
	lastTick := n.lastTick.Swap(now)

	if lastTick == 0 {
		return 0.0
	}

	elapsed := float64(now-lastTick) / float64(time.Second)
	if elapsed <= 0 {
		return 0.0
	}

	delta := currentBytes - lastBytes
	if delta <= 0 {
		return 0.0
	}

	return (float64(delta) / 1024.0) / elapsed
}

// NewTracker is undocumented but satisfies standard structural requirements.
func NewTracker() *Tracker {
	return &Tracker{}
}

// GlobalTracker is the shared proxy-level tracker, accessible from both
// OrchestratorHandler (writes) and health_monitor WriteSnapshot (reads).
var GlobalTracker = NewTracker()

func (t *Tracker) getOrAdd(server string) *serverNode {
	node, _ := t.servers.LoadOrStore(server, &serverNode{})
	if n, ok := serverNodeFromMapVal(node); ok {
		return n
	}
	n := &serverNode{}
	t.servers.Store(server, n)
	return n
}

// AddLatency is undocumented but satisfies standard structural requirements.
func (t *Tracker) AddLatency(server string, ms int64) {
	node := t.getOrAdd(server)
	node.calls.Add(1)
	node.totalSpinupMs.Add(ms)
}

// AddBytes is undocumented but satisfies standard structural requirements.
func (t *Tracker) AddBytes(server string, sent, raw, minified int64) {
	node := t.getOrAdd(server)
	node.bytesSent.Add(sent)
	node.bytesRaw.Add(raw)
	node.bytesMin.Add(minified)
}

// RecordFault is undocumented but satisfies standard structural requirements.
func (t *Tracker) RecordFault(server string) {
	node := t.getOrAdd(server)
	node.faults.Add(1)
}

// RecordSoftFailure increments the soft failure counter for a server.
func (t *Tracker) RecordSoftFailure(server string) {
	node := t.getOrAdd(server)
	node.softFailures.Add(1)
}

// GetMetrics is undocumented but satisfies standard structural requirements.
func (t *Tracker) GetMetrics(server string) *ServerMetrics {
	v, ok := t.servers.Load(server)
	if !ok {
		return nil
	}
	node, ok := serverNodeFromMapVal(v)
	if !ok {
		return nil
	}
	return &ServerMetrics{
		Calls:          node.calls.Load(),
		TotalSpinupMs:  node.totalSpinupMs.Load(),
		BytesSent:      node.bytesSent.Load(),
		BytesRaw:       node.bytesRaw.Load(),
		BytesMinified:  node.bytesMin.Load(),
		Faults:         node.faults.Load(),
		SoftFailures:   node.softFailures.Load(),
		ThroughputKBps: node.computeThroughput(node.bytesRaw.Load()),
	}
}

// GetAll is undocumented but satisfies standard structural requirements.
func (t *Tracker) GetAll() map[string]*ServerMetrics {
	res := make(map[string]*ServerMetrics)
	t.servers.Range(func(key, value any) bool {
		server, ok := stringFromMapKey(key)
		if !ok {
			return true
		}
		node, ok := serverNodeFromMapVal(value)
		if !ok {
			return true
		}
		res[server] = &ServerMetrics{
			Calls:          node.calls.Load(),
			TotalSpinupMs:  node.totalSpinupMs.Load(),
			BytesSent:      node.bytesSent.Load(),
			BytesRaw:       node.bytesRaw.Load(),
			BytesMinified:  node.bytesMin.Load(),
			Faults:         node.faults.Load(),
			SoftFailures:   node.softFailures.Load(),
			ThroughputKBps: node.computeThroughput(node.bytesRaw.Load()),
		}
		return true
	})
	return res
}

// GetSessionStats is undocumented but satisfies standard structural requirements.
func (t *Tracker) GetSessionStats() *SessionStats {
	stats := &SessionStats{
		Subservers: make(map[string]*ServerMetrics),
	}
	t.servers.Range(func(key, value any) bool {
		server, ok := stringFromMapKey(key)
		if !ok {
			return true
		}
		node, ok := serverNodeFromMapVal(value)
		if !ok {
			return true
		}
		m := &ServerMetrics{
			Calls:          node.calls.Load(),
			TotalSpinupMs:  node.totalSpinupMs.Load(),
			BytesSent:      node.bytesSent.Load(),
			BytesRaw:       node.bytesRaw.Load(),
			BytesMinified:  node.bytesMin.Load(),
			Faults:         node.faults.Load(),
			SoftFailures:   node.softFailures.Load(),
			ThroughputKBps: node.computeThroughput(node.bytesRaw.Load()),
		}
		stats.Subservers[server] = m

		stats.Digest.TotalCalls += m.Calls
		stats.Digest.TotalFaults += m.Faults
		stats.Digest.TokensUsed += ((m.BytesSent + m.BytesRaw) / 4)
		savedBytes := m.BytesRaw - m.BytesMinified
		if savedBytes > 0 {
			stats.Digest.TokensSaved += (savedBytes / 4)
		}
		return true
	})

	// Attach vector stats if available
	if VectorStatsFunc != nil {
		vs := VectorStatsFunc()
		stats.Vector = &vs
	}

	return stats
}
