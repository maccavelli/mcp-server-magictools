package provider

type Tier string

const (
	TierFast      Tier = "fast"
	TierThinking  Tier = "thinking"
	TierEmbedding Tier = "embedding"
)

const (
	ProviderGemini = "gemini"
	ProviderClaude = "claude"
	ProviderOpenAI = "openai"
	ProviderVoyage = "voyage"
	ProviderOllama = "ollama"
)

type ProviderSpec struct {
	ID              string
	Label           string
	Fast            bool
	Thinking        bool
	Embedding       bool
	EnvVar          string
	IsLocal         bool
	SupportsBaseURL bool
	StaticModels    map[Tier][]string
	Dimensions      map[string]int
}

var catalog = map[string]ProviderSpec{
	ProviderGemini: {
		ID:              ProviderGemini,
		Label:           "Gemini (Google Gemini API)",
		Fast:            true,
		Thinking:        true,
		Embedding:       true,
		EnvVar:          "GEMINI_API_KEY",
		IsLocal:         false,
		SupportsBaseURL: false,
		StaticModels: map[Tier][]string{
			TierFast:     {"gemini-2.5-flash", "gemini-2.5-pro", "gemini-2.0-flash"},
			TierThinking: {"gemini-2.5-pro", "gemini-2.5-flash"},
			TierEmbedding: {
				"gemini-embedding-2 (768 dims)",
				"text-embedding-005 (768 dims)",
				"text-embedding-004 (768 dims)",
				"text-embedding-004 (256 dims)",
			},
		},
		Dimensions: map[string]int{
			"gemini-embedding-2 (768 dims)": 768,
			"text-embedding-005 (768 dims)": 768,
			"text-embedding-004 (768 dims)": 768,
			"text-embedding-004 (256 dims)": 256,
			"gemini-embedding-2-preview":    768,
		},
	},
	ProviderClaude: {
		ID:              ProviderClaude,
		Label:           "Claude (Anthropic Claude API)",
		Fast:            true,
		Thinking:        true,
		Embedding:       false,
		EnvVar:          "CLAUDE_API_KEY",
		IsLocal:         false,
		SupportsBaseURL: false,
		StaticModels: map[Tier][]string{
			TierFast:     {"claude-3-5-sonnet-latest", "claude-3-5-haiku-latest", "claude-3-opus-latest"},
			TierThinking: {"claude-3-7-sonnet-latest", "claude-3-5-sonnet-latest"},
		},
	},
	ProviderOpenAI: {
		ID:              ProviderOpenAI,
		Label:           "OpenAI (OpenAI API)",
		Fast:            true,
		Thinking:        true,
		Embedding:       true,
		EnvVar:          "OPENAI_API_KEY",
		IsLocal:         false,
		SupportsBaseURL: false,
		StaticModels: map[Tier][]string{
			TierFast:     {"gpt-4o", "gpt-4o-mini", "o3-mini"},
			TierThinking: {"o3-mini", "o1", "gpt-4o"},
			TierEmbedding: {
				"text-embedding-3-small (512 dims)",
				"text-embedding-3-small (1536 dims)",
				"text-embedding-3-large (256 dims)",
				"text-embedding-3-large (1024 dims)",
			},
		},
		Dimensions: map[string]int{
			"text-embedding-3-small (512 dims)":  512,
			"text-embedding-3-small (1536 dims)": 1536,
			"text-embedding-3-large (256 dims)":  256,
			"text-embedding-3-large (1024 dims)": 1024,
			"text-embedding-3-small":             1536,
		},
	},
	ProviderVoyage: {
		ID:              ProviderVoyage,
		Label:           "Voyage (Claude Embeddings via Voyage API)",
		Fast:            false,
		Thinking:        false,
		Embedding:       true,
		EnvVar:          "VOYAGE_API_KEY",
		IsLocal:         false,
		SupportsBaseURL: false,
		StaticModels: map[Tier][]string{
			TierEmbedding: {"voyage-3-lite", "voyage-3", "voyage-code-3"},
		},
		Dimensions: map[string]int{
			"voyage-3-lite": 512,
			"voyage-3":      1024,
			"voyage-code-3": 1024,
		},
	},
	ProviderOllama: {
		ID:              ProviderOllama,
		Label:           "Ollama (Local API)",
		Fast:            false,
		Thinking:        false,
		Embedding:       true,
		EnvVar:          "",
		IsLocal:         true,
		SupportsBaseURL: true,
		StaticModels: map[Tier][]string{
			TierEmbedding: {"granite-embedding:30m", "snowflake-arctic-embed:33m", "all-minilm:33m", "nomic-embed-text"},
		},
		Dimensions: map[string]int{
			"granite-embedding:30m":      384,
			"snowflake-arctic-embed:33m": 384,
			"all-minilm:33m":             384,
			"nomic-embed-text":           768,
		},
	},
}

// Get returns the ProviderSpec for a provider ID.
func Get(id string) (ProviderSpec, bool) {
	spec, ok := catalog[id]
	return spec, ok
}

// ForTier returns all provider specs that support the given tier.
func ForTier(tier Tier) []ProviderSpec {
	var result []ProviderSpec
	order := []string{ProviderGemini, ProviderClaude, ProviderOpenAI, ProviderVoyage, ProviderOllama}
	for _, id := range order {
		spec := catalog[id]
		switch tier {
		case TierFast:
			if spec.Fast {
				result = append(result, spec)
			}
		case TierThinking:
			if spec.Thinking {
				result = append(result, spec)
			}
		case TierEmbedding:
			if spec.Embedding {
				result = append(result, spec)
			}
		}
	}
	return result
}
