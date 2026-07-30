package handler

import (
	"context"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

func (h *OrchestratorHandler) alignToolsResolveResults(
	ctx context.Context,
	args *alignToolsInput,
	showFullSchema bool,
	preferredServer string,
) ([]*db.ToolRecord, error) {
	telemetry.SearchMetrics.TotalSearches.Add(1)

	cacheKey := alignBuildCacheKey(h, args, showFullSchema)
	results, fromCache := h.alignTryCacheHit(cacheKey, args)
	var err error

	if !fromCache {
		telemetry.SearchMetrics.AlignCacheMisses.Add(1)
		results, err = h.alignDiscoverResults(ctx, args)
	}

	if err != nil && !isSearchNoMatchErr(err) {
		return nil, err
	}

	results, err = h.alignRetryUnconstrainedSearch(ctx, args, preferredServer, results)
	if err != nil && !isSearchNoMatchErr(err) {
		return nil, err
	}

	results = h.alignCollectInternalMatches(ctx, args, results)
	h.alignCacheDiscoveryResults(cacheKey, args, fromCache, results)
	alignApplyFailureProximityPenalties(ctx, h, args.Query, results)

	return results, err
}
