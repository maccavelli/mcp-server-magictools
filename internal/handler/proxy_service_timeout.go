package handler

import (
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// ResolveTimeout computes the appropriate execution timeout for a proxy call
// based on the tool's configured timeout, its pipeline role, and default bounds.
func ResolveTimeout(toolRecord *db.ToolRecord) time.Duration {
	timeout := 120 * time.Second

	if toolRecord != nil && toolRecord.TimeoutSecs > 0 {
		timeout = time.Duration(toolRecord.TimeoutSecs) * time.Second
	}

	// MUTATOR role escalation (batch operations: ~90s/file)
	if toolRecord != nil && toolRecord.Role == roleMutator && timeout < 600*time.Second {
		timeout = 600 * time.Second
	}

	return timeout
}
