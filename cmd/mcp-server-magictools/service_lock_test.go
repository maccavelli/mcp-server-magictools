package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestServiceLockSerializesAndCancellationDoesNotReleaseHolder(t *testing.T) {
	dir := t.TempDir()
	old := serviceLockDir
	serviceLockDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { serviceLockDir = old })

	started := make(chan struct{})
	release := make(chan struct{})
	held := make(chan error, 1)
	go func() {
		held <- withServiceLock(context.Background(), "local.magictools.test.lock", func(context.Context) error {
			close(started)
			<-release
			return nil
		})
	}()
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("holder did not acquire the lock")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel()
	err := withServiceLock(ctx, "local.magictools.test.lock", func(context.Context) error {
		t.Fatal("waiter ran while the holder still owned the lock")
		return nil
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter err = %v, want deadline", err)
	}

	close(release)
	if err := <-held; err != nil {
		t.Fatal(err)
	}
	if err := withServiceLock(context.Background(), "local.magictools.test.lock", func(context.Context) error { return nil }); err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
}
