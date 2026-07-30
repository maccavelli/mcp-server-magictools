package db

import (
	"context"
	"testing"

	"github.com/blevesearch/bleve/v2"
)

// hitHasURN reports whether a search result includes the given URN.
func hitHasURN(res *bleve.SearchResult, urn string) bool {
	for _, h := range res.Hits {
		if h.ID == urn {
			return true
		}
	}
	return false
}

// TestBLV8_PrunedFieldsNotSearchable is the BLV-8 regression: fields removed from
// the Bleve adapter (here input_contract) must no longer be indexed or matchable on
// the lexical leg, while fields that were kept (here requires) must still match.
// A distinct token placed only in a pruned field must not surface its tool; the same
// kind of token in a kept field must.
func TestBLV8_PrunedFieldsNotSearchable(t *testing.T) {
	si, err := NewSearchIndex(t.TempDir())
	if err != nil {
		t.Fatalf("NewSearchIndex: %v", err)
	}

	// One token lives ONLY in a pruned field, another ONLY in a kept field. Neither
	// appears in name/description/etc., so each token's matchability is decided purely
	// by whether its field is still indexed.
	rec := &ToolRecord{
		URN:           "srv:tool_one",
		Server:        "srv",
		Name:          "tool_one",
		Description:   "an ordinary tool",
		InputContract: "prunedonlytoken",         // BLV-8 removed → must NOT be searchable
		Requires:      []string{"keptonlytoken"}, // still indexed → must be searchable
	}
	if err := si.IndexRecord(ToBleveDoc(rec)); err != nil {
		t.Fatalf("IndexRecord: %v", err)
	}

	// Kept field (requires) — the tool must surface.
	keptRes, err := si.Search(context.Background(), "keptonlytoken", "", "", DomainSystem)
	if err != nil {
		t.Fatalf("Search(kept): %v", err)
	}
	if !hitHasURN(keptRes, "srv:tool_one") {
		t.Errorf("kept field 'requires' should still be searchable, but the tool did not surface for its token")
	}

	// Pruned field (input_contract) — the token is no longer indexed anywhere, so the
	// tool must NOT surface for it.
	prunedRes, err := si.Search(context.Background(), "prunedonlytoken", "", "", DomainSystem)
	if err != nil {
		t.Fatalf("Search(pruned): %v", err)
	}
	if hitHasURN(prunedRes, "srv:tool_one") {
		t.Errorf("pruned field 'input_contract' is still searchable — BLV-8 prune did not take effect")
	}
}
