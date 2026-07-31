package config

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"

	"github.com/spf13/viper"
	"gopkg.in/yaml.v3"
)

// ApplyResult contains auditing and activation metadata after a transaction.
type ApplyResult struct {
	Changed         bool
	ChangedPaths    []string
	OldHash         string
	NewHash         string
	RestartRequired bool
}

// ConfigStore handles locked, atomic, selective configuration AST mutations.
type ConfigStore struct {
	Paths Paths
}

// NewStore creates a ConfigStore instance bound to resolved paths.
func NewStore(paths Paths) *ConfigStore {
	return &ConfigStore{Paths: paths}
}

// Apply executes an atomic, locked, selective patch transaction against the config file.
func (s *ConfigStore) Apply(ctx context.Context, patch ConfigurationPatch) (ApplyResult, error) {
	if s == nil || s.Paths.Config == "" {
		return ApplyResult{}, fmt.Errorf("invalid config store target")
	}

	lockPath := s.Paths.Config + ".lock"
	fl, err := lockFile(lockPath)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("acquire config lock failed: %w", err)
	}
	defer func() {
		if fl != nil {
			_ = fl.unlock() //nolint:errcheck // best-effort lock release
		}
	}()

	existingData, err := os.ReadFile(s.Paths.Config)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("read existing config failed: %w", err)
	}

	oldHashHex := fmt.Sprintf("%x", sha256.Sum256(existingData))

	var doc yaml.Node
	if err := yaml.Unmarshal(existingData, &doc); err != nil {
		return ApplyResult{}, fmt.Errorf("parse existing YAML failed: %w", err)
	}

	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return ApplyResult{}, fmt.Errorf("existing config is not a valid YAML mapping")
	}

	if patch.IsEmpty() {
		return ApplyResult{
			Changed: false,
			OldHash: oldHashHex,
			NewHash: oldHashHex,
		}, nil
	}

	changedPaths, err := applyPatchToAST(&doc, patch)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("apply patch to AST failed: %w", err)
	}

	newData, err := yaml.Marshal(&doc)
	if err != nil {
		return ApplyResult{}, fmt.Errorf("marshal candidate YAML failed: %w", err)
	}

	newHashHex := fmt.Sprintf("%x", sha256.Sum256(newData))

	if bytes.Equal(newData, existingData) {
		return ApplyResult{
			Changed: false,
			OldHash: oldHashHex,
			NewHash: newHashHex,
		}, nil
	}

	// Validate candidate using pure DecodeConfiguration
	if _, err := DecodeConfiguration(newData); err != nil {
		return ApplyResult{}, fmt.Errorf("validate candidate failed: %w", err)
	}

	// Atomic write
	if err := atomicWriteFile(s.Paths.Config, newData); err != nil {
		return ApplyResult{}, fmt.Errorf("commit config atomically failed: %w", err)
	}

	return ApplyResult{
		Changed:         true,
		ChangedPaths:    changedPaths,
		OldHash:         oldHashHex,
		NewHash:         newHashHex,
		RestartRequired: true,
	}, nil
}

// DecodeConfiguration is a pure configuration decoder that unmarshals and validates
// candidate YAML bytes without side effects (no file generation or IDE migration).
func DecodeConfiguration(data []byte) (*Config, error) {
	v := viper.New()
	v.SetConfigType("yaml")
	if err := v.ReadConfig(bytes.NewReader(data)); err != nil {
		return nil, fmt.Errorf("invalid YAML syntax: %w", err)
	}

	var ide IDEConfig
	if err := v.Unmarshal(&ide); err != nil {
		return nil, fmt.Errorf("failed to unmarshal candidate configuration: %w", err)
	}

	cfg := &Config{
		MaxResponseTokens: ide.Configuration.MaxResponseTokens,
		SqueezeLevelState: ide.Configuration.SqueezeLevel,
		LogFormat:         ide.Configuration.LogFormat,
		LogLevel:          ide.Configuration.LogLevel,
		MCPLogLevel:       ide.Configuration.MCPLogLevel,
		Intelligence:      ide.Configuration.Intelligence,
	}

	return cfg, nil
}
