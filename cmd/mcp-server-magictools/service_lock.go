package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// serviceLockDir is the directory holding the per-label lifecycle lock.
// Tests point it at a scratch directory so they never lock the operator config.
var serviceLockDir = func() (string, error) { return config.DefaultConfigDir(), nil }

func serviceLockPath(label string) (string, error) {
	dir, err := serviceLockDir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "service-"+label+".lock"), nil
}

func withServiceLock(ctx context.Context, label string, fn func(context.Context) error) error {
	path, err := serviceLockPath(label)
	if err != nil {
		return err
	}
	release, err := acquireServiceLock(ctx, path)
	if err != nil {
		return fmt.Errorf("acquire service lock: %w", err)
	}
	defer release()
	return fn(ctx)
}
