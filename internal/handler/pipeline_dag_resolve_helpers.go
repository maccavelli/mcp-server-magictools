package handler

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func dagBuildRegistry(pipelineTools []*db.ToolRecord) map[string]*db.ToolRecord {
	registry := make(map[string]*db.ToolRecord, len(pipelineTools))
	for _, t := range pipelineTools {
		registry[t.URN] = t
	}
	return registry
}

func dagSelectedSet(stages []PipelineStep) map[string]bool {
	selected := make(map[string]bool, len(stages))
	for _, s := range stages {
		selected[s.ToolName] = true
	}
	return selected
}

func dagNeedsTrifecta(intent string, stages []PipelineStep) bool {
	intentLower := strings.ToLower(intent)
	if strings.Contains(intentLower, "improve") || strings.Contains(intentLower, "evaluate") {
		return true
	}
	trifectaURNs := []string{urnBrainstormThesisArchitect, urnBrainstormAntithesisSkeptic, urnBrainstormAporiaEngine}
	for _, s := range stages {
		if slices.Contains(trifectaURNs, s.ToolName) {
			return true
		}
	}
	return false
}

func dagInjectTrifecta(stages []PipelineStep, registry map[string]*db.ToolRecord, intent string) []PipelineStep {
	if !dagNeedsTrifecta(intent, stages) {
		return stages
	}
	selected := dagSelectedSet(stages)
	trifectaURNs := []string{urnBrainstormThesisArchitect, urnBrainstormAntithesisSkeptic, urnBrainstormAporiaEngine}
	for _, urn := range trifectaURNs {
		if selected[urn] {
			continue
		}
		target, ok := registry[urn]
		if !ok {
			continue
		}
		stages = append(stages, PipelineStep{
			ToolName: target.URN, Role: target.Role, Phase: target.Phase,
			Purpose:        "Atomic Socratic Trifecta Enclave: Automatically bound sequentially.",
			InputContract:  target.InputContract,
			OutputContract: target.OutputContract,
		})
		selected[urn] = true
	}
	return stages
}

func dagInjectMandatoryPlanner(stages []PipelineStep, registry map[string]*db.ToolRecord, intent string) []PipelineStep {
	if !intentRequiresMutation(intent) {
		return stages
	}
	selected := dagSelectedSet(stages)
	for _, s := range stages {
		if s.Role == rolePlanner {
			return stages
		}
	}
	plannerURN := urnGoModernizerGenerateImplPlan
	target, ok := registry[plannerURN]
	if !ok || selected[plannerURN] {
		return stages
	}
	slog.Info("resolveDynamicDAG: mandatory PLANNER injected", keyURN, plannerURN)
	return append(stages, PipelineStep{
		ToolName: target.URN, Role: target.Role, Phase: target.Phase,
		Purpose:        "Mandatory PLANNER Injection: Ensures plan generation before autonomous MUTATOR injection.",
		InputContract:  target.InputContract,
		OutputContract: target.OutputContract,
	})
}

func dagInjectMandatoryReporting(stages []PipelineStep, registry map[string]*db.ToolRecord) []PipelineStep {
	for _, s := range stages {
		if s.ToolName == urnBrainstormGenerateFinalReport {
			return stages
		}
	}
	reportURN := urnBrainstormGenerateFinalReport
	target, ok := registry[reportURN]
	if !ok {
		return stages
	}
	slog.Info("resolveDynamicDAG: mandatory REPORTING injected", keyURN, reportURN)
	return append(stages, PipelineStep{
		ToolName: target.URN, Role: target.Role, Phase: target.Phase,
		Purpose:        "Mandatory REPORTING Injection: Ensures final Socratic report generation.",
		InputContract:  target.InputContract,
		OutputContract: target.OutputContract,
	})
}

func dagAppendRequiredNode(stages []PipelineStep, selected map[string]bool, registry map[string]*db.ToolRecord, urn, purposeFmt, anchor string) ([]PipelineStep, bool) {
	if selected[urn] {
		return stages, false
	}
	target, ok := registry[urn]
	if !ok {
		return stages, false
	}
	stages = append(stages, PipelineStep{
		ToolName: target.URN, Role: target.Role, Phase: target.Phase,
		Purpose:        fmt.Sprintf(purposeFmt, anchor),
		InputContract:  target.InputContract,
		OutputContract: target.OutputContract,
	})
	selected[urn] = true
	return stages, true
}

func dagExpandStageDependencies(stages []PipelineStep, selected map[string]bool, registry map[string]*db.ToolRecord) ([]PipelineStep, bool) {
	added := false
	for _, s := range stages {
		rec, exists := registry[s.ToolName]
		if !exists {
			continue
		}
		for _, req := range rec.Requires {
			var ok bool
			stages, ok = dagAppendRequiredNode(stages, selected, registry, req,
				"Dynamic DAG Requirement: Anchoring execution mapped natively off %s bounds.", s.ToolName)
			if ok {
				added = true
			}
		}
		for _, trig := range rec.Triggers {
			var ok bool
			stages, ok = dagAppendRequiredNode(stages, selected, registry, trig,
				"Dynamic DAG Trigger: Fired organically executing %s dependency limits.", s.ToolName)
			if ok {
				added = true
			}
		}
	}
	return stages, added
}

func dagExpandReverseInterceptors(stages []PipelineStep, selected map[string]bool, pipelineTools []*db.ToolRecord) ([]PipelineStep, bool) {
	added := false
	for _, rec := range pipelineTools {
		if selected[rec.URN] || rec.Server != serverMagictools {
			continue
		}
		for _, req := range rec.Requires {
			if !selected[req] {
				continue
			}
			stages = append(stages, PipelineStep{
				ToolName: rec.URN, Role: rec.Role, Phase: rec.Phase,
				Purpose:        fmt.Sprintf("Dynamic DAG Interceptor: Automatically bound to required node %s natively.", req),
				InputContract:  rec.InputContract,
				OutputContract: rec.OutputContract,
			})
			selected[rec.URN] = true
			added = true
			break
		}
	}
	return stages, added
}

func dagExpandDependencyLoop(stages []PipelineStep, pipelineTools []*db.ToolRecord) []PipelineStep {
	for {
		selected := dagSelectedSet(stages)
		var added bool
		var ok bool
		stages, ok = dagExpandStageDependencies(stages, selected, dagBuildRegistry(pipelineTools))
		added = added || ok
		stages, ok = dagExpandReverseInterceptors(stages, selected, pipelineTools)
		added = added || ok
		if !added {
			break
		}
	}
	return stages
}
