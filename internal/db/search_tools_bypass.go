package db

import (
	"context"
)

func (s *Store) searchToolsBypassPaths(ctx context.Context, query string, category string, serverConstraint string, domain SearchDomain) ([]*ToolRecord, bool) {
	if hits, ok := s.searchExactNameBypass(ctx, query, category, serverConstraint, domain); ok {
		return hits, true
	}
	return s.searchURNBypass(ctx, query, category, serverConstraint, domain)
}
