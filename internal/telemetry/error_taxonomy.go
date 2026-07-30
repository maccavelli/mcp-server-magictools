package telemetry

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ErrorTaxonomyCounters tracks different classes of orchestrator errors.
type ErrorTaxonomyCounters struct {
	Timeout              atomic.Int64
	ConnectionRefused    atomic.Int64
	Panic                atomic.Int64
	Validation           atomic.Int64
	HallucinationBlocked atomic.Int64
	PipeError            atomic.Int64
	ContextCancelled     atomic.Int64
}

// ErrorTaxonomy is the global instance tracking specific error buckets.
var ErrorTaxonomy = &ErrorTaxonomyCounters{}

// ErrorRingEntry holds a single recent error event.
type ErrorRingEntry struct {
	Timestamp     int64
	Server        string
	CorrelationID string
	Message       string
}

// ErrorRing maintains a fixed-size ring buffer of recent errors.
type ErrorRing struct {
	entries []ErrorRingEntry
	head    int
	size    int
	mu      sync.Mutex
}

// NewErrorRing creates a new ring buffer with the stated capacity.
func NewErrorRing(size int) *ErrorRing {
	return &ErrorRing{
		entries: make([]ErrorRingEntry, size),
		size:    size,
	}
}

// RecentErrors is the global ring buffer for recent errors.
var RecentErrors = NewErrorRing(50)

// Record adds an error event to the ring buffer.
func (r *ErrorRing) Record(server, correlationID, message string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries[r.head] = ErrorRingEntry{
		Timestamp:     time.Now().UnixNano(),
		Server:        server,
		CorrelationID: correlationID,
		Message:       message, // In the UI we truncate this if it's too long
	}
	r.head = (r.head + 1) % r.size
}

// GetAll returns a snapshot of the ring buffer, ordered oldest to newest.
func (r *ErrorRing) GetAll() []ErrorRingEntry {
	r.mu.Lock()
	defer r.mu.Unlock()

	var result []ErrorRingEntry
	for i := range r.size {
		idx := (r.head + i) % r.size
		if r.entries[idx].Timestamp > 0 {
			result = append(result, r.entries[idx])
		}
	}
	return result
}

// Classify inspects an error and increments the appropriate taxonomy counter.
func (c *ErrorTaxonomyCounters) Classify(err error) {
	if err == nil {
		return
	}
	msg := err.Error()

	if strings.Contains(msg, "timeout") || strings.Contains(msg, "context deadline exceeded") {
		c.Timeout.Add(1)
	} else if strings.Contains(msg, "connection refused") || strings.Contains(msg, "EPIPE") {
		c.ConnectionRefused.Add(1)
	} else if strings.Contains(msg, "panic") {
		c.Panic.Add(1)
	} else if strings.Contains(msg, "VALIDATION_ERROR") || strings.Contains(msg, "schema constraints") {
		c.Validation.Add(1)
	} else if strings.Contains(msg, "hallucinated") || strings.Contains(msg, "FORBIDDEN") || strings.Contains(msg, "unsupported schema string") {
		c.HallucinationBlocked.Add(1)
	} else if strings.Contains(msg, "pipe") || strings.Contains(msg, "broken pipe") || strings.Contains(msg, "EOF") {
		c.PipeError.Add(1)
	} else if strings.Contains(msg, "context canceled") {
		c.ContextCancelled.Add(1)
	}
}
