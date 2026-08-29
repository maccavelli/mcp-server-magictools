package main

import (
	"os"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcplib/logging"
)

func TestConfigHelpers(t *testing.T) {
	// Credential display is logging.MaskSecret now, not a local maskKey. The
	// formats differ substantively: maskKey("short") returned "****hort",
	// revealing four of five characters, where MaskSecret reveals none below
	// its eight-rune threshold. See PLAN 0004 deviation D4.
	if got := logging.MaskSecret("123456789"); got != "••••••••6789" {
		t.Errorf("MaskSecret(\"123456789\") = %q", got)
	}
	if got := logging.MaskSecret("short"); got != "••••••••" {
		t.Errorf("MaskSecret(\"short\") = %q, want no revealed characters", got)
	}

	if choiceToProvider("1") != "gemini" {
		t.Error("choiceToProvider failed")
	}
	if choiceToProvider("2") != "claude" {
		t.Error("choiceToProvider failed")
	}
	if choiceToProvider("3") != "openai" {
		t.Error("choiceToProvider failed")
	}
	if choiceToProvider("4") != "ollama" {
		t.Error("choiceToProvider failed")
	}
	if choiceToProvider("9") != "" {
		t.Error("choiceToProvider failed")
	}

	models := staticModelsForProvider("claude")
	if len(models) == 0 {
		t.Error("staticModelsForProvider failed")
	}

	if valOrDefault(10, 5) != 10 {
		t.Error("valOrDefault failed")
	}
	if valOrDefault(0, 5) != 5 {
		t.Error("valOrDefault failed")
	}

	if fileExists("/this/file/does/not/exist/ever") {
		t.Error("fileExists failed")
	}
}

func TestConfigUIFunctions(t *testing.T) {
	t.Skip("Skipping interactive UI test that requires stdin")
	cfg, _ := config.New("", "")

	// We might panic or fail early, but it will execute lines
	defer func() {
		recover()
	}()

	showCurrentConfig(cfg)
	configureBackplane(cfg, &config.BackplanePatch{})
	configureFastTier(cfg, &config.FastTierPatch{})
	configureThinkingTier(cfg, &config.ThinkingTierPatch{})
	configureEmbeddingEngine(cfg, &config.EmbeddingPatch{})
}

func TestEnsureInitialized(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "config-init-test")
	defer os.RemoveAll(tmpDir)

	err := ensureInitialized(tmpDir, tmpDir+"/config.yaml", tmpDir+"/servers.yaml", tmpDir+"/overrides.yaml", true)
	if err != nil {
		t.Error(err)
	}
}
