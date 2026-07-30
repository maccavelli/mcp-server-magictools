package db

import (
	"testing"

	"github.com/blevesearch/bleve/v2"
)

func TestSyntheticIntents(t *testing.T) {
	si, err := NewSearchIndex("test_index_synthetic")
	if err != nil {
		t.Fatalf("failed to create index: %v", err)
	}
	defer si.Close()

	prompt := "how do I refactor Go code"
	dag := []string{"go-modernizer:apply_vetted_edit", "go-modernizer:go_complexity_analyzer"}

	err = si.IndexSyntheticIntent(prompt, dag)
	if err != nil {
		t.Fatalf("failed to index synthetic intent: %v", err)
	}

	results, err := si.SearchSyntheticIntents("refactor Go")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}

	if len(results) == 0 {
		t.Fatal("expected results, got 0")
	}

	if results[0].URN != dag[0] {
		t.Errorf("expected %s, got %s", dag[0], results[0].URN)
	}

	// Test GetToolsByServer
	// Index a normal tool first
	doc := BleveToolDocument{
		URN:    "server1:tool1",
		Name:   "tool1",
		Server: "server1",
	}
	si.IndexRecord(doc)

	urns, err := si.GetToolsByServer("server1", 10)
	if err != nil {
		t.Fatalf("GetToolsByServer failed: %v", err)
	}
	if len(urns) != 1 || urns[0] != "server1:tool1" {
		t.Errorf("unexpected urns: %v", urns)
	}

	// Test SwapIndex
	mapping := bleve.NewIndexMapping()
	newIdx, _ := bleve.NewMemOnly(mapping)
	err = si.SwapIndex(newIdx)
	if err != nil {
		t.Errorf("SwapIndex failed: %v", err)
	}
}
