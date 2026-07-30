package db

import (
	"strings"

	"github.com/blevesearch/bleve/v2"
)

func toolAllowedForExactSearch(r *ToolRecord, query, category, serverConstraint string, domain SearchDomain) bool {
	if category != "" && !strings.EqualFold(r.Category, category) {
		return false
	}
	if serverConstraint != "" && r.Server != serverConstraint {
		return false
	}
	return IsServerVisibleInDomain(r.Server, query, serverConstraint, domain)
}

func (s *Store) finalizeExactMatch(r *ToolRecord) *ToolRecord {
	if intel, err := s.GetIntelligence(r.URN); err == nil && intel != nil {
		r.OverlayIntelligence(intel)
	}
	r.ConfidenceScore = 1.0
	return r
}

func (s *Store) searchExactNameViaBleve(query, category, serverConstraint string, domain SearchDomain) ([]*ToolRecord, bool) {
	if s.Index == nil {
		return nil, false
	}
	idx := s.Index.GetUnderlyingIndex()
	if idx == nil {
		return nil, false
	}
	q1 := bleve.NewTermQuery(strings.ToLower(query))
	q1.SetField("name")
	q2 := bleve.NewMatchQuery(query)
	q2.SetField("name")
	searchReq := bleve.NewSearchRequest(bleve.NewDisjunctionQuery(q1, q2))
	searchReq.Size = 20
	res, err := idx.Search(searchReq)
	if err != nil || res == nil {
		return nil, false
	}
	var matches []*ToolRecord
	for _, hit := range res.Hits {
		r, err := s.GetTool(hit.ID)
		if err != nil || r == nil || !strings.EqualFold(r.Name, query) {
			continue
		}
		if !toolAllowedForExactSearch(r, query, category, serverConstraint, domain) {
			continue
		}
		matches = append(matches, s.finalizeExactMatch(r))
	}
	if len(matches) == 0 {
		return nil, false
	}
	return matches, true
}
