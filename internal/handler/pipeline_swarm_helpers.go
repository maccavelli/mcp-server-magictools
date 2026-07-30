package handler

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/dag"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/intelligence"
	"github.com/maccavelli/mcp-server-magictools/internal/telemetry"
	"github.com/maccavelli/mcp-server-magictools/internal/vector"
)

type swarmScoreContext struct {
	intent        string
	intentWeights map[string]float64
	ghostMap      map[string]int64
	engineScores  map[string]float64
	engineType    string
	penalties     map[string]float64
	roleMap       map[string]bool
	now           int64
}

func (h *OrchestratorHandler) swarmLoadGhostMap(intent string) map[string]int64 {
	ghostMap := make(map[string]int64)
	if h.Store == nil || h.Store.Index == nil {
		return ghostMap
	}
	ghostResults, err := h.Store.Index.SearchSyntheticIntents(intent)
	if err != nil {
		return ghostMap
	}
	for _, g := range ghostResults {
		ghostMap[g.URN] = g.Timestamp
	}
	return ghostMap
}

func (h *OrchestratorHandler) swarmComputeEngineScores(ctx context.Context, intent string, pipelineTools []*db.ToolRecord) (map[string]float64, string) {
	bm25Scores := getOfflineBM25Scores(intent, pipelineTools)
	e := vector.GetEngine()
	if e == nil || !e.VectorEnabled() {
		return bm25Scores, "BM25"
	}
	matchedResults, err := e.SearchWithScores(ctx, intent, 20)
	if err != nil {
		slog.Warn("compose_pipeline: vector search failed, falling back to BM25 natively", keyError, err)
		return bm25Scores, "BM25"
	}
	return swarmFuseHybridScores(h, matchedResults, bm25Scores), "Hybrid-Fusion"
}

func swarmFuseHybridScores(h *OrchestratorHandler, matchedResults []vector.ScoredResult, bm25Scores map[string]float64) map[string]float64 {
	vectorScores := make(map[string]float64, len(matchedResults))
	for _, r := range matchedResults {
		vectorScores[r.Key] = r.Score
	}
	alpha := 0.50
	if h.Config != nil {
		alpha = h.Config.ScoreFusionAlpha
	}
	corroboration := db.DefaultFusionConfig().CorroborationBonus
	if h.Store != nil {
		corroboration = h.Store.Fusion.CorroborationBonus
	}
	scores := make(map[string]float64)
	allURNs := make(map[string]struct{})
	for urn := range bm25Scores {
		allURNs[urn] = struct{}{}
	}
	for urn := range vectorScores {
		allURNs[urn] = struct{}{}
	}
	for urn := range allURNs {
		scores[urn] = db.Score(vectorScores[urn], bm25Scores[urn], alpha, corroboration)
	}
	winURN := ""
	var bestFused float64
	for urn, sc := range scores {
		if sc > bestFused {
			bestFused = sc
			winURN = urn
		}
	}
	telemetry.RecordFusionWinner(vectorScores, bm25Scores, winURN, alpha, nil, db.NormalizeBM25)
	return scores
}

func swarmBuildRoleMap(targetRoles []string) map[string]bool {
	roleMap := make(map[string]bool, len(targetRoles))
	for _, tr := range targetRoles {
		roleMap[strings.ToUpper(tr)] = true
	}
	return roleMap
}

func swarmCollectCandidateURNs(pipelineTools []*db.ToolRecord, roleMap map[string]bool) []string {
	var urns []string
	for _, t := range pipelineTools {
		if t == nil || t.Role == roleDiagnostic {
			continue
		}
		if len(roleMap) > 0 && !roleMap[t.Role] {
			continue
		}
		urns = append(urns, t.URN)
	}
	return urns
}

func swarmScoreCandidate(h *OrchestratorHandler, t *db.ToolRecord, ctx swarmScoreContext, serverCounts map[string]int, totalCandidates int) float64 {
	sEngine := ctx.engineScores[t.URN]
	sSynergy := swarmSynergyScore(ctx.ghostMap, t.URN, ctx.now)
	rRole := computeRoleBoostBlended(t.Role, ctx.intentWeights)
	biasVector, biasSynergy, biasRole := swarmSynthesisBiases(h)
	sFinal := (sEngine * biasVector) + (sSynergy * biasSynergy) + (rRole * biasRole)
	if sIntent := intelligence.GetIntentToolScore(h.Store, ctx.intent, t.URN); sIntent > 0 {
		sFinal += sIntent * 0.10
	}
	if p, ok := ctx.penalties[t.URN]; ok {
		sFinal *= p
	} else {
		sFinal *= 1.0
	}
	sFinal *= swarmNegativeTriggerPenalty(t.NegativeTriggers, ctx.intent)
	sFinal *= swarmServerDiversityMultiplier(t.Server, serverCounts, totalCandidates)
	return sFinal
}

