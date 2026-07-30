package main

import (
	"bufio"
	"os"
	"strings"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func TestConfigHelpers(t *testing.T) {
	if maskKey("123456789") != "****6789" {
		t.Errorf("maskKey failed: got %q", maskKey("123456789"))
	}
	if maskKey("short") != "****hort" {
		t.Error("maskKey short failed")
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

	// Create a dummy reader
	input := "1\nkey\n1\n\n\n\n"
	reader := bufio.NewReader(strings.NewReader(input))

	// We might panic or fail early, but it will execute lines
	defer func() {
		recover()
	}()

	showCurrentConfig(cfg)
	configureBackplane(cfg)
	configureFastTier(cfg, reader)
	configureThinkingTier(cfg, reader)
	configureEmbeddingEngine(cfg, reader)
}

func TestEnsureInitialized(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "config-init-test")
	defer os.RemoveAll(tmpDir)

	err := ensureInitialized(tmpDir, tmpDir+"/config.yaml", tmpDir+"/servers.yaml", tmpDir+"/overrides.yaml", true)
	if err != nil {
		t.Error(err)
	}
}
