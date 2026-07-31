//go:build !darwin

package util

import "github.com/shirou/gopsutil/v4/process"

// GetProcessEnviron returns the environment variables for the process as a
// slice of "KEY=VALUE" entries. gopsutil implements this on Linux
// (/proc/<pid>/environ) and Windows (PEB); darwin has its own implementation
// in environ_darwin.go.
func GetProcessEnviron(pid int) ([]string, error) {
	p, err := process.NewProcess(safeInt32FromPID(pid))
	if err != nil {
		return nil, err
	}
	return p.Environ()
}
