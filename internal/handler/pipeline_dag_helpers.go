package handler

import "fmt"

type dagRoleIndices struct {
	analyzer    int
	critic      int
	mutator     int
	synthesizer int
	planner     int
}

func dagFirstRoleIndices(stages []PipelineStep) dagRoleIndices {
	idx := dagRoleIndices{
		analyzer: -1, critic: -1, mutator: -1, synthesizer: -1, planner: -1,
	}
	for i, s := range stages {
		switch s.Role {
		case roleAnalyzer:
			if idx.analyzer == -1 {
				idx.analyzer = i
			}
		case roleCritic:
			if idx.critic == -1 {
				idx.critic = i
			}
		case roleMutator:
			if idx.mutator == -1 {
				idx.mutator = i
			}
		case roleSynthesizer:
			if idx.synthesizer == -1 {
				idx.synthesizer = i
			}
		case rolePlanner:
			if idx.planner == -1 {
				idx.planner = i
			}
		}
	}
	return idx
}

func dagGrammarWarnings(idx dagRoleIndices) []string {
	var warnings []string
	if idx.mutator >= 0 && idx.analyzer >= 0 && idx.mutator < idx.analyzer {
		warnings = append(warnings, "Grammar Violation: MUTATOR tool appears before ANALYZER tools — mutations must follow analysis.")
	}
	if idx.mutator >= 0 && idx.planner >= 0 && idx.mutator < idx.planner {
		warnings = append(warnings, "Grammar Violation: MUTATOR tool appears before PLANNER tools — mutations must follow formal planning.")
	}
	if idx.planner >= 0 && idx.synthesizer >= 0 && idx.planner < idx.synthesizer {
		warnings = append(warnings, "Grammar Violation: PLANNER tool appears before SYNTHESIZER tools — planning requires synthesized diagnostics.")
	}
	if idx.synthesizer >= 0 && idx.critic >= 0 && idx.synthesizer < idx.critic {
		warnings = append(warnings, "Grammar Violation: SYNTHESIZER tool appears before CRITIC tools — synthesis requires adversarial verdicts.")
	}
	return warnings
}

func dagContractWarnings(stages []PipelineStep) []string {
	var warnings []string
	for i := 0; i < len(stages)-1; i++ {
		if stages[i].OutputContract != "" && stages[i+1].InputContract != "" {
			if stages[i].OutputContract != stages[i+1].InputContract {
				warnings = append(warnings, fmt.Sprintf(
					"Data Contract Mismatch: Stage %d (%s) outputs [%s] but Stage %d (%s) expects [%s].",
					i+1, stages[i].ToolName, stages[i].OutputContract,
					i+2, stages[i+1].ToolName, stages[i+1].InputContract))
			}
		}
	}
	for i := 0; i < len(stages)-1; i++ {
		if stages[i].OutputContract == "" {
			continue
		}
		consumed := false
		for j := i + 1; j < len(stages); j++ {
			if stages[j].InputContract == stages[i].OutputContract {
				consumed = true
				break
			}
		}
		if !consumed {
			warnings = append(warnings, fmt.Sprintf(
				"Data Contract Gap: Stage %d (%s) outputs [%s] but no subsequent stage consumes it.",
				i+1, stages[i].ToolName, stages[i].OutputContract))
		}
	}
	return warnings
}
