package vector

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func TestInitGlobalEngineExtra(t *testing.T) {
	// Initialize with nil config or empty embedder
	c, _ := config.New("", "")
	InitGlobalEngine(t.TempDir(), c)

	if GetEngine() == nil {
		t.Error("Expected engine to be initialized")
	}

	keys := GetEngine().Keys()
	if len(keys) != 0 {
		t.Errorf("Expected 0 keys, got %d", len(keys))
	}

	validKeys := make(map[string]bool)
	GetEngine().PruneOrphanedNodes(validKeys)
	computeSentinelHash("provider", "model", 100, "url")
}

func TestEmbeddersExtra(t *testing.T) {
	cfg, _ := config.New("", "")

	// Test Ollama
	cfg.Intelligence.EmbeddingProvider = "ollama"
	cfg.Intelligence.EmbeddingModel = "test"
	cfg.Intelligence.EmbeddingAPIURL = "http://localhost:11434"
	emb := NewEmbedderFromConfig(cfg)
	if emb == nil || emb.Provider() != "ollama" {
		t.Error("expected ollama")
	}

	// Test Gemini
	cfg.Intelligence.EmbeddingProvider = "gemini"
	cfg.Intelligence.EmbeddingAPIKey = "test-key"
	emb = NewEmbedderFromConfig(cfg)
	if emb == nil || emb.Provider() != "gemini" {
		t.Error("expected gemini")
	}

	// Test OpenAI
	cfg.Intelligence.EmbeddingProvider = "openai"
	emb = NewEmbedderFromConfig(cfg)
	if emb == nil || emb.Provider() != "openai" {
		t.Error("expected openai")
	}

	// Test Voyage
	cfg.Intelligence.EmbeddingProvider = "voyage"
	emb = NewEmbedderFromConfig(cfg)
	if emb == nil || emb.Provider() != "voyage" {
		t.Error("expected voyage")
	}
}
