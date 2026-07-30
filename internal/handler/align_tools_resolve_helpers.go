package handler

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/intelligence"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
)

func alignBuildCacheKey(h *OrchestratorHandler, args *alignToolsInput, showFullSchema bool) string {
	hasArgs := args.Arguments != nil
	return fmt.Sprintf("%s|%s|%s|%v|%v|prf:%f|thr:%f|alpha:%f|strict:%v|fallback:%v|gates:%f/%f|gen:%d",
		args.Query, args.Category, args.ServerName, showFullSchema, hasArgs,
		h.Config.PRFConfidenceSkip,
		h.Config.ScoreThreshold,
		h.Config.ScoreFusionAlpha,
		h.Config.StrictGates,
		h.Config.DisableSearchFallback,
		h.Config.VectorMinCosine,
		h.Config.BM25MinNormalized,
		h.alignGen.Load(),
	)
}

func (h *OrchestratorHandler) alignTryCacheHit(cacheKey string, args *alignToolsInput) ([]*db.ToolRecord, bool) {
	if args.Arguments != nil {
		return nil, false
	}
	cached, ok := h.AlignCache.Get(cacheKey)
	if !ok {
		return nil, false
	}
	telemetry.SearchMetrics.AlignCacheHits.Add(1)
	restoreSearchTop5FromCache(cached)
	return cloneToolRecords(cached.Results), true
}

func (h *OrchestratorHandler) alignTryURNPrefixFastPath(args *alignToolsInput) []*db.ToolRecord {
	searchQuery := args.Query
	if !strings.Contains(searchQuery, ":") && args.ServerName != "" && searchQuery != "" {
		searchQuery = args.ServerName + ":" + searchQuery
	}
	if !strings.Contains(searchQuery, ":") {
		return nil
	}
	tr, hErr := h.Store.GetTool(searchQuery)
	if hErr != nil || tr == nil {
		return nil
	}
	if isAmbiguousURNFastPath(args.ServerName, args.Query, searchQuery) {
		return nil
	}
	tr.ConfidenceScore = 1.0
	storeAlignFastPathTrace(args.Query, keyURN)
	return []*db.ToolRecord{tr}
}

func (h *OrchestratorHandler) alignTryInternalExactMatch(args *alignToolsInput) []*db.ToolRecord {
	h.toolsMu.RLock()
	defer h.toolsMu.RUnlock()
	for _, it := range h.InternalTools {
		if strings.EqualFold(it.Name, args.Query) || strings.EqualFold("magictools:"+it.Name, args.Query) {
			storeAlignFastPathTrace(args.Query, "internal")
			return []*db.ToolRecord{{
				URN:                    "magictools:" + it.Name,
				Name:                   it.Name,
				Server:                 serverMagictools,
				Category:               it.Category,
				Description:            it.Description,
				HighlightedDescription: it.Description,
				IsNative:               true,
			}}
		}
	}
	return nil
}

func (h *OrchestratorHandler) alignSearchThreshold(args *alignToolsInput) float64 {
	if args.Query == "" {
		return 0.0
	}
	return h.Config.ScoreThreshold
}

func (h *OrchestratorHandler) alignBleveSearch(ctx context.Context, args *alignToolsInput, threshold float64) ([]*db.ToolRecord, error) {
	telemetry.SearchMetrics.AlignSearchInvocations.Add(1)
	telemetry.SearchMetrics.LexicalSearches.Add(1)
	return h.Store.SearchTools(ctx, args.Query, args.Category, args.ServerName, threshold, h.Config.ScoreFusionAlpha, db.DomainUserLand, false)
}

func (h *OrchestratorHandler) alignApplyPRFExpansion(
	ctx context.Context,
	args *alignToolsInput,
	results []*db.ToolRecord,
	threshold float64,
) []*db.ToolRecord {
	topConfidence := 0.0
	if len(results) > 0 {
		topConfidence = results[0].ConfidenceScore
	}
	skipPRF := h.Config.PRFConfidenceSkip > 0 && topConfidence >= h.Config.PRFConfidenceSkip
	if skipPRF {
		slog.Debug("align_tools: PRF skipped — top result confidence exceeds threshold",
			"confidence", topConfidence, "threshold", h.Config.PRFConfidenceSkip)
		return results
	}
	if args.Query == "" || len(results) == 0 {
		return results
	}
	prfTerms := intelligence.ExtractPRFTerms(results, args.Query, 5)
	if len(prfTerms) == 0 {
		return results
	}
	expandedQuery := args.Query + " " + strings.Join(prfTerms, " ")
	expandedResults, expandErr := h.Store.SearchTools(ctx, expandedQuery, args.Category, args.ServerName, threshold, h.Config.ScoreFusionAlpha, db.DomainUserLand, false)
	if expandErr != nil || len(expandedResults) == 0 {
		return results
	}
	overlap := intelligence.ComputeResultOverlap(results, expandedResults)
	if overlap < config.Intelligence.PRFOverlapThreshold {
		slog.Debug("align_tools: PRF expansion rejected (severe topic drift)", "overlap", overlap)
		return results
	}
	enriched := append([]*db.ToolRecord{}, results...)
	seen := make(map[string]bool, len(results))
	for _, r := range results {
		seen[r.URN] = true
	}
	for _, er := range expandedResults {
		if !seen[er.URN] {
			enriched = append(enriched, er)
			seen[er.URN] = true
		}
	}
	sortResultsByConfidence(enriched)
	slog.Debug("align_tools: PRF expansion enriched", "terms", prfTerms, "overlap", overlap)
	return enriched
}

