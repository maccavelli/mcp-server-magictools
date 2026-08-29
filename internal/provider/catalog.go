// Package provider holds MagicTools' provider tier model: which providers may
// serve which tier, and the embedding-specific data mcplib does not model.
//
// Provider identity — id, label, environment variable, locality, endpoint
// support and the generative model catalog — is DERIVED from
// llmprovider.Descriptors(). It used to be duplicated here, which is how this
// catalog came to recommend gemini-2.0-flash after it was shut down and to omit
// Grok entirely. See MADR 0004.
package provider

import "github.com/maccavelli/mcplib/llmprovider"

// Tier is a MagicTools concept: the same provider may serve different roles
// with different models. llmprovider knows nothing about tiers.
type Tier string

// Provider tiers.
const (
	TierFast      Tier = "fast"
	TierThinking  Tier = "thinking"
	TierEmbedding Tier = "embedding"
)

// Canonical provider ids. The generative ones alias llmprovider's constants so
// there is one spelling per provider across both modules.
const (
	ProviderGemini      = llmprovider.ProviderGemini
	ProviderClaude      = llmprovider.ProviderClaude
	ProviderOpenAI      = llmprovider.ProviderOpenAI
	ProviderGrok        = llmprovider.ProviderGrok
	ProviderOpencodeZen = llmprovider.ProviderOpencodeZen
	ProviderOpencodeGo  = llmprovider.ProviderOpencodeGo
	ProviderHuggingFace = llmprovider.ProviderHuggingFace
	ProviderKilo        = llmprovider.ProviderKilo
	ProviderOllama      = llmprovider.ProviderOllama

	// ProviderVoyage is embedding-only and has no llmprovider descriptor:
	// llmprovider has no embedding abstraction, and adding one is its own
	// decision record. It is therefore defined and described entirely here.
	ProviderVoyage = "voyage"
)

// ProviderSpec is what the configuration wizard needs about one provider.
// Identity fields are populated from the matching ProviderDescriptor; the tier
// flags and the embedding data are MagicTools' own.
type ProviderSpec struct {
	ID              string
	Label           string
	Fast            bool
	Thinking        bool
	Embedding       bool
	EnvVar          string
	IsLocal         bool
	SupportsBaseURL bool
	// StaticModels carries the embedding catalog only. Fast and thinking
	// models come from the descriptor, so a model retired in mcplib cannot
	// still be recommended here.
	StaticModels map[Tier][]string
	// Dimensions maps an embedding model's display label to its vector width.
	Dimensions map[string]int
}

