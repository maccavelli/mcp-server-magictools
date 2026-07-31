package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Paths holds the canonical resolved absolute paths for all MagicTools configuration files.
type Paths struct {
	Dir       string
	Config    string
	Servers   string
	Overrides string
}

// ResolvePaths resolves configuration file paths according to the following precedence:
// 1. Explicit flagPath argument (from --config)
// 2. Environment variable MCP_MAGIC_TOOLS_CONFIG
// 3. Environment variable MCP_MAGIC_TOOLS_CONFIG_DIR or OS default config dir
//
// For a resolved YAML config path, companion files (servers.yaml and tool_overrides.yaml)
// reside in the same parent directory as the primary config file.
// For legacy JSON configs, companion files remain in DefaultConfigDir().
// Returns an error if a directory path is passed where a file is required.
func ResolvePaths(flagPath string) (Paths, error) {
	var targetConfig string

	if flagPath != "" {
		targetConfig = flagPath
	} else if envPath := os.Getenv(EnvConfigPath); envPath != "" {
		targetConfig = envPath
	}

	if targetConfig != "" {
		absPath, err := filepath.Abs(targetConfig)
		if err != nil {
			return Paths{}, fmt.Errorf("invalid config path %q: %w", targetConfig, err)
		}
		absPath = filepath.Clean(absPath)

		info, err := os.Stat(absPath)
		if err == nil && info.IsDir() {
			return Paths{}, fmt.Errorf("config path %q is a directory, expected a file", absPath)
		}

		if strings.HasSuffix(absPath, ".json") {
			defaultDir := DefaultConfigDir()
			absDefaultDir, err := filepath.Abs(defaultDir)
			if err != nil {
				absDefaultDir = defaultDir
			}
			absDefaultDir = filepath.Clean(absDefaultDir)
			return Paths{
				Dir:       absDefaultDir,
				Config:    absPath,
				Servers:   filepath.Join(absDefaultDir, ServersConfigFile),
				Overrides: filepath.Join(absDefaultDir, "tool_overrides.yaml"),
			}, nil
		}

		dir := filepath.Dir(absPath)
		return Paths{
			Dir:       dir,
			Config:    absPath,
			Servers:   filepath.Join(dir, ServersConfigFile),
			Overrides: filepath.Join(dir, "tool_overrides.yaml"),
		}, nil
	}

	dir := DefaultConfigDir()
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return Paths{}, fmt.Errorf("failed to resolve default config dir: %w", err)
	}
	absDir = filepath.Clean(absDir)

	return Paths{
		Dir:       absDir,
		Config:    filepath.Join(absDir, ToolConfigFile),
		Servers:   filepath.Join(absDir, ServersConfigFile),
		Overrides: filepath.Join(absDir, "tool_overrides.yaml"),
	}, nil
}