func (h *OrchestratorHandler) alignDiscoverResults(ctx context.Context, args *alignToolsInput) ([]*db.ToolRecord, error) {
	var results []*db.ToolRecord
	results = append(results, h.alignTryURNPrefixFastPath(args)...)
	results = append(results, h.alignTryInternalExactMatch(args)...)
	if len(results) > 0 {
		return results, nil
	}
	threshold := h.alignSearchThreshold(args)
	var err error
	results, err = h.alignBleveSearch(ctx, args, threshold)
	if err != nil {
		return nil, err
	}
	return h.alignApplyPRFExpansion(ctx, args, results, threshold), nil
}

func (h *OrchestratorHandler) alignRetryUnconstrainedSearch(
	ctx context.Context,
	args *alignToolsInput,
	preferredServer string,
	results []*db.ToolRecord,
) ([]*db.ToolRecord, error) {
	if len(results) > 0 || preferredServer == "" || args.ServerName != preferredServer {
		return results, nil
	}
	slog.Info("align_tools: intent pre-filter produced zero results, retrying unconstrained",
		"dropped_server", preferredServer)
	args.ServerName = ""
	return h.Store.SearchTools(ctx, args.Query, args.Category, args.ServerName, h.Config.ScoreThreshold, h.Config.ScoreFusionAlpha, db.DomainUserLand, false)
}

func (h *OrchestratorHandler) alignCollectInternalMatches(ctx context.Context, args *alignToolsInput, results []*db.ToolRecord) []*db.ToolRecord {
	h.toolsMu.RLock()
	defer h.toolsMu.RUnlock()
	var internalMatches []*db.ToolRecord
	for _, it := range h.InternalTools {
		if args.ServerName != "" && !strings.EqualFold(serverMagictools, args.ServerName) {
			continue
		}
		alreadyIncluded := false
		for _, r := range results {
			if r.Name == it.Name && r.Server == serverMagictools {
				alreadyIncluded = true
				break
			}
		}
		if alreadyIncluded {
			continue
		}
		if args.Query != "" &&
			!strings.Contains(strings.ToLower(it.Name), strings.ToLower(args.Query)) &&
			!strings.Contains(strings.ToLower(it.Description), strings.ToLower(args.Query)) {
			continue
		}
		if args.Category != "" && !strings.EqualFold(it.Category, args.Category) {
			continue
		}
		internalMatches = append(internalMatches, &db.ToolRecord{
			URN:                    "magictools:" + it.Name,
			Name:                   it.Name,
			Server:                 serverMagictools,
			Category:               it.Category,
			Description:            it.Description,
			HighlightedDescription: it.Description,
			InputSchema:            h.toSchemaMap(it.InputSchema),
			IsNative:               true,
		})
	}
	_ = ctx
	return append(results, internalMatches...)
}

func (h *OrchestratorHandler) alignCacheDiscoveryResults(cacheKey string, args *alignToolsInput, fromCache bool, results []*db.ToolRecord) {
	if args.Arguments != nil || fromCache || len(results) == 0 {
		return
	}
	bTop, hTop := snapshotTop5FromMetrics()
	h.AlignCache.Add(cacheKey, &AlignCacheEntry{
		Results:   cloneToolRecords(results),
		BleveTop5: bTop,
		HnswTop5:  hTop,
	})
}

func alignApplyFailureProximityPenalties(ctx context.Context, h *OrchestratorHandler, query string, results []*db.ToolRecord) {
	if query == "" || len(results) <= 1 {
		return
	}
	urns := make([]string, len(results))
	for i, r := range results {
		urns[i] = r.URN
	}
	penalties := intelligence.CheckFailureProximityBatch(ctx, h.Store, query, urns)
	for _, r := range results {
		p, exists := penalties[r.URN]
		if !exists {
			p = 1.0
		}
		r.ConfidenceScore *= p
	}
	sort.SliceStable(results, func(i, j int) bool {
		return results[i].ConfidenceScore > results[j].ConfidenceScore
	})
}
