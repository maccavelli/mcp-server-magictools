package telemetry

import (
	"os"
	"runtime"
	"strconv"
)

// RuntimeSnapshot captures the orchestrator's Go runtime state for observability.
type RuntimeSnapshot struct {
	HeapAllocMB  float64 `json:"heap_alloc_mb"`
	HeapSysMB    float64 `json:"heap_sys_mb"`
	NumGC        uint32  `json:"num_gc"`
	PauseTotalMs float64 `json:"pause_total_ms"`
	NumGoroutine int     `json:"num_goroutine"`
	GoMaxProcs   int     `json:"go_max_procs"`
	GoMemLimitMB int64   `json:"go_mem_limit_mb"` // -1 if not set
	HeadroomPct  float64 `json:"headroom_pct"`    // % of GOMEMLIMIT remaining
}

// CaptureRuntime reads the Go runtime memory statistics for the orchestrator process.
// Should be called on flush ticks (60s) to minimize STW impact.
func CaptureRuntime() RuntimeSnapshot {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	snap := RuntimeSnapshot{
		HeapAllocMB:  float64(m.HeapAlloc) / 1024 / 1024,
		HeapSysMB:    float64(m.HeapSys) / 1024 / 1024,
		NumGC:        m.NumGC,
		PauseTotalMs: float64(m.PauseTotalNs) / 1e6,
		NumGoroutine: runtime.NumGoroutine(),
		GoMaxProcs:   runtime.GOMAXPROCS(0),
		GoMemLimitMB: -1, // sentinel for "not set"
	}

	// Parse GOMEMLIMIT if set
	if envVal := os.Getenv("GOMEMLIMIT"); envVal != "" {
		if bytes, err := parseMemLimit(envVal); err == nil && bytes > 0 {
			snap.GoMemLimitMB = bytes / (1024 * 1024)
			snap.HeadroomPct = (1.0 - float64(m.HeapAlloc)/float64(bytes)) * 100
		}
	}

	return snap
}

// parseMemLimit parses a GOMEMLIMIT value like "256MiB" or "1GiB" into bytes.
func parseMemLimit(s string) (int64, error) {
	if len(s) > 3 {
		suffix := s[len(s)-3:]
		numPart := s[:len(s)-3]
		switch suffix {
		case "GiB":
			n, err := strconv.ParseFloat(numPart, 64)
			return int64(n * 1024 * 1024 * 1024), err
		case "MiB":
			n, err := strconv.ParseFloat(numPart, 64)
			return int64(n * 1024 * 1024), err
		case "KiB":
			n, err := strconv.ParseFloat(numPart, 64)
			return int64(n * 1024), err
		}
	}
	// Try plain bytes
	return strconv.ParseInt(s, 10, 64)
}
