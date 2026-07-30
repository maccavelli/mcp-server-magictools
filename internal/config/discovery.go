// Package config provides functionality for the config subsystem.
package config

import (
	"fmt"
	"os"
	"path/filepath"
)

// DiscoverIDEConfig attempts to find the IDE's mcp_config.json in common locations.
// Probes both Linux (XDG) and macOS (~/Library/Application Support) paths.
func DiscoverIDEConfig() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home dir: %w", err)
	}

	paths := []string{
		// Gemini/Antigravity (universal)
		filepath.Join(home, ".gemini", "antigravity", "mcp_config.json"),
		// Linux: VS Code, Cursor, Claude
		filepath.Join(home, ".config", "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "mcp_config.json"),
		filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "mcp_config.json"),
		filepath.Join(home, ".config", "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "mcp_config.json"),
		filepath.Join(home, ".config", "Cursor", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "mcp_config.json"),
		filepath.Join(home, ".config", "Claude", "claude_desktop_config.json"),
		filepath.Join(home, ".claude-code", "mcp_config.json"),
	}

	// macOS: Application Support paths (these are no-ops on Linux since the paths won't exist)
	if configDir, err := os.UserConfigDir(); err == nil && configDir != filepath.Join(home, ".config") {
		paths = append(paths,
			filepath.Join(configDir, "Code", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "mcp_config.json"),
			filepath.Join(configDir, "Cursor", "User", "globalStorage", "saoudrizwan.claude-dev", "settings", "mcp_config.json"),
			filepath.Join(configDir, "Code", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "mcp_config.json"),
			filepath.Join(configDir, "Cursor", "User", "globalStorage", "rooveterinaryinc.roo-cline", "settings", "mcp_config.json"),
			filepath.Join(configDir, "Claude", "claude_desktop_config.json"),
		)
	}

	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}

	// Default fallback if none exists
	return filepath.Join(home, ".gemini", "antigravity", "mcp_config.json"), nil
}
