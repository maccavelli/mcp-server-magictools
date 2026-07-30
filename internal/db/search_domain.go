package db

import "strings"

// searchRecallK is the unified candidate pool size for Bleve and HNSW retrieval legs.
const searchRecallK = 20

// perResultCullFactor scales scoreThreshold for tail-result suppression after fusion boosts.
// Results below (scoreThreshold * perResultCullFactor) are dropped individually.
const perResultCullFactor = 0.4

// recordAllowedInSearch applies category, server, and domain visibility rules.
func recordAllowedInSearch(record *ToolRecord, query, category, serverConstraint string, domain SearchDomain) bool {
	if record == nil {
		return false
	}
	if category != "" && !strings.EqualFold(record.Category, category) {
		return false
	}
	if serverConstraint != "" && record.Server != serverConstraint {
		return false
	}
	return IsServerVisibleInDomain(record.Server, query, serverConstraint, domain)
}
