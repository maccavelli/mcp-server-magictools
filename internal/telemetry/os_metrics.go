package telemetry

import (
	"log/slog"
	"os"
	"runtime"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// Global start time of the orchestrator for exact uptime tracking.
var StartTime = time.Now()

// ProcessStats is undocumented but satisfies standard structural requirements.
type ProcessStats struct {
	PID          int32   `json:"pid"`
	UptimeString string  `json:"uptime"`
	CPUPercent   float64 `json:"cpu_percent"`
	MemoryRSS_MB float64 `json:"memory_rss_mb"`
	MemoryVMS_MB float64 `json:"memory_vms_mb"`
	Goroutines   int     `json:"goroutines"`
}

// GetSystemProcessStats uses pure Go (gopsutil) to read OS-level process footprint
func GetSystemProcessStats() *ProcessStats {
	pid := safeInt32FromPID(os.Getpid())

	// Default fallbacks using the runtime if OS-level parsing fails
	stats := &ProcessStats{
		PID:          pid,
		UptimeString: time.Since(StartTime).Round(time.Second).String(),
		Goroutines:   runtime.NumGoroutine(),
	}

	p, err := process.NewProcess(pid)
	if err != nil {
		slog.Warn("Failed to locate process for metrics", "error", err)
		return stats
	}

	// Calculate CPU (This calculates rolling utilization since last call internally in gopsutil.
	// For immediate calls, it may return 0, so long-running servers like magictools report accurately).
	cpuPct, err := p.CPUPercent()
	if err == nil {
		stats.CPUPercent = cpuPct
	}

	memInfo, err := p.MemoryInfo()
	if err == nil {
		stats.MemoryRSS_MB = float64(memInfo.RSS) / 1024 / 1024
		stats.MemoryVMS_MB = float64(memInfo.VMS) / 1024 / 1024
	}

	return stats
}
