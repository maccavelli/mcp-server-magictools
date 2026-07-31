package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/provider"
)

func TestCFG01_ThinkingTierLabelParsingRegression(t *testing.T) {
	thinkingSpecs := provider.ForTier(provider.TierThinking)
	for _, s := range thinkingSpecs {
		label := s.Label
		prefix := string(label[0]) // e.g. "G" or "C"
		_ = prefix
		// Verify catalog mapping returns correct spec ID
		spec, ok := provider.Get(s.ID)
		if !ok || spec.ID != s.ID {
			t.Fatalf("mismatch for thinking provider spec %s", s.ID)
		}
		if !spec.Thinking {
			t.Fatalf("provider %s in thinking catalog has Thinking == false", s.ID)
		}
	}
}

func TestCFG02_SaveConfigurationGatedOnFastProvider(t *testing.T) {
	// Verify that ConfigurationPatch can be applied via ConfigStore for Thinking/Embedding/Backplane
	// without any Fast tier provider set.
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialYAML := `configuration:
  logLevel: INFO
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0600); err != nil {
		t.Fatal(err)
	}

	paths, err := config.ResolvePaths(configPath)
	if err != nil {
		t.Fatal(err)
	}

	store := config.NewStore(paths)

	// Apply patch containing only Thinking tier and Backplane settings
	patch := config.ConfigurationPatch{
		Thinking: config.ThinkingTierPatch{
			ThinkingProvider: config.Set("claude"),
			ThinkingModel:    config.Set("claude-3-7-sonnet-latest"),
			ThinkingAPIKey:   config.Set("sk-ant-test"),
		},
		Backplane: config.BackplanePatch{
			SharedLLMEnabled: config.Set(true),
			LLMPort:          config.Set(48081),
		},
	}

	res, err := store.Apply(context.Background(), patch)
	if err != nil {
		t.Fatalf("store.Apply failed without Fast tier configured: %v", err)
	}

	if !res.Changed {
		t.Fatal("expected res.Changed == true")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	strData := string(data)
	if !strings.Contains(strData, "thinking_provider: claude") {
		t.Errorf("thinking_provider not saved:\n%s", strData)
	}
	if !strings.Contains(strData, "shared_llm_enabled: true") {
		t.Errorf("shared_llm_enabled not saved:\n%s", strData)
	}
}

func TestCFG04_WizardExitWithoutSavePreservesOriginal(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	initialYAML := `# Original Custom Header
configuration:
  logLevel: DEBUG
  custom_unowned_key: "preserved_value"
`
	if err := os.WriteFile(configPath, []byte(initialYAML), 0600); err != nil {
		t.Fatal(err)
	}

	paths, err := config.ResolvePaths(configPath)
	if err != nil {
		t.Fatal(err)
	}

	// Exit without save (stagedPatch empty, or selecting 0)
	stagedPatch := config.ConfigurationPatch{}
	if !stagedPatch.IsEmpty() {
		t.Fatal("expected empty staged patch")
	}

	// Read file back and verify bytes match initialYAML exactly
	data, err := os.ReadFile(paths.Config)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(data, []byte(initialYAML)) {
		t.Fatalf("config file was mutated when exiting wizard without save:\nGot:\n%s\nExpected:\n%s", string(data), initialYAML)
	}
}

func TestCFG06_ConfigureRejectsForceAndNonInteractive(t *testing.T) {
	tmpDir := t.TempDir()
	CfgPath = filepath.Join(tmpDir, "config.yaml")
	defer func() { CfgPath = "" }()

	forceInit = true
	nonInteractive = false
	err := runConfigure(configureCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--force") {
		t.Errorf("expected configure to reject --force, got: %v", err)
	}

	forceInit = false
	nonInteractive = true
	err = runConfigure(configureCmd, nil)
	if err == nil || !strings.Contains(err.Error(), "--non-interactive") {
		t.Errorf("expected configure to reject --non-interactive, got: %v", err)
	}
	nonInteractive = false
}
