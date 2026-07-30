package db

import (
	"context"
	"os"
	"testing"
)

func TestSearchEvolution(t *testing.T) {
	path := "test_search_index"
	_ = os.RemoveAll(path)
	defer os.RemoveAll(path)

	store, err := NewStore(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Add mock tools
	tools := []*ToolRecord{
		{
			URN:         "urn:ddg:search",
			Name:        "DDG Search",
			Description: "Search the web for general information",
			Intent:      "find, locate, retrieval",
			Category:    "search",
		},
		{
			URN:         "urn:filesystem:ls",
			Name:        "List Directory",
			Description: "List files and directories",
			Intent:      "ls, find files, view contents",
			Category:    "filesystem",
		},
	}

	for _, tool := range tools {
		if err := store.SaveTool(tool); err != nil {
			t.Fatal(err)
		}
	}

	// 1. Exact ID match (Boosted)
	results, _ := store.SearchTools(context.Background(), "urn:ddg:search", "", "", 0.0, 0.5, DomainSystem, false)
	if len(results) == 0 || results[0].URN != "urn:ddg:search" {
		t.Errorf("Expected exact URN match, got %+v", results)
	}

	// 2. Fuzzy match (1 typo)
	results, _ = store.SearchTools(context.Background(), "ddg serch", "", "", 0.0, 0.5, DomainSystem, false)
	if len(results) == 0 || results[0].URN != "urn:ddg:search" {
		t.Errorf("Expected fuzzy match for 'ddg serch', got %+v", results)
	}

	// 3. Intent match
	results, _ = store.SearchTools(context.Background(), "locate information", "", "", 0.0, 0.5, DomainSystem, false)
	if len(results) == 0 || results[0].URN != "urn:ddg:search" {
		t.Errorf("Expected intent match for 'locate', got %+v", results)
	}

	// 4. HIERARCHICAL ROUTING: Scoped Search (Boosted OR)
	// Query "find" matches both DDG and LS via intent.
	// We scoped to 'search', so DDG should be the TOP result even if other relevant tools are present.
	results, _ = store.SearchTools(context.Background(), "find", "search", "", 0.0, 0.5, DomainSystem, false)
	if len(results) == 0 || results[0].URN != "urn:ddg:search" {
		t.Errorf("Expected top match for 'find' in 'search' to be DDG, got %+v", results)
	}

	// Scope to 'filesystem' should return LS as the TOP result.
	results, _ = store.SearchTools(context.Background(), "find", "filesystem", "", 0.0, 0.5, DomainSystem, false)
	if len(results) == 0 || results[0].URN != "urn:filesystem:ls" {
		t.Errorf("Expected top match for 'find' in 'filesystem' to be LS, got %+v", results)
	}
}
