//go:build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

func SocketPath() string {
	return filepath.Join(os.TempDir(), "magictools.sock")
}

func ListenPrimary() (net.Listener, error) {
	path := SocketPath()

	// ROB-1: Probe the existing socket before removing it.
	// If another instance is actively listening, fail fast instead of
	// silently destroying its connections.
	if conn, err := net.DialTimeout("unix", path, 500*time.Millisecond); err == nil {
		if closeErr := conn.Close(); closeErr != nil {
			return nil, fmt.Errorf("probe close %s: %w", path, closeErr)
		}
		return nil, fmt.Errorf("another instance is already listening on %s", path)
	}

	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("remove stale socket %s: %w", path, err)
	}

	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}

	// Secure the socket
	if err := os.Chmod(path, 0600); err != nil {
		if closeErr := l.Close(); closeErr != nil {
			return nil, fmt.Errorf("chmod %s: %w (close: %w)", path, err, closeErr)
		}
		return nil, err
	}

	return l, nil
}

func DialPrimary() (net.Conn, error) {
	return net.Dial("unix", SocketPath())
}

func DialPrimaryURL() string {
	return "http://unix/mcp"
}
