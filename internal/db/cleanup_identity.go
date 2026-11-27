package db

import (
	"math"
	"os"
	"path/filepath"

	"github.com/shirou/gopsutil/v4/process"
)

// lookupExecutable reads one pid's executable. Tests replace it. It does not
// enumerate processes.
var lookupExecutable = readExecutable

func readExecutable(pid int) (string, error) {
	if pid <= 0 || pid > math.MaxInt32 {
		return "", os.ErrInvalid
	}
	p, err := process.NewProcess(int32(pid)) //nolint:gosec // range checked above
	if err != nil {
		return "", err
	}
	exe, err := p.Exe()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	return exe, nil
}

func selfExecutable() (string, error) {
	bin, err := os.Executable()
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	return bin, nil
}

func selfStart() (uint64, error) {
	return processStart(os.Getpid())
}

func processStart(pid int) (uint64, error) {
	if pid <= 0 || pid > math.MaxInt32 {
		return 0, os.ErrInvalid
	}
	p, err := process.NewProcess(int32(pid)) //nolint:gosec // range checked above
	if err != nil {
		return 0, err
	}
	ms, err := p.CreateTime()
	if err != nil || ms <= 0 {
		return 0, err
	}
	return uint64(ms), nil
}
