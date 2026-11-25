// Package serviceidentity provides read-only process identity for service
// lifecycle health. It cannot enumerate or signal processes.
package serviceidentity

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"github.com/shirou/gopsutil/v4/process"
)

// Identity is a process PID, creation token, and canonical executable.
type Identity struct {
	PID        int
	Start      uint64
	Executable string
}

func refuseTest() {
	if testing.Testing() {
		panic("serviceidentity real reader called in a test binary: inject a fake identity")
	}
}

func token(pid int) (uint64, error) {
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

// Self returns this process's creation token and canonical executable.
func Self() (Identity, error) {
	refuseTest()
	pid := os.Getpid()
	start, err := token(pid)
	if err != nil {
		return Identity{}, err
	}
	bin, err := os.Executable()
	if err != nil {
		return Identity{}, err
	}
	if resolved, err := filepath.EvalSymlinks(bin); err == nil {
		bin = resolved
	}
	return Identity{PID: pid, Start: start, Executable: bin}, nil
}

// Verify reports whether the PID retains its creation token and executable.
func Verify(id Identity) (bool, error) {
	refuseTest()
	if id.PID <= 0 || id.Start == 0 {
		return false, nil
	}
	start, err := token(id.PID)
	if err != nil {
		return false, nil //nolint:nilerr // a vanished process is not the recorded identity, not a verification failure
	}
	if start != id.Start {
		return false, nil
	}
	if id.PID > math.MaxInt32 {
		return false, os.ErrInvalid
	}
	p, err := process.NewProcess(int32(id.PID)) //nolint:gosec // range checked above
	if err != nil {
		return false, err
	}
	exe, err := p.Exe()
	if err != nil {
		return false, err
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	want := id.Executable
	if resolved, err := filepath.EvalSymlinks(want); err == nil {
		want = resolved
	}
	return exe == want, nil
}
