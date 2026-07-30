package db

import (
	"slices"
	"strings"
)

const (
	serverBrainstorm   = "brainstorm"
	serverGoModernizer = "go-modernizer"
	serverMagictools   = "magictools"
)

func isUserLandRestrictedServer(server string) bool {
	return server == serverBrainstorm || server == serverGoModernizer
}

func isPipelineAllowedServer(server string) bool {
	return server == serverMagictools || server == serverBrainstorm || server == serverGoModernizer
}

// IsServerVisibleInDomain reports whether tools from server are eligible for retrieval
// in the given search domain, considering query intent and optional server constraint.
func IsServerVisibleInDomain(server, query, serverConstraint string, domain SearchDomain) bool {
	switch domain {
	case DomainSystem:
		return true
	case DomainUserLand:
		if !isUserLandRestrictedServer(server) {
			return true
		}
		if serverConstraint != "" && strings.EqualFold(serverConstraint, server) {
			return true
		}
		qLower := strings.ToLower(query)
		if strings.Contains(qLower, strings.ToLower(server)) {
			return true
		}
		return false
	case DomainPipelineOrchestration:
		return isPipelineAllowedServer(server)
	default:
		return true
	}
}

// matchesNegativeTriggers reports whether every token of any negative trigger appears
// as a whole word in the query (same semantics as applyLexicalBoosts penalty).
func matchesNegativeTriggers(query string, triggers []string) bool {
	if len(triggers) == 0 {
		return false
	}
	queryLower := strings.ToLower(query)
	queryTokens := strings.Fields(queryLower)
	for _, neg := range triggers {
		negTokens := strings.Fields(strings.ToLower(neg))
		if len(negTokens) == 0 {
			continue
		}
		allMatch := true
		for _, nt := range negTokens {
			found := slices.Contains(queryTokens, nt)
			if !found {
				allMatch = false
				break
			}
		}
		if allMatch {
			return true
		}
	}
	return false
}
