package db

import (
	"context"
	"strings"
)

func (s *Store) searchURNBypass(_ context.Context, query string, category string, serverConstraint string, domain SearchDomain) ([]*ToolRecord, bool) {
	// ⚡ FAST-PATH URN BYPASS: If query is an exact URN, bypass Bleve and Vector entirely
	if strings.Contains(query, ":") {
		if r, err := s.GetTool(query); err == nil && r != nil {
			allowed := true
			if category != "" && !strings.EqualFold(r.Category, category) {
				allowed = false
			}
			if serverConstraint != "" && r.Server != serverConstraint {
				allowed = false
			}

			if !IsServerVisibleInDomain(r.Server, query, serverConstraint, domain) {
				allowed = false
			}

			if allowed {
				if intel, err := s.GetIntelligence(r.URN); err == nil && intel != nil {
					r.OverlayIntelligence(intel)
				}
				r.ConfidenceScore = 1.0
				return []*ToolRecord{r}, true
			}
		}
	}

	return nil, false
}
