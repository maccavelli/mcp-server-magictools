package util

import (
	"bytes"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
)

// MatchEnviron checks if a specific target entry exists in the null-terminated environment block.
func MatchEnviron(data []byte, target string) bool {
	// /proc/*/environ is null-byte separated
	entries := bytes.SplitSeq(data, []byte{0})
	for entry := range entries {
		if string(entry) == target {
			return true
		}
	}
	return false
}

// MatchEnvironPrefix checks if any entry in the null-terminated environment block starts with the given prefix.
func MatchEnvironPrefix(data []byte, prefix string) bool {
	entries := bytes.SplitSeq(data, []byte{0})
	for entry := range entries {
		if strings.HasPrefix(string(entry), prefix) {
			return true
		}
	}
	return false
}

// GetRSS returns the Resident Set Size of the process in bytes.
// Cross-platform using gopsutil.
func GetRSS(pid int) (uint64, error) {
	p, err := process.NewProcess(safeInt32FromPID(pid))
	if err != nil {
		return 0, err
	}
	mem, err := p.MemoryInfo()
	if err != nil {
		return 0, err
	}
	return mem.RSS, nil
}

// GetProcessCPU returns CPU percentage for the process.
func GetProcessCPU(pid int) (float64, error) {
	p, err := process.NewProcess(safeInt32FromPID(pid))
	if err != nil {
		return 0, err
	}
	// Note: First call to CPUPercent usually returns 0. To get an actual rate,
	// one would need to measure over an interval. For the health check,
	// we will measure over time in the monitor loop.
	return p.CPUPercent()
}

// GetProcessEnviron returns the environment variables for the process as a slice of "KEY=VALUE".
func GetProcessEnviron(pid int) ([]string, error) {
	p, err := process.NewProcess(safeInt32FromPID(pid))
	if err != nil {
		return nil, err
	}
	return p.Environ()
}
