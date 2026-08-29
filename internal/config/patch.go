package config

type PatchState uint8

const (
	PatchUnchanged PatchState = iota
	PatchSet
	PatchRemove
)

type Field[T any] struct {
	State PatchState
	Value T
}

func Unchanged[T any]() Field[T] {
	return Field[T]{State: PatchUnchanged}
}

func Set[T any](val T) Field[T] {
	return Field[T]{State: PatchSet, Value: val}
}

func Remove[T any]() Field[T] {
	return Field[T]{State: PatchRemove}
}

// FastTierPatch defines owned patch fields for the Fast Tier LLM block.
type FastTierPatch struct {
	Provider       Field[string]
	Model          Field[string]
	APIKey         Field[string]
	APIKeyEnv      Field[string]
	APIURL         Field[string]
	FallbackModels Field[[]string]
	RetryCount     Field[int]
	RetryDelay     Field[int]
	TimeoutSeconds Field[int]
}

// ThinkingTierPatch defines owned patch fields for the Thinking Tier LLM block.
type ThinkingTierPatch struct {
	ThinkingProvider  Field[string]
	ThinkingModel     Field[string]
	ThinkingAPIKey    Field[string]
	ThinkingAPIKeyEnv Field[string]
	ThinkingAPIURL    Field[string]
}

// EmbeddingPatch defines owned patch fields for the Embedding Engine block.
type EmbeddingPatch struct {
	EmbeddingProvider       Field[string]
	EmbeddingModel          Field[string]
	EmbeddingAPIKey         Field[string]
	EmbeddingAPIKeyEnv      Field[string]
	EmbeddingAPIURL         Field[string]
	EmbeddingDimensionality Field[int]
	VectorEnabled           Field[bool]
}

// BackplanePatch defines owned patch fields for the Shared LLM Backplane block.
type BackplanePatch struct {
	SharedLLMEnabled  Field[bool]
	LLMPort           Field[int]
	MaxConcurrent     Field[int]
	MaxRPM            Field[int]
	MaxBurstPerSecond Field[int]
	SubServerTokenMax Field[int]
	OrphanStreamTTL   Field[int]
}

// RuntimeConfigPatch defines owned patch fields for runtime configuration updates.
type RuntimeConfigPatch struct {
	LogLevel           Field[string]
	MCPLogLevel        Field[string]
	LogFormat          Field[string]
	SqueezeLevel       Field[int]
	ScoreThreshold     Field[float64]
	ConfidenceGap      Field[float64]
	ValidateProxyCalls Field[bool]
	PinnedServers      Field[[]string]
	TrustServers       Field[[]string]
	SqueezeBypass      Field[[]string]
	RingBufferTargets  Field[[]string]
	TokenSpendThresh   Field[int]
	LRULimit           Field[int]
}

// ConfigurationPatch aggregates all section-specific patches.
type ConfigurationPatch struct {
	Fast      FastTierPatch
	Thinking  ThinkingTierPatch
	Embedding EmbeddingPatch
	Backplane BackplanePatch
	Runtime   RuntimeConfigPatch
}

// IsEmpty returns true if all fields in all patches are PatchUnchanged.
func (cp ConfigurationPatch) IsEmpty() bool {
	return isFastEmpty(cp.Fast) &&
		isThinkingEmpty(cp.Thinking) &&
		isEmbeddingEmpty(cp.Embedding) &&
		isBackplaneEmpty(cp.Backplane) &&
		isRuntimeEmpty(cp.Runtime)
}

func isFastEmpty(p FastTierPatch) bool {
	return p.Provider.State == PatchUnchanged &&
		p.Model.State == PatchUnchanged &&
		p.APIKey.State == PatchUnchanged &&
		p.APIKeyEnv.State == PatchUnchanged &&
		p.APIURL.State == PatchUnchanged &&
		p.FallbackModels.State == PatchUnchanged &&
		p.RetryCount.State == PatchUnchanged &&
		p.RetryDelay.State == PatchUnchanged &&
		p.TimeoutSeconds.State == PatchUnchanged
}

func isThinkingEmpty(p ThinkingTierPatch) bool {
	return p.ThinkingProvider.State == PatchUnchanged &&
		p.ThinkingModel.State == PatchUnchanged &&
		p.ThinkingAPIKey.State == PatchUnchanged &&
		p.ThinkingAPIKeyEnv.State == PatchUnchanged
}

func isEmbeddingEmpty(p EmbeddingPatch) bool {
	return p.EmbeddingProvider.State == PatchUnchanged &&
		p.EmbeddingModel.State == PatchUnchanged &&
		p.EmbeddingAPIKey.State == PatchUnchanged &&
		p.EmbeddingAPIKeyEnv.State == PatchUnchanged &&
		p.EmbeddingAPIURL.State == PatchUnchanged &&
		p.EmbeddingDimensionality.State == PatchUnchanged &&
		p.VectorEnabled.State == PatchUnchanged
}

func isBackplaneEmpty(p BackplanePatch) bool {
	return p.SharedLLMEnabled.State == PatchUnchanged &&
		p.LLMPort.State == PatchUnchanged &&
		p.MaxConcurrent.State == PatchUnchanged &&
		p.MaxRPM.State == PatchUnchanged &&
		p.MaxBurstPerSecond.State == PatchUnchanged &&
		p.SubServerTokenMax.State == PatchUnchanged &&
		p.OrphanStreamTTL.State == PatchUnchanged
}

func isRuntimeEmpty(p RuntimeConfigPatch) bool {
	return p.LogLevel.State == PatchUnchanged &&
		p.MCPLogLevel.State == PatchUnchanged &&
		p.LogFormat.State == PatchUnchanged &&
		p.SqueezeLevel.State == PatchUnchanged &&
		p.ScoreThreshold.State == PatchUnchanged &&
		p.ConfidenceGap.State == PatchUnchanged &&
		p.ValidateProxyCalls.State == PatchUnchanged &&
		p.PinnedServers.State == PatchUnchanged &&
		p.TrustServers.State == PatchUnchanged &&
		p.SqueezeBypass.State == PatchUnchanged &&
		p.RingBufferTargets.State == PatchUnchanged &&
		p.TokenSpendThresh.State == PatchUnchanged &&
		p.LRULimit.State == PatchUnchanged
}
