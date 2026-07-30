package db

import "testing"

func TestIsServerVisibleInDomain(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name             string
		server           string
		query            string
		serverConstraint string
		domain           SearchDomain
		want             bool
	}{
		{"recall user land", "recall", "metrics", "", DomainUserLand, true},
		{"brainstorm masked user land", serverBrainstorm, "search tools", "", DomainUserLand, false},
		{"brainstorm explicit query", serverBrainstorm, "brainstorm ideas", "", DomainUserLand, true},
		{"brainstorm server constraint", serverBrainstorm, "", serverBrainstorm, DomainUserLand, true},
		{"go-modernizer masked", serverGoModernizer, "refactor code", "", DomainUserLand, false},
		{"go-modernizer in query", serverGoModernizer, "go-modernizer upgrade", "", DomainUserLand, true},
		{"magictools user land", serverMagictools, "align", "", DomainUserLand, true},
		{"recall pipeline blocked", "recall", "search", "", DomainPipelineOrchestration, false},
		{"brainstorm pipeline", serverBrainstorm, "", "", DomainPipelineOrchestration, true},
		{"magictools pipeline", serverMagictools, "", "", DomainPipelineOrchestration, true},
		{"recall system", "recall", "", "", DomainSystem, true},
		{"brainstorm system", serverBrainstorm, "", "", DomainSystem, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsServerVisibleInDomain(tc.server, tc.query, tc.serverConstraint, tc.domain)
			if got != tc.want {
				t.Fatalf("IsServerVisibleInDomain(%q, %q, %q, %v) = %v, want %v",
					tc.server, tc.query, tc.serverConstraint, tc.domain, got, tc.want)
			}
		})
	}
}

func TestMatchesNegativeTriggers(t *testing.T) {
	t.Parallel()
	if !matchesNegativeTriggers("never delete files", []string{"never delete"}) {
		t.Fatal("expected negative trigger match")
	}
	if matchesNegativeTriggers("delete files", []string{"never delete"}) {
		t.Fatal("partial tokens should not match")
	}
}

func TestSearchToolsFallback_negativeTriggers(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	_ = store.SaveTool(&ToolRecord{
		URN:         "test:delete",
		Name:        "delete",
		Server:      "test",
		Description: "delete records permanently",
		Category:    "data",
	})
	_ = store.SaveIntelligence("test:delete", &ToolIntelligence{
		NegativeTriggers: []string{"never delete"},
	})

	results, err := store.SearchToolsFallback("never delete", "", "", DomainUserLand)
	if err != nil {
		t.Fatalf("SearchToolsFallback: %v", err)
	}
	for _, r := range results {
		if r.URN == "test:delete" {
			t.Fatal("fallback should exclude tools matching negative triggers")
		}
	}
}

func TestSearchToolsFallback_ranking(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	_ = store.SaveTool(&ToolRecord{
		URN:         "recall:get_metrics",
		Name:        "get_metrics",
		Server:      "recall",
		Description: "Returns recall health metrics telemetry",
		Category:    "memory",
	})
	_ = store.SaveTool(&ToolRecord{
		URN:         "recall:search",
		Name:        "search",
		Server:      "recall",
		Description: "Search transcripts for keywords",
		Category:    "memory",
	})

	results, err := store.SearchToolsFallback("metrics recall", "", "recall", DomainUserLand)
	if err != nil {
		t.Fatalf("SearchToolsFallback: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	if results[0].URN != "recall:get_metrics" {
		t.Fatalf("expected recall:get_metrics first, got %s (score=%f)", results[0].URN, results[0].ConfidenceScore)
	}
	if results[0].ConfidenceScore <= results[1].ConfidenceScore {
		t.Fatalf("expected descending scores, got %f then %f", results[0].ConfidenceScore, results[1].ConfidenceScore)
	}
}

func TestSearchToolsFallback_domainParity(t *testing.T) {
	tmpDir := t.TempDir()
	store, err := NewStore(tmpDir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	_ = store.SaveTool(&ToolRecord{
		URN:         "brainstorm:think",
		Name:        "think",
		Server:      serverBrainstorm,
		Description: "brainstorm ideas",
		Category:    "pipeline",
	})

	results, err := store.SearchToolsFallback("brainstorm", "", "", DomainUserLand)
	if err != nil {
		t.Fatalf("SearchToolsFallback: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("brainstorm should be visible when query targets server name")
	}
	if results[0].URN != "brainstorm:think" {
		t.Fatalf("unexpected top result: %s", results[0].URN)
	}

	results, err = store.SearchToolsFallback("ideas", "", "", DomainUserLand)
	if err != nil {
		t.Fatalf("SearchToolsFallback: %v", err)
	}
	for _, r := range results {
		if r.Server == serverBrainstorm {
			t.Fatal("brainstorm should be masked when query does not target it")
		}
	}
}
