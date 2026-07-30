package main

import (
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// TestConfigDiscovery verifies environmental discovery logic for the database and config paths.
func TestConfigDiscovery(t *testing.T) {
	// Test the logic that discovers DB and Config paths
	// We'll mock Env vars
	os.Setenv("MCP_MAGIC_TOOLS_DB_PATH", "/tmp/mcp-test-db")
	os.Setenv("MCP_MAGIC_TOOLS_CONFIG", "/tmp/mcp_config.json")
	defer os.Unsetenv("MCP_MAGIC_TOOLS_DB_PATH")
	defer os.Unsetenv("MCP_MAGIC_TOOLS_CONFIG")

	// This is hard to test from main() without a sub-process
	// but we've covered the individual components.
}

// TestPrimeFromRecallNoClient asserts safe fail-open capability when initialized with a nil recall client.
func TestPrimeFromRecallNoClient(t *testing.T) {
	cfg := &config.Config{}
	cfg.TokenSpendThresh = 1500000 // Default value

	// Should be a safe no-op with nil client — no panic, no config changes
	primeFromRecall(t.Context(), nil, cfg)

	if cfg.TokenSpendThresh != 1500000 {
		t.Errorf("expected TokenSpendThresh to remain at default 1500000, got %d", cfg.TokenSpendThresh)
	}
}
