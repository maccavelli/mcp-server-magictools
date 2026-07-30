//go:build !windows

// Package main: non-Windows stubs for the Windows SCM integration (service_windows.go).
//
// These never execute at runtime on non-Windows platforms — the CLI dispatches
// into them only under `runtime.GOOS == "windows"` — but they must exist so the
// shared, build-tag-free service.go compiles everywhere.
package main

import (
	"context"
	"fmt"
)

// runServe simply runs fn on non-Windows platforms; there is no SCM dispatch.
func runServe(parent context.Context, fn func(context.Context) error) error {
	return fn(parent)
}

func installWindowsService(binPath string, env []string) error {
	return fmt.Errorf("windows service is not supported on this platform")
}

func uninstallWindowsService() error {
	return fmt.Errorf("windows service is not supported on this platform")
}

func startWindowsService() error {
	return fmt.Errorf("windows service is not supported on this platform")
}

func stopWindowsService() error {
	return fmt.Errorf("windows service is not supported on this platform")
}

func windowsServiceInstalled() (bool, error) {
	return false, fmt.Errorf("windows service is not supported on this platform")
}
