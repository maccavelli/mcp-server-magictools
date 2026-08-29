package main

import (
	"strings"
	"testing"

	"github.com/maccavelli/mcplib/llmprovider"
	"github.com/maccavelli/mcplib/wizard"

	"github.com/maccavelli/mcp-server-magictools/internal/provider"
)

func TestOptionText_RoundTripsIndex(t *testing.T) {
	choices := []wizard.Choice{
		{Label: "Gemini (Google)"},
		{Label: "Kilo Gateway", Detail: "free models available"},
		{Label: "Gemini (Google)"}, // a duplicate label must still resolve
	}
	for i, c := range choices {
		text := optionText(i, c)
		got, ok := optionIndex(text)
		if !ok {
			t.Fatalf("optionIndex(%q) failed", text)
		}
		if got != i {
			t.Errorf("optionIndex(%q) = %d, want %d", text, got, i)
		}
		if !strings.Contains(text, c.Label) {
			t.Errorf("rendered %q omits label %q", text, c.Label)
		}
	}
}

func TestOptionIndex_RejectsUnrecognised(t *testing.T) {
	for _, text := range []string{"", "Gemini", ".", "0) None/Clear", "x. Gemini"} {
		if _, ok := optionIndex(text); ok {
			t.Errorf("optionIndex(%q) unexpectedly succeeded", text)
		}
	}
}

// TestConfigure_OffersEveryDescriptorPerTier is the drift guard. Each
// generative tier's menu must be exactly the descriptors mcplib registers: this
// wizard kept its own provider list, which is why Grok was never offered and
// gemini-2.0-flash was recommended after it was shut down.
func TestConfigure_OffersEveryDescriptorPerTier(t *testing.T) {
	descriptors := llmprovider.Descriptors()
	if len(descriptors) == 0 {
		t.Fatal("llmprovider.Descriptors() is empty")
	}

	for _, tier := range []provider.Tier{provider.TierFast, provider.TierThinking} {
		var ids []string
		for _, spec := range provider.ForTier(tier) {
			if _, ok := llmprovider.DescriptorFor(spec.ID); ok {
				ids = append(ids, spec.ID)
			}
		}
		if len(ids) != len(descriptors) {
			t.Errorf("%s tier offers %d providers, want %d", tier, len(ids), len(descriptors))
		}
		offered := make(map[string]bool, len(ids))
		for _, id := range ids {
			offered[id] = true
		}
		for _, d := range descriptors {
			if !offered[d.ID] {
				t.Errorf("%s tier menu omits %q", tier, d.ID)
			}
		}
	}
}

// TestConfigure_EmbeddingTierStaysLocal guards the scope boundary from PLAN
// 0004 deviation D7: the embedding menu is MagicTools' own, because llmprovider
// has no embedding abstraction and Voyage therefore has no descriptor.
func TestConfigure_EmbeddingTierStaysLocal(t *testing.T) {
	var sawVoyage bool
	for _, spec := range provider.ForTier(provider.TierEmbedding) {
		if spec.ID == provider.ProviderVoyage {
			sawVoyage = true
		}
	}
	if !sawVoyage {
		t.Error("embedding menu must still offer Voyage, which has no descriptor")
	}
}

// TestStaticModelsForProvider_ComesFromMcplib asserts the fallback catalog is
// no longer a local copy.
func TestStaticModelsForProvider_ComesFromMcplib(t *testing.T) {
	for _, d := range llmprovider.Descriptors() {
		got := staticModelsForProvider(d.ID)
		want := llmprovider.StaticModels(d.ID)
		if len(got) != len(want) {
			t.Errorf("%s: got %d models, want %d", d.ID, len(got), len(want))
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: model[%d] = %q, want %q", d.ID, i, got[i], want[i])
			}
		}
	}
	if models := staticModelsForProvider(provider.ProviderVoyage); models != nil {
		t.Errorf("Voyage has no generative catalog, got %v", models)
	}
}
