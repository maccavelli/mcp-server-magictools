// Package client provides functionality for the client subsystem.
package client

import (
	"log/slog"
	"regexp"
	"strings"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

var (
	requiresRegex       = regexp.MustCompile(`\[REQUIRES:\s*(.*?)\]`)
	triggersRegex       = regexp.MustCompile(`\[TRIGGERS:\s*(.*?)\]`)
	roleTagRegex        = regexp.MustCompile(`(?i)\[ROLE:\s*(\w+)\]`)
	phaseTagRegex       = regexp.MustCompile(`(?i)\[PHASE:\s*(\w+)\]`)
	inputContractRegex  = regexp.MustCompile(`(?i)\[INPUT:\s*(\S+)\]`)
	outputContractRegex = regexp.MustCompile(`(?i)\[OUTPUT:\s*(\S+)\]`)
)

// Pipeline Taxonomy Constants
const (
	RoleAnalyzer    = "ANALYZER"
	RoleMutator     = "MUTATOR"
	RoleCritic      = "CRITIC"
	RoleSynthesizer = "SYNTHESIZER"
	RoleDiagnostic  = "DIAGNOSTIC"
	RolePlanner     = "PLANNER"
	RoleThreat      = "THREAT"
	RoleReporting   = "REPORTING"

	PhaseBootstrap   = 0
	PhaseAnalysis    = 1
	PhaseAdversarial = 2
	PhaseProposal    = 3
	PhaseCritique    = 4
	PhaseSynthesis   = 5
	PhaseMutator     = 6
	PhaseValidation  = 7
	PhaseTerminal    = 8
)

// phaseNames maps description-level [PHASE: X] tag values to Phase constants.
var phaseNames = map[string]int{
	"BOOTSTRAP":   PhaseBootstrap,
	"ANALYSIS":    PhaseAnalysis,
	"ADVERSARIAL": PhaseAdversarial,
	"PROPOSAL":    PhaseProposal,
	"CRITIQUE":    PhaseCritique,
	"SYNTHESIS":   PhaseSynthesis,
	"MUTATOR":     PhaseMutator,
	"VALIDATION":  PhaseValidation,
	"TERMINAL":    PhaseTerminal,
}

// validRoles is the set of recognized pipeline roles.
var validRoles = map[string]bool{
	RoleAnalyzer:    true,
	RoleMutator:     true,
	RoleCritic:      true,
	RoleSynthesizer: true,
	RolePlanner:     true,
	RoleThreat:      true,
	RoleReporting:   true,
}

// defaultPhaseForRole maps a role to its default execution phase.
// Tools can override this with an explicit [PHASE: X] tag.
var defaultPhaseForRole = map[string]int{
	RoleAnalyzer:    PhaseAnalysis,
	RoleCritic:      PhaseCritique,
	RoleSynthesizer: PhaseSynthesis,
	RoleMutator:     PhaseMutator,
	RolePlanner:     PhaseSynthesis,
	RoleThreat:      PhaseAdversarial,
	RoleReporting:   PhaseTerminal,
}

// hydrateRoleAndPhase assigns Role and Phase to a ToolRecord by parsing
// description tags. No hardcoded per-tool or per-server fallbacks.
//
// Classification sources (all from tool description):
//   - [ROLE: X]  → sets Role
//   - [PHASE: X] → sets Phase (overrides default role→phase mapping)
//   - [REQUIRES: urn1, urn2] → sets dependency edges
//   - [TRIGGERS: urn1, urn2] → sets trigger edges
func hydrateRoleAndPhase(record *db.ToolRecord) {
	if record == nil {
		return
	}

	// Parse dependency/trigger edges.
	hydrateDependencies(record)

	// Parse [INPUT: X] and [OUTPUT: X] data-contract tags.
	if match := inputContractRegex.FindStringSubmatch(record.Description); len(match) > 1 {
		record.InputContract = strings.ToLower(match[1])
	}
	if match := outputContractRegex.FindStringSubmatch(record.Description); len(match) > 1 {
		record.OutputContract = strings.ToLower(match[1])
	}

	// Parse [ROLE: X] tag — the single source of truth.
	if match := roleTagRegex.FindStringSubmatch(record.Description); len(match) > 1 {
		role := strings.ToUpper(match[1])
		if validRoles[role] {
			record.Role = role

			// Parse [PHASE: X] tag — optional override for non-default phases.
			if phaseMatch := phaseTagRegex.FindStringSubmatch(record.Description); len(phaseMatch) > 1 {
				phaseName := strings.ToUpper(phaseMatch[1])
				if p, ok := phaseNames[phaseName]; ok {
					record.Phase = p
				} else {
					slog.Warn("pipeline_taxonomy: unknown [PHASE:] tag value, using role default",
						"urn", record.URN, "phase_tag", phaseName)
					record.Phase = defaultPhaseForRole[role]
				}
			} else {
				// No explicit [PHASE:] → use default for this role.
				record.Phase = defaultPhaseForRole[role]
			}
			return
		}
	}

	// No [ROLE:] tag found. Check for system/diagnostic tools.
	if record.Name == "get_internal_logs" {
		record.Role = RoleDiagnostic
		record.Phase = -1
		return
	}

	// Untagged non-system tool on a pipeline server — log warning.
	// This tool will be excluded from compose_pipeline DAGs (Role == "").
	if record.Server == serverBrainstorm || record.Server == serverGoModernizer {
		slog.Warn("pipeline_taxonomy: tool missing [ROLE:] tag, excluded from DAG",
			"urn", record.URN, "server", record.Server)
	}
}

// hydrateDependencies parses [REQUIRES:] and [TRIGGERS:] tags from descriptions.
func hydrateDependencies(record *db.ToolRecord) {
	if reqMatch := requiresRegex.FindStringSubmatch(record.Description); len(reqMatch) > 1 {
		urns := strings.SplitSeq(reqMatch[1], ",")
		for u := range urns {
			record.Requires = append(record.Requires, strings.TrimSpace(u))
		}
	}
	if trigMatch := triggersRegex.FindStringSubmatch(record.Description); len(trigMatch) > 1 {
		urns := strings.SplitSeq(trigMatch[1], ",")
		for u := range urns {
			record.Triggers = append(record.Triggers, strings.TrimSpace(u))
		}
	}
}
