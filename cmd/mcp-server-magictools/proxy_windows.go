//go:build windows

// Package cmd provides functionality for the cmd subsystem.
package main

func initWindowsStdio() {
	// In Go, os.Stdin and os.Stdout do not perform CRLF translation.
	// They operate in binary mode natively.
}