func swarmSynergyScore(ghostMap map[string]int64, urn string, now int64) float64 {
	ts, ok := ghostMap[urn]
	if !ok {
		return 0.0
	}
	if ts <= 0 {
		return 0.1
	}
	ageHours := float64(now-ts) / 3600.0
	const halfLifeHours = 72.0
	return math.Exp(-0.693 * ageHours / halfLifeHours)
}

func swarmSynthesisBiases(h *OrchestratorHandler) (biasVector, biasSynergy, biasRole float64) {
	biasVector, biasSynergy, biasRole = 0.40, 0.15, 0.35
	if h.Config != nil {
		biasVector = h.Config.SynthesisBiasVector
		biasSynergy = h.Config.SynthesisBiasSynergy
		biasRole = h.Config.SynthesisBiasRole
	}
	return biasVector, biasSynergy, biasRole
}

func swarmNegativeTriggerPenalty(triggers []string, intent string) float64 {
	if len(triggers) == 0 {
		return 1.0
	}
	normalizedIntent := strings.ToLower(intent)
	for _, nt := range triggers {
		if strings.Contains(normalizedIntent, strings.ToLower(nt)) {
			return 0.5
		}
	}
	return 1.0
}

func swarmServerDiversityMultiplier(server string, serverCounts map[string]int, totalCandidates int) float64 {
	if totalCandidates <= 2 {
		return 1.0
	}
	serverRatio := float64(serverCounts[server]) / float64(totalCandidates)
	switch {
	case serverRatio > 0.50:
		return 0.85
	case serverRatio < 0.20:
		return 1.15
	default:
		return 1.0
	}
}

func swarmComputeMADThreshold(candidates []scoredTool) float64 {
	threshold := 0.1
	var activeScores []float64
	for _, c := range candidates {
		if c.finalScore > 0 {
			activeScores = append(activeScores, c.finalScore)
		}
	}
	if len(activeScores) == 0 {
		return threshold
	}
	sort.Float64s(activeScores)
	median := swarmMedian(activeScores)
	mad := swarmMedianAbsDev(activeScores, median)
	if mad < 0.05 {
		mad = 0.05
	}
	if dynamic := median + mad; dynamic > threshold {
		threshold = dynamic
	}
	return threshold
}

func swarmMedian(scores []float64) float64 {
	n := len(scores)
	if n == 0 {
		return 0
	}
	if n%2 == 0 {
		return (scores[n/2-1] + scores[n/2]) / 2.0
	}
	return scores[n/2]
}

func swarmMedianAbsDev(scores []float64, median float64) float64 {
	absDevs := make([]float64, len(scores))
	for i, s := range scores {
		absDevs[i] = math.Abs(s - median)
	}
	sort.Float64s(absDevs)
	return swarmMedian(absDevs)
}

func swarmInjectIfMissing(qualified []scoredTool, best *scoredTool, threshold float64) []scoredTool {
	if best == nil {
		return qualified
	}
	for _, q := range qualified {
		if q.record.URN == best.record.URN {
			return qualified
		}
	}
	clone := *best
	clone.finalScore = threshold + 0.01
	return append(qualified, clone)
}

func swarmPreserveCriticalRoles(candidates, qualified []scoredTool, roleMap map[string]bool, threshold float64) []scoredTool {
	if len(roleMap) > 0 && !roleMap[rolePlanner] && !roleMap[roleSynthesizer] && !roleMap[roleReporting] {
		return qualified
	}
	for i := range candidates {
		c := &candidates[i]
		if c.record.Role == rolePlanner || c.record.Role == roleSynthesizer || c.record.Role == roleReporting {
			qualified = swarmInjectIfMissing(qualified, c, threshold)
		}
	}
	return qualified
}

