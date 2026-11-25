//go:build unix

package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func acquireServiceLock(ctx context.Context, path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600) //nolint:gosec // lock path is the per-user config dir or a test scratch dir
	if err != nil {
		return nil, err
	}
	locked, err := waitServiceLock(ctx, f)
	if err != nil || !locked {
		_ = f.Close() //nolint:errcheck // closing a lock file that was never acquired
		return nil, err
	}
	return func() {
		_ = unix.Flock(int(f.Fd()), unix.LOCK_UN) //nolint:errcheck // unlock is best-effort on the way out
		_ = f.Close()                             //nolint:errcheck // the lock is already released
	}, nil
}

func waitServiceLock(ctx context.Context, f *os.File) (bool, error) {
	for {
		err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return true, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
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
