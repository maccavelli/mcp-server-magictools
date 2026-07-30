//go:build windows

// Package ui provides cross-platform terminal initialization for the MagicTools dashboard.
package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

// EnableVirtualTerminalProcessing enables ANSI escape sequence support on Windows.
// This is required for PowerShell and CMD to correctly render colors and cursor moves.
func EnableVirtualTerminalProcessing() error {
	stdout := windows.Handle(os.Stdout.Fd())
	var mode uint32
	if err := windows.GetConsoleMode(stdout, &mode); err == nil {
		mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		mode |= windows.ENABLE_PROCESSED_OUTPUT
		mode |= windows.ENABLE_WRAP_AT_EOL_OUTPUT
		_ = windows.SetConsoleMode(stdout, mode)
	}

	stderr := windows.Handle(os.Stderr.Fd())
	if err := windows.GetConsoleMode(stderr, &mode); err == nil {
		mode |= windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING
		mode |= windows.ENABLE_PROCESSED_OUTPUT
		mode |= windows.ENABLE_WRAP_AT_EOL_OUTPUT
		_ = windows.SetConsoleMode(stderr, mode)
	}

	return nil
}
