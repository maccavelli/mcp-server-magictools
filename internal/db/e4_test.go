package db

import (
	"context"
	"testing"
)

// TestSearchIndex_ExactNameBoost is the BLV-5 regression: an exact tool-name query
// must rank the tool whose name matches over one that merely mentions the words in
// its description, now that the exact/wildcard legs target the name_exact keyword.
func TestSearchIndex_ExactNameBoost(t *testing.T) {
	si, err := NewSearchIndex(t.TempDir())
	if err != nil {
		t.Fatalf("NewSearchIndex: %v", err)
	}
	defer si.Close()

	if err := si.IndexRecord(ToBleveDoc(&ToolRecord{
		URN: "a:foo_bar", Name: "foo_bar", Server: "a", Description: "alpha tool",
	})); err != nil {
		t.Fatal(err)
	}
	if err := si.IndexRecord(ToBleveDoc(&ToolRecord{
		URN: "a:other", Name: "other", Server: "a", Description: "mentions foo bar repeatedly foo bar",
	})); err != nil {
		t.Fatal(err)
	}

	res, err := si.Search(context.Background(), "foo_bar", "", "", DomainSystem)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if res.Total == 0 {
		t.Fatal("no results")
	}
	if res.Hits[0].ID != "a:foo_bar" {
		t.Errorf("exact-name match should rank first; got %q", res.Hits[0].ID)
	}
}

// TestFlushMetrics_LatencyAverageNotFrozen is the BDG-6 regression: the rolling
// latency average must track a sustained shift instead of freezing near its early
// value (the old integer-division form truncated late updates to zero).
func TestFlushMetrics_LatencyAverageNotFrozen(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	const urn = "m:tool"
	// 5 low-latency calls then 45 high-latency calls, each in its own flush window.
	// True cumulative mean = (5*10 + 45*1000)/50 = 901.
	for round := range 50 {
		lat := int64(10)
		if round >= 5 {
			lat = 1000
		}
		store.UpdateToolMetrics(urn, true)
		store.IncrementToolCalls(urn, lat)
		store.FlushMetrics()
	}

	intel, err := store.GetIntelligence(urn)
	if err != nil || intel == nil {
		t.Fatalf("GetIntelligence: %v", err)
	}
	if intel.Metrics.TotalCalls != 50 {
		t.Errorf("TotalCalls = %d, want 50", intel.Metrics.TotalCalls)
	}
	// The frozen implementation would stay far below the true mean (~901).
	if intel.Metrics.AvgLatencyMs < 500 {
		t.Errorf("AvgLatencyMs = %d, want it to track the shift toward ~901 (>500)", intel.Metrics.AvgLatencyMs)
	}
}
