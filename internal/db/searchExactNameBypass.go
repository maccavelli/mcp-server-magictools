package db

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

func (s *Store) searchExactNameViaScan(query, category, serverConstraint string, domain SearchDomain) ([]*ToolRecord, bool) {
	var exactMatches []*ToolRecord
	viewOrWarn(s.DB, func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			itemValueOrWarn(item, func(val []byte) error {
				var r ToolRecord
				if err := json.Unmarshal(val, &r); err != nil {
					return err
				}
				if !strings.EqualFold(r.Name, query) {
					return nil
				}
				if !toolAllowedForExactSearch(&r, query, category, serverConstraint, domain) {
					return nil
				}
				exactMatches = append(exactMatches, s.finalizeExactMatch(&r))
				return nil
			})
		}
		return nil
	})
	if len(exactMatches) == 0 {
		return nil, false
	}
	return exactMatches, true
}

func (s *Store) searchExactNameBypass(_ context.Context, query, category, serverConstraint string, domain SearchDomain) ([]*ToolRecord, bool) {
	if query == "" || strings.Contains(query, ":") {
		return nil, false
	}
	if matches, ok := s.searchExactNameViaBleve(query, category, serverConstraint, domain); ok {
		return matches, true
	}
	return s.searchExactNameViaScan(query, category, serverConstraint, domain)
}
