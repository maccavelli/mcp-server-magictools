package db

import (
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"github.com/dgraph-io/badger/v4"
)

func loadIntelMapInTxn(txn *badger.Txn) map[string]*ToolIntelligence {
	intelMap := make(map[string]*ToolIntelligence)
	intelIt := txn.NewIterator(badger.DefaultIteratorOptions)
	intelPrefix := []byte("intel:")
	for intelIt.Seek(intelPrefix); intelIt.ValidForPrefix(intelPrefix); intelIt.Next() {
		item := intelIt.Item()
		urn := strings.TrimPrefix(string(item.Key()), "intel:")
		itemValueOrWarn(item, func(val []byte) error {
			var ti ToolIntelligence
			if json.Unmarshal(val, &ti) == nil {
				intelMap[urn] = &ti
			}
			return nil
		})
	}
	intelIt.Close()
	return intelMap
}

func scanFallbackToolsInTxn(txn *badger.Txn, intelMap map[string]*ToolIntelligence, query, category, serverConstraint string, domain SearchDomain, results *[]*ToolRecord) {
	queryLower := strings.ToLower(query)
	categoryLower := strings.ToLower(category)
	serverLower := strings.ToLower(serverConstraint)

	it := txn.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()
	prefix := []byte("tool:")
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		valErr := item.Value(func(val []byte) error {
			var r ToolRecord
			if err := json.Unmarshal(val, &r); err != nil {
				return err
			}
			if categoryLower != "" && !strings.EqualFold(r.Category, categoryLower) {
				return nil
			}
			if serverLower != "" && !strings.EqualFold(r.Server, serverLower) {
				return nil
			}
			if !IsServerVisibleInDomain(r.Server, query, serverConstraint, domain) {
				return nil
			}
			words := strings.Fields(queryLower)
			if !fallbackTextMatch(&r, words, queryLower) {
				return nil
			}
			if intel, ok := intelMap[r.URN]; ok && intel != nil {
				r.OverlayIntelligence(intel)
			}
			if matchesNegativeTriggers(query, r.NegativeTriggers) {
				return nil
			}
			r.ConfidenceScore = fallbackMatchScore(&r, words, queryLower)
			*results = append(*results, &r)
			return nil
		})
		if valErr != nil && !errors.Is(valErr, badger.ErrDiscardedTxn) {
			slog.Debug("failed to parse fallback tool", "error", valErr)
		}
	}
}

func sortToolRecordsByConfidence(results []*ToolRecord) {
	for i := 1; i < len(results); i++ {
		for j := i; j > 0 && results[j].ConfidenceScore > results[j-1].ConfidenceScore; j-- {
			results[j], results[j-1] = results[j-1], results[j]
		}
	}
}
