// Package main provides functionality for the main subsystem.
package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// watchdog monitors the parent process and triggers shutdown if it exits.
func watchdog(ctx context.Context, cancel context.CancelFunc) {
	initialPPID := os.Getppid()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if os.Getppid() != initialPPID {
				slog.Info("parent process died; shutting down")
				cancel()
				return
			}
		}
	}
}

// watchBinary monitors the executable for changes and triggers graceful shutdown.
// Disabled in service mode where systemd/launchd manages restarts natively.
func watchBinary(ctx context.Context, cancel context.CancelFunc) {
	if os.Getenv("MCP_SERVICE_MODE") == "true" {
		slog.Info("watchBinary: disabled in service mode (systemd/launchd manages restarts)")
		return
	}

	exe, err := os.Executable()
	if err != nil {
		slog.Warn("watchBinary: failed to get executable path", "error", err)
		return
	}

	exeDir := filepath.Dir(exe)
	exeName := filepath.Base(exe)

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Warn("watchBinary: failed to create fsnotify watcher", "error", err)
		return
	}
	defer closeWatcherOrWarn(watcher)

	if err := watcher.Add(exeDir); err != nil {
		slog.Warn("watchBinary: failed to watch executable directory", "dir", exeDir, "error", err)
		return
	}

	slog.Info("watchBinary: successfully added auto-reload watch on executable", "file", exe)

	for {
		select {
		case <-ctx.Done():
			return
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}

			if filepath.Base(event.Name) == exeName {
				if event.Has(fsnotify.Write) || event.Has(fsnotify.Rename) || event.Has(fsnotify.Create) || event.Has(fsnotify.Remove) || event.Has(fsnotify.Chmod) {
					slog.Info("watchBinary: binary change detected; initiating graceful shutdown", "event", event.Op.String())

					// Debounce to allow any ongoing writes/moves to settle before we exit
					debounce := time.NewTimer(500 * time.Millisecond)
					select {
					case <-debounce.C:
					case <-ctx.Done():
						debounce.Stop()
					}
					cancel()
					return
				}
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			slog.Warn("watchBinary: fsnotify error", "error", err)
		}
	}
}
