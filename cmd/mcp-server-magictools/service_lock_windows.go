//go:build windows

package main

import (
	"context"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

func acquireServiceLock(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	locked, err := waitServiceLock(ctx, f)
	if err != nil || !locked {
		_ = f.Close() //nolint:errcheck // closing a lock file that was never acquired
		return nil, err
	}
	return func() {
		var ol windows.Overlapped
		_ = windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &ol) //nolint:errcheck // unlock is best-effort on the way out
		_ = f.Close()                                                  //nolint:errcheck // the lock is already released
	}, nil
}

func waitServiceLock(ctx context.Context, f *os.File) (bool, error) {
	for {
		var ol windows.Overlapped
		err := windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &ol)
		if err == nil {
			return true, nil
		}
		if err != windows.ERROR_LOCK_VIOLATION {
			return false, err
		}
		timer := time.NewTimer(20 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false, ctx.Err()
		case <-timer.C:
		}
	}
}
