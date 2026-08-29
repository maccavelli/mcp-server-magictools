package provider

import (
	"strings"
	"testing"

	"github.com/maccavelli/mcplib/llmprovider"
)

// TestCatalog_CoversEveryDescriptor is the drift guard. This catalog kept its
// own copy of provider identity, which is how it came to omit Grok entirely and
// to recommend gemini-2.0-flash after it was shut down. Every generative
// provider mcplib registers must be offerable for both generative tiers.
func TestCatalog_CoversEveryDescriptor(t *testing.T) {
	descriptors := llmprovider.Descriptors()
	if len(descriptors) == 0 {
		t.Fatal("llmprovider.Descriptors() is empty")
	}

	for _, tier := range []Tier{TierFast, TierThinking} {
		offered := make(map[string]bool)
		for _, s := range ForTier(tier) {
			offered[s.ID] = true
		}
		for _, d := range descriptors {
			if !offered[d.ID] {
				t.Errorf("%s tier does not offer descriptor %q", tier, d.ID)
			}
		}
	}
}

// TestCatalog_DerivesIdentityFromDescriptors asserts the identity fields are
// taken from mcplib rather than restated here, so they cannot drift.
func TestCatalog_DerivesIdentityFromDescriptors(t *testing.T) {
	for _, d := range llmprovider.Descriptors() {
		spec, ok := Get(d.ID)
		if !ok {
			t.Errorf("no spec for descriptor %q", d.ID)
			continue
		}
		if spec.Label != d.Label {
			t.Errorf("%s: Label = %q, want %q", d.ID, spec.Label, d.Label)
		}
		if spec.EnvVar != d.EnvVar {
			t.Errorf("%s: EnvVar = %q, want %q", d.ID, spec.EnvVar, d.EnvVar)
		}
		if spec.IsLocal != d.IsLocal {
			t.Errorf("%s: IsLocal = %v, want %v", d.ID, spec.IsLocal, d.IsLocal)
		}
		if spec.SupportsBaseURL != d.SupportsBaseURL {
			t.Errorf("%s: SupportsBaseURL = %v, want %v", d.ID, spec.SupportsBaseURL, d.SupportsBaseURL)
		}
	}
}

// TestCatalog_NoShutDownModels is the direct regression for the bug MADR 0004
// was written around: this catalog recommended gemini-2.0-flash for months
// after Google shut it down, because the model list was a local copy.
func TestCatalog_NoShutDownModels(t *testing.T) {
	retired := []string{"gemini-2.0-", "gemini-1.5-"}

	for _, tier := range []Tier{TierFast, TierThinking} {
		for _, s := range ForTier(tier) {
			for _, m := range GenerativeModels(s.ID) {
				for _, bad := range retired {
					if strings.Contains(m, bad) {
						t.Errorf("%s tier offers retired model %q for %s", tier, m, s.ID)
					}
				}
			}
		}
	}

	for _, s := range ForTier(TierEmbedding) {
		for _, m := range s.StaticModels[TierEmbedding] {
			for _, bad := range retired {
				if strings.Contains(m, bad) {
					t.Errorf("embedding tier offers retired model %q for %s", m, s.ID)
				}
			}
		}
	}
}

// TestCatalog_VoyageStaysLocal guards the scope boundary: Voyage is an
// embedding-only provider llmprovider does not model, so it must be offered for
// embeddings and never for a tier that runs a generation.
func TestCatalog_VoyageStaysLocal(t *testing.T) {
	spec, ok := Get(ProviderVoyage)
	if !ok {
		t.Fatal("Voyage missing from the catalog")
	}
	if spec.Fast || spec.Thinking {
		t.Error("Voyage must not serve a generative tier: it has no llmprovider.Provider")
	}
	if !spec.Embedding {
		t.Error("Voyage must serve the embedding tier")
	}
	if spec.Label == "" || spec.EnvVar == "" {
		t.Error("Voyage has no descriptor, so its identity must be stated locally")
	}
	if _, ok := llmprovider.DescriptorFor(ProviderVoyage); ok {
		t.Error("Voyage now has a descriptor; move it off the local identity path")
	}
}

// TestCatalog_EmbeddingModelsHaveDimensions keeps the vector index sizeable:
// every offered embedding model must map to a width.
func TestCatalog_EmbeddingModelsHaveDimensions(t *testing.T) {
	for _, s := range ForTier(TierEmbedding) {
		models := s.StaticModels[TierEmbedding]
		if len(models) == 0 {
			t.Errorf("%s serves the embedding tier with no models", s.ID)
		}
		for _, m := range models {
			if s.Dimensions[m] == 0 {
				t.Errorf("%s: embedding model %q has no dimensionality", s.ID, m)
			}
		}
	}
}

// TestCatalog_ReturnsDefensiveCopies asserts a caller cannot corrupt the
// catalog for the next caller.
func TestCatalog_ReturnsDefensiveCopies(t *testing.T) {
	first, ok := Get(ProviderGemini)
	if !ok {
		t.Fatal("gemini missing from the catalog")
	}
	first.Dimensions["gemini-embedding-2 (768 dims)"] = 1
	first.StaticModels[TierEmbedding][0] = "corrupted"

	second, _ := Get(ProviderGemini)
	if second.Dimensions["gemini-embedding-2 (768 dims)"] == 1 {
		t.Error("Dimensions is shared between callers")
	}
	if second.StaticModels[TierEmbedding][0] == "corrupted" {
		t.Error("StaticModels is shared between callers")
	}
}
