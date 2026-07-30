//go:build !windows

// Package ui provides cross-platform terminal initialization for the MagicTools dashboard.
package ui

// EnableVirtualTerminalProcessing is a no-op on non-Windows platforms.
func EnableVirtualTerminalProcessing() error {
	return nil
}
