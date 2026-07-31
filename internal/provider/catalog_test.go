package provider

import "testing"

func TestCatalogForTier(t *testing.T) {
	fast := ForTier(TierFast)
	if len(fast) != 3 {
		t.Fatalf("expected 3 fast providers, got %d", len(fast))
	}
	for _, p := range fast {
		if p.ID == ProviderOllama || p.ID == ProviderVoyage {
			t.Errorf("unsupported generative provider %s present in Fast tier catalog", p.ID)
		}
	}

	thinking := ForTier(TierThinking)
	if len(thinking) != 3 {
		t.Fatalf("expected 3 thinking providers, got %d", len(thinking))
	}
	for _, p := range thinking {
		if p.ID == ProviderOllama || p.ID == ProviderVoyage {
			t.Errorf("unsupported generative provider %s present in Thinking tier catalog", p.ID)
		}
	}

	embedding := ForTier(TierEmbedding)
	if len(embedding) != 4 {
		t.Fatalf("expected 4 embedding providers, got %d", len(embedding))
	}
}
