package handler

import (
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func TestIsSafeToCache(t *testing.T) {
	h := &OrchestratorHandler{Config: &config.Config{ScoreFusionAlpha: 0.5}}

	// Safe words
	if !h.isSafeToCache("server:get_status") {
		t.Error("expected true")
	}
	if !h.isSafeToCache("oc_describe_pod") {
		t.Error("expected true")
	}
	if !h.isSafeToCache("git_status") {
		t.Error("expected true")
	}
	// Unsafe overrides
	if h.isSafeToCache("add_user") {
		t.Error("expected false due to 'add'")
	}
	// Default
	if h.isSafeToCache("unknown_verb") {
		t.Error("expected false default")
	}
}

func TestGetCacheKey(t *testing.T) {
	h := &OrchestratorHandler{Config: &config.Config{ScoreFusionAlpha: 0.5}}

	args := map[string]any{"foo": "bar"}
	key1 := h.getCacheKey("test_urn", args)
	key2 := h.getCacheKey("test_urn", args)

	if key1 != key2 {
		t.Error("expected keys to match")
	}

	args2 := map[string]any{"foo": "baz"}
	key3 := h.getCacheKey("test_urn", args2)
	if key1 == key3 {
		t.Error("expected keys to differ")
	}
}
