//go:build unix || linux || darwin || freebsd || openbsd || netbsd

package config

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

type fileLock struct {
	file *os.File
}

func lockFile(path string) (*fileLock, error) {
	cleanPath := filepath.Clean(path)
	f, err := os.OpenFile(cleanPath, os.O_CREATE|os.O_RDWR, 0600) //nolint:gosec // path is clean config lockfile
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file %s: %w", path, err)
	}

	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX); err != nil {
		_ = f.Close() //nolint:errcheck // cleanup file handle on lock failure
		return nil, fmt.Errorf("failed to acquire flock on %s: %w", path, err)
	}

	return &fileLock{file: f}, nil
}

func (l *fileLock) unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer func() {
		_ = l.file.Close() //nolint:errcheck // cleanup file handle on unlock
	}()
	return unix.Flock(int(l.file.Fd()), unix.LOCK_UN)
}
