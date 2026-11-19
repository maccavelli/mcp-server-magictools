//go:build !linux

package proctable

import (
	"errors"

	"github.com/shirou/gopsutil/v4/process"
)

// startToken is the process's creation time as gopsutil reads it: p_starttime on darwin and the BSDs, the process
// creation time on Windows. Both are fixed for the life of the process.
func startToken(pid int32) (uint64, error) {
	p, err := process.NewProcess(pid)
	if err != nil {
		return 0, err
	}
	ms, err := p.CreateTime()
	if err != nil {
		return 0, err
	}
	if ms <= 0 {
		return 0, errors.New("proctable: no start time")
	}
	return uint64(ms), nil
}

func pidNamespace() string { return "" }
