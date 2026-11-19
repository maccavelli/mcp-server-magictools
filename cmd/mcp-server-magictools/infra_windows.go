//go:build windows

// Package main provides functionality for the main subsystem.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
)

// setupZombieReaper is a no-op on Windows since process groups inherently track lifecycles avoiding zombies efficiently.
func setupZombieReaper(ctx context.Context) {}

// enforceResourceLimits is a no-op on Windows since file descriptor scaling is inherently managed by the kernel limits implicitly.
func enforceResourceLimits() {}

// makeSignalContext returns a context cancelled on SIGINT or SIGTERM.
// SIGHUP is not defined on Windows and is intentionally omitted here.
func makeSignalContext(parent context.Context) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
}

// reloadSignals returns nil on Windows — SIGHUP is not delivered, so service-mode
// config reload via signal is unavailable (use the file watcher instead).
func reloadSignals() []os.Signal { return nil }

// isReloadSignal always reports false on Windows.
func isReloadSignal(sig os.Signal) bool { return false }
