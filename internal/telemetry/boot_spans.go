package telemetry

import (
	"sync"
	"time"
)

// BootSpan represents a single timed phase during the orchestrator boot sequence.
// TEL-1: Structured boot timing spans for dashboard consumption and Recall archival.
type BootSpan struct {
	Server   string `json:"server"`
	Phase    string `json:"phase"` // "handshake", "tools_list", "index", "vector_hydrate"
	Duration int64  `json:"duration_ms"`
	Error    string `json:"error,omitempty,omitzero"`
}

// BootTracer collects structured spans during the boot sequence.
type BootTracer struct {
	mu    sync.Mutex
	spans []BootSpan
	start time.Time
}

// GlobalBootTracer is the shared boot sequence tracer.
var GlobalBootTracer = &BootTracer{}

// Start marks the beginning of a boot span.
func (t *BootTracer) Start() {
	t.mu.Lock()
	t.start = time.Now()
	t.spans = nil
	t.mu.Unlock()
}

// Record adds a completed boot span.
func (t *BootTracer) Record(server, phase string, duration time.Duration, err error) {
	span := BootSpan{
		Server:   server,
		Phase:    phase,
		Duration: duration.Milliseconds(),
	}
	if err != nil {
		span.Error = err.Error()
	}

	t.mu.Lock()
	t.spans = append(t.spans, span)
	t.mu.Unlock()
}

// Spans returns a snapshot of all recorded boot spans.
func (t *BootTracer) Spans() []BootSpan {
	t.mu.Lock()
	defer t.mu.Unlock()
	result := make([]BootSpan, len(t.spans))
	copy(result, t.spans)
	return result
}

// TotalDuration returns the elapsed time since Start() was called.
func (t *BootTracer) TotalDuration() time.Duration {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.start.IsZero() {
		return 0
	}
	return time.Since(t.start)
}

// ProxyRelayTracker tracks per-request latency for the stdio→HTTP→response relay cycle.
// TEL-5: Proxy relay latency histogram with percentile tracking.
type ProxyRelayTracker struct {
	mu      sync.Mutex
	samples []int64
	maxSize int
}

// GlobalRelayTracker is the shared proxy relay latency tracker.
var GlobalRelayTracker = &ProxyRelayTracker{maxSize: 1000}

// Record adds a relay latency sample in milliseconds.
func (p *ProxyRelayTracker) Record(durationMs int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.samples) >= p.maxSize {
		// Rotate: drop oldest half
		p.samples = p.samples[p.maxSize/2:]
	}
	p.samples = append(p.samples, durationMs)
}

// Stats returns the count, average, min, max, and p95 of relay latencies.
func (p *ProxyRelayTracker) Stats() map[string]any {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.samples)
	if n == 0 {
		return map[string]any{
			"count": 0,
			"avg":   0,
			"min":   0,
			"max":   0,
			"p95":   0,
		}
	}

	// Calculate without sorting (approximate p95)
	var sum, minLat, maxLat int64
	minLat = p.samples[0]
	for _, s := range p.samples {
		sum += s
		if s < minLat {
			minLat = s
		}
		if s > maxLat {
			maxLat = s
		}
	}

	// Approximate p95 using sorted copy
	sorted := make([]int64, n)
	copy(sorted, p.samples)
	sortInt64s(sorted)
	p95Idx := int(float64(n) * 0.95)
	if p95Idx >= n {
		p95Idx = n - 1
	}

	return map[string]any{
		"count": n,
		"avg":   sum / int64(n),
		"min":   minLat,
		"max":   maxLat,
		"p95":   sorted[p95Idx],
	}
}

// sortInt64s performs an insertion sort (efficient for small N ≤ 1000).
func sortInt64s(a []int64) {
	for i := 1; i < len(a); i++ {
		key := a[i]
		j := i - 1
		for j >= 0 && a[j] > key {
			a[j+1] = a[j]
			j--
		}
		a[j+1] = key
	}
}
