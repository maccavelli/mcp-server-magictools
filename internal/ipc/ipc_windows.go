//go:build windows

package ipc

import (
	"fmt"
	"net"
	"os/user"

	"github.com/Microsoft/go-winio"
)

func PipePath() string {
	u, err := user.Current()
	if err != nil {
		return `\\.\pipe\magictools_default`
	}
	// Use username to namespace the pipe
	return fmt.Sprintf(`\\.\pipe\magictools_%s`, u.Username)
}

func ListenPrimary() (net.Listener, error) {
	u, err := user.Current()
	if err != nil {
		return nil, fmt.Errorf("failed to get current user: %w", err)
	}

	// Dynamic User-SID SDDL injection for strict local security
	// D: = DACL
	// (A;;GA;;;SID) = Allow Generic All to the current user SID
	sddl := fmt.Sprintf("D:(A;;GA;;;%s)", u.Uid)

	config := &winio.PipeConfig{
		SecurityDescriptor: sddl,
		MessageMode:        false,
	}

	return winio.ListenPipe(PipePath(), config)
}

func DialPrimary() (net.Conn, error) {
	return winio.DialPipe(PipePath(), nil)
}

func DialPrimaryURL() string {
	return "http://pipe/mcp"
}

// SocketPath returns an empty string on Windows — Named Pipes are used instead.
// This stub exists for cross-platform compilation compatibility with cleanupServiceArtifacts.
func SocketPath() string {
	return ""
}