// tierSpec holds only what a descriptor cannot supply. Order is menu order.
var tierSpecs = []struct {
	id                        string
	fast, thinking, embedding bool
	embeddingModels           []string
	dimensions                map[string]int
	label, envVar             string
	isLocal, supportsBaseURL  bool
}{
	{
		id: ProviderGemini, fast: true, thinking: true, embedding: true,
		embeddingModels: []string{
			"gemini-embedding-2 (768 dims)",
			"text-embedding-005 (768 dims)",
			"text-embedding-004 (768 dims)",
			"text-embedding-004 (256 dims)",
		},
		dimensions: map[string]int{
			"gemini-embedding-2 (768 dims)": 768,
			"text-embedding-005 (768 dims)": 768,
			"text-embedding-004 (768 dims)": 768,
			"text-embedding-004 (256 dims)": 256,
			"gemini-embedding-2-preview":    768,
		},
	},
	{id: ProviderClaude, fast: true, thinking: true},
	{
		id: ProviderOpenAI, fast: true, thinking: true, embedding: true,
		embeddingModels: []string{
			"text-embedding-3-small (512 dims)",
			"text-embedding-3-small (1536 dims)",
			"text-embedding-3-large (256 dims)",
			"text-embedding-3-large (1024 dims)",
		},
		dimensions: map[string]int{
			"text-embedding-3-small (512 dims)":  512,
			"text-embedding-3-small (1536 dims)": 1536,
			"text-embedding-3-large (256 dims)":  256,
			"text-embedding-3-large (1024 dims)": 1024,
			"text-embedding-3-small":             1536,
		},
	},
	{id: ProviderGrok, fast: true, thinking: true},
	{id: ProviderOpencodeZen, fast: true, thinking: true},
	{id: ProviderOpencodeGo, fast: true, thinking: true},
	{id: ProviderHuggingFace, fast: true, thinking: true},
	{id: ProviderKilo, fast: true, thinking: true},
	{
		// Ollama serves every tier since mcplib v1.2.0 promoted it from a
		// listing-only entry to a real Provider (MADR 0004 Phase 3).
		id: ProviderOllama, fast: true, thinking: true, embedding: true,
		embeddingModels: []string{
			"granite-embedding:30m", "snowflake-arctic-embed:33m",
			"all-minilm:33m", "nomic-embed-text",
		},
		dimensions: map[string]int{
			"granite-embedding:30m":      384,
			"snowflake-arctic-embed:33m": 384,
			"all-minilm:33m":             384,
			"nomic-embed-text":           768,
		},
	},
	{
		// No descriptor: embedding-only, so its identity is stated here.
		id: ProviderVoyage, embedding: true,
		label: "Voyage (Claude Embeddings via Voyage API)", envVar: "VOYAGE_API_KEY",
		embeddingModels: []string{"voyage-3-lite", "voyage-3", "voyage-code-3"},
		dimensions: map[string]int{
			"voyage-3-lite": 512,
			"voyage-3":      1024,
			"voyage-code-3": 1024,
		},
	},
}

// specs builds the catalog, merging each tier spec with its descriptor. It is
// computed per call so a caller mutating a returned map cannot corrupt it.
func specs() []ProviderSpec {
	out := make([]ProviderSpec, 0, len(tierSpecs))
	for _, t := range tierSpecs {
		s := ProviderSpec{
			ID:              t.id,
			Label:           t.label,
			Fast:            t.fast,
			Thinking:        t.thinking,
			Embedding:       t.embedding,
			EnvVar:          t.envVar,
			IsLocal:         t.isLocal,
			SupportsBaseURL: t.supportsBaseURL,
		}
		if d, ok := llmprovider.DescriptorFor(t.id); ok {
			s.Label = d.Label
			s.EnvVar = d.EnvVar
			s.IsLocal = d.IsLocal
			s.SupportsBaseURL = d.SupportsBaseURL
		}
		if len(t.embeddingModels) > 0 {
			s.StaticModels = map[Tier][]string{
				TierEmbedding: append([]string(nil), t.embeddingModels...),
			}
		}
		if len(t.dimensions) > 0 {
			s.Dimensions = make(map[string]int, len(t.dimensions))
			for k, v := range t.dimensions {
				s.Dimensions[k] = v
			}
		}
		out = append(out, s)
	}
	return out
}

// Get returns the ProviderSpec for a provider ID.
func Get(id string) (ProviderSpec, bool) {
	for _, s := range specs() {
		if s.ID == id {
			return s, true
		}
	}
	return ProviderSpec{}, false
}

// ForTier returns all provider specs that support the given tier, in menu order.
func ForTier(tier Tier) []ProviderSpec {
	var result []ProviderSpec
	for _, s := range specs() {
		switch tier {
		case TierFast:
			if s.Fast {
				result = append(result, s)
			}
		case TierThinking:
			if s.Thinking {
				result = append(result, s)
			}
		case TierEmbedding:
			if s.Embedding {
				result = append(result, s)
			}
		}
	}
	return result
}

// GenerativeModels returns the models to offer for a generative tier. They come
// from mcplib's curated catalog, so a model retired there disappears here.
func GenerativeModels(id string) []string {
	d, ok := llmprovider.DescriptorFor(id)
	if !ok {
		return nil
	}
	return d.StaticModels
}
