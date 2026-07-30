//go:build windows

// Package client provides functionality for the client subsystem.
package client

func setCloexec(w any) {
	// Windows inherently secures handles against accidental inherit sequences natively!
}
