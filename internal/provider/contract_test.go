package provider

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
	"github.com/maccavelli/mcplib/llmprovider"
)

func TestAdvertisedGenerativeProvidersConstruct(t *testing.T) {
	fastProviders := ForTier(TierFast)
	for _, spec := range fastProviders {
		p, err := llmprovider.NewProvider(spec.ID, "dummy-key-for-contract-test", "dummy-model")
		if err != nil {
			t.Errorf("failed to construct advertised Fast provider %s: %v", spec.ID, err)
		}
		if p == nil {
			t.Errorf("nil provider returned for advertised Fast provider %s", spec.ID)
		}
	}

	thinkingProviders := ForTier(TierThinking)
	for _, spec := range thinkingProviders {
		p, err := llmprovider.NewProvider(spec.ID, "dummy-key-for-contract-test", "dummy-model")
		if err != nil {
			t.Errorf("failed to construct advertised Thinking provider %s: %v", spec.ID, err)
		}
		if p == nil {
			t.Errorf("nil provider returned for advertised Thinking provider %s", spec.ID)
		}
	}
}

func TestAdvertisedEmbeddersConstruct(t *testing.T) {
	embedders := ForTier(TierEmbedding)
	for _, spec := range embedders {
		cfg := &config.Config{
			Intelligence: config.IntelligenceEngine{
				EmbeddingProvider:       spec.ID,
				EmbeddingModel:          "dummy-model",
				EmbeddingAPIKey:         "dummy-key",
				EmbeddingDimensionality: 768,
				EmbeddingAPIURL:         "http://localhost:11434",
			},
		}

		emb := vector.NewEmbedderFromConfig(cfg)
		if emb == nil {
			t.Errorf("vector.NewEmbedderFromConfig returned nil for advertised embedding provider %s", spec.ID)
		} else if emb.Provider() != spec.ID {
			t.Errorf("expected provider %s, got %s", spec.ID, emb.Provider())
		}
	}
}

func TestUnadvertisedGenerativeProvidersRejected(t *testing.T) {
	// Generative Ollama and Voyage must return unsupported provider error from mcplib
	unsupported := []string{ProviderOllama, ProviderVoyage}
	for _, id := range unsupported {
		_, err := llmprovider.NewProvider(id, "dummy-key", "dummy-model")
		if err == nil {
			t.Errorf("expected error when constructing unsupported generative provider %s, got nil", id)
		}
	}
}
