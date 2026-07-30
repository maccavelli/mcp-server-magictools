package db

import (
	"fmt"
	"strings"
	"testing"
)

// TestBatchSaveTools_LargeBatchNoTxnTooBig is the BDG-2 regression: a batch whose
// aggregate tool-record bytes exceed Badger's single-transaction size limit must
// still commit (via the WriteBatch path) instead of failing with ErrTxnTooBig.
func TestBatchSaveTools_LargeBatchNoTxnTooBig(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	const n = 40
	// ~324 KiB per record × 40 ≈ 13 MiB, well over the 8 MiB memtable that bounded
	// the old single-transaction BatchSaveTools.
	big := strings.Repeat("schema-field-data-", 18000)
	records := make([]*ToolRecord, n)
	for i := range records {
		urn := fmt.Sprintf("big:tool%d", i)
		records[i] = &ToolRecord{
			URN: urn, Name: fmt.Sprintf("tool%d", i), Server: "big", Category: "x",
			InputSchema: map[string]any{"blob": big},
		}
	}

	if err := store.BatchSaveTools(records, nil); err != nil {
		t.Fatalf("BatchSaveTools failed on a large batch (ErrTxnTooBig regression?): %v", err)
	}

	for i := range records {
		urn := fmt.Sprintf("big:tool%d", i)
		if _, err := store.GetTool(urn); err != nil {
			t.Errorf("tool %s missing after large batch save: %v", urn, err)
		}
	}
}
