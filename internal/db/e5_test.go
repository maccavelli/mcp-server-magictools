package db

import (
	"fmt"
	"testing"
)

// TestPruneSyntheticDocs_CapsGrowth is the BLV-11 regression: synthetic
// (intent_cache / hfsc_trace) documents must be trimmed to the retention cap
// instead of accreting in the Bleve index forever.
func TestPruneSyntheticDocs_CapsGrowth(t *testing.T) {
	si, err := NewSearchIndex(t.TempDir())
	if err != nil {
		t.Fatalf("NewSearchIndex: %v", err)
	}

	for i := range 5 {
		if err := si.IndexSyntheticIntent(fmt.Sprintf("prompt-%d", i), []string{"a:tool"}); err != nil {
			t.Fatal(err)
		}
		if err := si.IndexHFSCTrace(HFSCTraceDocument{
			ID: fmt.Sprintf("trace-%d", i), Server: "a", ReceivedAt: int64(i + 1),
		}); err != nil {
			t.Fatal(err)
		}
	}

	// Keep 2 of each type → 3 of each (6 total) are deleted.
	deleted, err := si.pruneSyntheticDocs(2)
	if err != nil {
		t.Fatalf("pruneSyntheticDocs: %v", err)
	}
	if deleted != 6 {
		t.Errorf("expected 6 deleted (3 per type), got %d", deleted)
	}

	// Idempotent: now at the cap, a second prune deletes nothing.
	if deleted2, _ := si.pruneSyntheticDocs(2); deleted2 != 0 {
		t.Errorf("expected 0 on second prune, got %d", deleted2)
	}
}

// TestPruneSynergyWeights_CapsCount is the BDG-9 regression: the synergy weight
// set must be capped, evicting the lowest-value transitions and keeping the most
// reinforced ones.
func TestPruneSynergyWeights_CapsCount(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	// 20 distinct hashes; hash-i has i+1 successes (weight ascending in i).
	for i := range 20 {
		h := fmt.Sprintf("hash-%d", i)
		for j := 0; j <= i; j++ {
			store.RecordSynergy(h, true)
		}
	}

	if evicted := store.evictSynergyWeightsBeyondCap(5); evicted != 15 {
		t.Errorf("expected 15 evicted (20-5), got %d", evicted)
	}

	// The 5 highest-weight hashes (15..19) must survive.
	for i := 15; i < 20; i++ {
		if succ, _ := store.GetSynergy(fmt.Sprintf("hash-%d", i)); succ == 0 {
			t.Errorf("high-weight hash-%d was evicted", i)
		}
	}
	// The lowest-weight hash must be gone.
	if succ, _ := store.GetSynergy("hash-0"); succ != 0 {
		t.Error("lowest-weight hash-0 should have been evicted")
	}
}
