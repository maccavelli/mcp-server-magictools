package client

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestHydrateDependencies(t *testing.T) {
	record := &db.ToolRecord{
		URN:         "test:tool",
		Description: "[REQUIRES: something]",
	}
	hydrateDependencies(record)
}

func TestHydrateRoleAndPhase(t *testing.T) {
	record := &db.ToolRecord{
		URN:         "test:tool",
		Description: "[PHASE: 1]",
	}
	hydrateRoleAndPhase(record)
}
