package telemetry

import (
	"sync/atomic"
	"time"
)

// TokenVelocity tracks the delta between successive health monitor ticks
// to compute tokens-per-second throughput.
var TokenVelocity struct {
	LastCount atomic.Int64
	LastTick  atomic.Int64 // UnixNano
}

// ComputeTokenVelocity calculates the current token velocity in tokens/second
// based on the delta since the last call. Returns 0.0 on first invocation.
func ComputeTokenVelocity(currentTotal int64) float64 {
	now := time.Now().UnixNano()
	lastCount := TokenVelocity.LastCount.Swap(currentTotal)
	lastTick := TokenVelocity.LastTick.Swap(now)

	if lastTick == 0 {
		return 0.0
	}

	elapsed := float64(now-lastTick) / float64(time.Second)
	if elapsed <= 0 {
		return 0.0
	}

	delta := currentTotal - lastCount
	if delta <= 0 {
		return 0.0
	}

	return float64(delta) / elapsed
}

// IPCSessionCounters tracks proxy client lifecycle events and IDE network traffic.
// TEL-3: Enhanced with SSE/POST byte split, stream resumption, rate limiting, and readiness metrics.
var IPCSessionCounters struct {
	Connects         atomic.Int64
	Disconnects      atomic.Int64
	Active           atomic.Int64
	TotalBytes       atomic.Int64
	PostRequests     atomic.Int64 // TEL-3: Total non-SSE MCP POST requests from IDE.
	SSEBytesSent     atomic.Int64 // TEL-3: Bytes sent specifically over SSE streams.
	PostBytesSent    atomic.Int64 // TEL-3: Bytes sent in POST JSON responses.
	SSEResumed       atomic.Int64 // TEL-3: Stream resumption events (Last-Event-ID present).
	RateLimitRejects atomic.Int64 // TEL-3: IPC requests rejected by rate limiter.
	Readiness503s    atomic.Int64 // TEL-3: Requests rejected during boot.
}

// IDEThroughput tracks the delta between successive health monitor ticks
// to compute IDE HTTP stream throughput in bytes/second.
var IDEThroughput struct {
	LastBytes atomic.Int64
	LastTick  atomic.Int64 // UnixNano
}

// ComputeIDEThroughput calculates the current IDE multiplexer throughput in bytes/second.
func ComputeIDEThroughput(currentBytes int64) float64 {
	now := time.Now().UnixNano()
	lastBytes := IDEThroughput.LastBytes.Swap(currentBytes)
	lastTick := IDEThroughput.LastTick.Swap(now)

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

	return float64(delta) / elapsed
}

// LatestSnapshot holds the most recent telemetry snapshot map for UDP broadcast.
// Written by WriteSnapshot in the health monitor, read by the UDP server ticker.
var LatestSnapshot atomic.Value // stores map[string]any
