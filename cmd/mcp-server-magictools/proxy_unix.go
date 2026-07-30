//go:build !windows

// Package cmd provides functionality for the cmd subsystem.
package main

func initWindowsStdio() {
	// No-op on Unix systems
}
