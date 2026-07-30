package db

import (
	"context"
)

func (s *Store) searchToolsWithQuery(ctx context.Context, query string, category string, serverConstraint string, scoreThreshold float64, alpha float64, domain SearchDomain, skipVector bool) ([]*ToolRecord, error) {
	if hits, ok := s.searchToolsBypassPaths(ctx, query, category, serverConstraint, domain); ok {
		return hits, nil
	}
	return s.searchToolsFuseResults(ctx, query, category, serverConstraint, scoreThreshold, alpha, domain, skipVector)
}
