//go:build windows

package config

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

type fileLock struct {
	file *os.File
}

func lockFile(path string) (*fileLock, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("failed to open lock file %s: %w", path, err)
	}

	var ol windows.Overlapped
	err = windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &ol)
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("failed to acquire LockFileEx on %s: %w", path, err)
	}

	return &fileLock{file: f}, nil
}

func (l *fileLock) unlock() error {
	if l == nil || l.file == nil {
		return nil
	}
	defer func() {
		_ = l.file.Close()
	}()
	var ol windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(l.file.Fd()), 0, 1, 0, &ol)
}