func swarmFillPhaseCoverageGaps(candidates, qualified []scoredTool, threshold float64) []scoredTool {
	coveredPhases := make(map[int]bool)
	qualifiedSet := make(map[string]bool)
	for _, q := range qualified {
		coveredPhases[q.record.Phase] = true
		qualifiedSet[q.record.URN] = true
	}
	for phase := 0; phase <= 5; phase++ {
		if coveredPhases[phase] {
			continue
		}
		var best *scoredTool
		for i := range candidates {
			c := &candidates[i]
			if c.record.Phase == phase && !qualifiedSet[c.record.URN] {
				if best == nil || c.finalScore > best.finalScore {
					best = c
				}
			}
		}
		if best != nil {
			clone := *best
			clone.finalScore = threshold + 0.01
			qualified = append(qualified, clone)
			qualifiedSet[clone.record.URN] = true
		}
	}
	return qualified
}

func swarmInjectMutatorCritics(candidates, qualified []scoredTool, threshold float64, hasMutator bool) []scoredTool {
	if !hasMutator {
		return qualified
	}
	for _, c := range candidates {
		if c.record.Server != serverBrainstorm || c.record.Role != roleCritic {
			continue
		}
		exists := false
		for _, q := range qualified {
			if q.record.URN == c.record.URN {
				exists = true
				break
			}
		}
		if exists {
			continue
		}
		c.finalScore *= 1.35
		if c.finalScore >= threshold {
			qualified = append(qualified, c)
		}
	}
	return qualified
}

func swarmApplyEdgeBoost(h *OrchestratorHandler, qualified []scoredTool) {
	if h.Store == nil {
		return
	}
	for i := 0; i < len(qualified)-1; i++ {
		edgeScore := dag.GetEdgeScore(h.Store, qualified[i].record.URN, qualified[i+1].record.URN)
		if edgeScore > 0.5 {
			qualified[i+1].finalScore += edgeScore * 0.1
		}
	}
}

func swarmComposeStages(qualified []scoredTool, engineType string, engineScores map[string]float64) []PipelineStep {
	var stages []PipelineStep
	for _, q := range qualified {
		purpose := fmt.Sprintf("Score: %.2f (%s: %.2f, Role: %s, Phase: %d)",
			q.finalScore, engineType, engineScores[q.record.URN], q.record.Role, q.record.Phase)
		stages = append(stages, PipelineStep{
			ToolName: q.record.URN, Role: q.record.Role, Phase: q.record.Phase, Purpose: purpose,
			InputContract: q.record.InputContract, OutputContract: q.record.OutputContract,
		})
	}
	return stages
}

func (h *OrchestratorHandler) swarmScoreAllCandidates(
	ctx context.Context,
	intent string,
	targetRoles []string,
	pipelineTools []*db.ToolRecord,
) ([]scoredTool, map[string]float64, string) {
	telemetry.SearchMetrics.TotalSearches.Add(1)
	intentWeights := classifyIntentWeights(intent)
	ghostMap := h.swarmLoadGhostMap(intent)
	engineScores, engineType := h.swarmComputeEngineScores(ctx, intent, pipelineTools)
	roleMap := swarmBuildRoleMap(targetRoles)
	penalties := intelligence.CheckFailureProximityBatch(ctx, h.Store, intent, swarmCollectCandidateURNs(pipelineTools, roleMap))
	sc := swarmScoreContext{
		intent: intent, intentWeights: intentWeights, ghostMap: ghostMap,
		engineScores: engineScores, engineType: engineType, penalties: penalties,
		roleMap: roleMap, now: time.Now().Unix(),
	}
	serverCounts := make(map[string]int)
	var candidates []scoredTool
	var totalCandidates int
	for _, t := range pipelineTools {
		if t == nil || t.Role == roleDiagnostic {
			continue
		}
		if len(roleMap) > 0 && !roleMap[t.Role] {
			continue
		}
		candidates = append(candidates, scoredTool{
			record: t, finalScore: swarmScoreCandidate(h, t, sc, serverCounts, totalCandidates),
		})
		serverCounts[t.Server]++
		totalCandidates++
	}
	return candidates, engineScores, engineType
}

func swarmQualifyCandidates(candidates []scoredTool, threshold float64) ([]scoredTool, bool) {
	var qualified []scoredTool
	hasMutator := false
	for _, c := range candidates {
		if c.finalScore >= threshold {
			qualified = append(qualified, c)
			if c.record.Role == roleMutator {
				hasMutator = true
			}
		}
	}
	return qualified, hasMutator
}
