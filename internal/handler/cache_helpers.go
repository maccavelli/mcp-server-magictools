package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// proxyResponseCacheTTL bounds how long a safe (read-only) proxy response is
// served from the freshness cache before re-validating against the sub-server.
const proxyResponseCacheTTL = 60 * time.Second

// isSafeToCache implements the "Safety-First" policy for the Freshness Layer.
// Only tools with explicit read/status intent are permitted to bypass the sub-server.
func (h *OrchestratorHandler) isSafeToCache(urn string) bool {
	u := strings.ToLower(urn)

	// 1. Mutation Blacklist (Safety Override)
	unsafeVerbs := []string{
		"add", "write", verbDelete, "apply", "patch", verbUpdate,
		"create", "build", "run", "restart", "stop", "start",
	}
	for _, v := range unsafeVerbs {
		if strings.Contains(u, v) {
			return false
		}
	}

	// 2. Explicit Whitelist (High Sensitivity Tools)
	if strings.Contains(u, "git_status") ||
		strings.Contains(u, "oc_get") ||
		strings.Contains(u, "oc_describe") {
		return true
	}

	// 3. Prefix Matching (Convention Based)
	safePrefixes := []string{
		"get_", "list_", "search_", "status_", "read_",
		"view_", "describe_", "check_", "fetch_", "lookup_", "ping_",
	}
	for _, p := range safePrefixes {
		if strings.Contains(u, ":"+p) || strings.HasPrefix(u, p) {
			return true
		}
	}

	return false
}

// getCacheKey generates a collision-resistant SHA256 hash of the tool identity and its parameters.
func (h *OrchestratorHandler) getCacheKey(urn string, args map[string]any) string {
	argData := marshalJSONOrEmpty(args)
	hash := sha256.New()
	hash.Write([]byte(urn))
	hash.Write(argData)
	return hex.EncodeToString(hash.Sum(nil))
}
