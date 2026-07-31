package config

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// applyPatchToAST applies a ConfigurationPatch to a yaml.Node AST document in place,
// modifying only patch-owned keys and preserving comments/formatting for unowned content.
func applyPatchToAST(doc *yaml.Node, patch ConfigurationPatch) ([]string, error) {
	if doc == nil || doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 {
		return nil, fmt.Errorf("invalid YAML document node")
	}

	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("YAML root is not a mapping")
	}

	cfgNode := findOrCreateMapping(root, "configuration")
	var changedPaths []string

	// Apply Runtime patch
	r := patch.Runtime
	applyStringField(cfgNode, "logLevel", r.LogLevel, &changedPaths)
	applyStringField(cfgNode, "mcpLogLevel", r.MCPLogLevel, &changedPaths)
	applyStringField(cfgNode, "logFormat", r.LogFormat, &changedPaths)
	applyIntField(cfgNode, "squeezeLevel", r.SqueezeLevel, &changedPaths)
	applyFloatField(cfgNode, "scoreThreshold", r.ScoreThreshold, &changedPaths)
	applyFloatField(cfgNode, "confidenceGap", r.ConfidenceGap, &changedPaths)
	applyBoolField(cfgNode, "validateProxyCalls", r.ValidateProxyCalls, &changedPaths)
	applyStringSeqField(cfgNode, "pinnedServers", r.PinnedServers, &changedPaths)
	applyStringSeqField(cfgNode, "trustServers", r.TrustServers, &changedPaths)
	applyStringSeqField(cfgNode, "squeezeBypass", r.SqueezeBypass, &changedPaths)
	applyStringSeqField(cfgNode, "ringBufferTargets", r.RingBufferTargets, &changedPaths)
	applyIntField(cfgNode, "tokenSpendThresh", r.TokenSpendThresh, &changedPaths)
	applyIntField(cfgNode, "lruLimit", r.LRULimit, &changedPaths)

	// Check if intelligence block is touched
	if !isFastEmpty(patch.Fast) || !isThinkingEmpty(patch.Thinking) || !isEmbeddingEmpty(patch.Embedding) || !isBackplaneEmpty(patch.Backplane) {
		intelNode := findOrCreateMapping(cfgNode, "intelligence")

		// Fast Tier
		f := patch.Fast
		applyStringField(intelNode, "provider", f.Provider, &changedPaths)
		applyStringField(intelNode, "model", f.Model, &changedPaths)
		applyStringField(intelNode, "api_key", f.APIKey, &changedPaths)
		applyStringField(intelNode, "api_key_env", f.APIKeyEnv, &changedPaths)
		applyStringField(intelNode, "api_url", f.APIURL, &changedPaths)
		applyStringSeqField(intelNode, "fallback_models", f.FallbackModels, &changedPaths)
		applyIntField(intelNode, "retry_count", f.RetryCount, &changedPaths)
		applyIntField(intelNode, "retry_delay_seconds", f.RetryDelay, &changedPaths)
		applyIntField(intelNode, "timeout_seconds", f.TimeoutSeconds, &changedPaths)

		// Thinking Tier
		t := patch.Thinking
		applyStringField(intelNode, "thinking_provider", t.ThinkingProvider, &changedPaths)
		applyStringField(intelNode, "thinking_model", t.ThinkingModel, &changedPaths)
		applyStringField(intelNode, "thinking_api_key", t.ThinkingAPIKey, &changedPaths)
		applyStringField(intelNode, "thinking_api_key_env", t.ThinkingAPIKeyEnv, &changedPaths)

		// Embedding Engine
		e := patch.Embedding
		applyStringField(intelNode, "embedding_provider", e.EmbeddingProvider, &changedPaths)
		applyStringField(intelNode, "embedding_model", e.EmbeddingModel, &changedPaths)
		applyStringField(intelNode, "embedding_api_key", e.EmbeddingAPIKey, &changedPaths)
		applyStringField(intelNode, "embedding_api_key_env", e.EmbeddingAPIKeyEnv, &changedPaths)
		applyStringField(intelNode, "embedding_api_url", e.EmbeddingAPIURL, &changedPaths)
		applyIntField(intelNode, "embedding_dimensionality", e.EmbeddingDimensionality, &changedPaths)
		applyBoolField(intelNode, "vector_enabled", e.VectorEnabled, &changedPaths)

		// Backplane
		b := patch.Backplane
		applyBoolField(intelNode, "shared_llm_enabled", b.SharedLLMEnabled, &changedPaths)
		applyIntField(intelNode, "llm_port", b.LLMPort, &changedPaths)
		applyIntField(intelNode, "max_concurrent_requests", b.MaxConcurrent, &changedPaths)
		applyIntField(intelNode, "max_rpm", b.MaxRPM, &changedPaths)
		applyIntField(intelNode, "max_burst_per_second", b.MaxBurstPerSecond, &changedPaths)
		applyIntField(intelNode, "sub_server_token_thresh", b.SubServerTokenMax, &changedPaths)
		applyIntField(intelNode, "orphan_stream_ttl_minutes", b.OrphanStreamTTL, &changedPaths)

		// If intelligence mapping became empty after edits, remove it
		if len(intelNode.Content) == 0 {
			removeNodeKey(cfgNode, "intelligence")
		}
	}

	return changedPaths, nil
}

func applyStringField(node *yaml.Node, key string, field Field[string], changed *[]string) {
	switch field.State {
	case PatchSet:
		setNodeValue(node, key, field.Value)
		*changed = append(*changed, key)
	case PatchRemove:
		removeNodeKey(node, key)
		*changed = append(*changed, key)
	}
}

func applyIntField(node *yaml.Node, key string, field Field[int], changed *[]string) {
	switch field.State {
	case PatchSet:
		setNodeValue(node, key, field.Value)
		*changed = append(*changed, key)
	case PatchRemove:
		removeNodeKey(node, key)
		*changed = append(*changed, key)
	}
}

func applyFloatField(node *yaml.Node, key string, field Field[float64], changed *[]string) {
	switch field.State {
	case PatchSet:
		setNodeValue(node, key, field.Value)
		*changed = append(*changed, key)
	case PatchRemove:
		removeNodeKey(node, key)
		*changed = append(*changed, key)
	}
}

func applyBoolField(node *yaml.Node, key string, field Field[bool], changed *[]string) {
	switch field.State {
	case PatchSet:
		setNodeValue(node, key, field.Value)
		*changed = append(*changed, key)
	case PatchRemove:
		removeNodeKey(node, key)
		*changed = append(*changed, key)
	}
}

func applyStringSeqField(node *yaml.Node, key string, field Field[[]string], changed *[]string) {
	switch field.State {
	case PatchSet:
		setNodeSequence(node, key, field.Value)
		*changed = append(*changed, key)
	case PatchRemove:
		removeNodeKey(node, key)
		*changed = append(*changed, key)
	}
}
