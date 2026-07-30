package db

import (
	"bytes"
	"errors"
	"testing"

	"github.com/dgraph-io/badger/v4"
)

// rawGet returns the raw bytes stored under a Badger key, or nil if absent.
func rawGet(t *testing.T, s *Store, key string) []byte {
	t.Helper()
	var out []byte
	err := s.DB.View(func(txn *badger.Txn) error {
		item, gErr := txn.Get([]byte(key))
		if gErr != nil {
			return gErr
		}
		return item.Value(func(v []byte) error { out = append([]byte{}, v...); return nil })
	})
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil
	}
	if err != nil {
		t.Fatalf("rawGet %s: %v", key, err)
	}
	return out
}

// TestSaveTool_VectorSeparateKey is the IDX-9 regression: the embedding vector is
// persisted under its own "vec:" key, so the persisted "tool:" blob (which the hot
// per-call UpdateToolUsage rewrites) stays small and vector-free. The caller's
// record is not mutated.
func TestSaveTool_VectorSeparateKey(t *testing.T) {
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	defer store.Close()

	rec := &ToolRecord{URN: "test:t1", Name: "t1", Server: "test", Category: "x", Vector: []float32{0.1, 0.2, 0.3}}
	if err := store.SaveTool(rec); err != nil {
		t.Fatalf("SaveTool: %v", err)
	}

	// Caller's record must not be mutated.
	if len(rec.Vector) != 3 {
		t.Errorf("SaveTool mutated the caller's Vector: %v", rec.Vector)
	}

	// Vector is retrievable under its own key.
	if got := store.GetToolVector("test:t1"); len(got) != 3 || got[0] != 0.1 {
		t.Errorf("GetToolVector = %v, want [0.1 0.2 0.3]", got)
	}

	// The persisted "tool:" blob does NOT embed the vector; the "vec:" key does.
	if blob := rawGet(t, store, "tool:test:t1"); bytes.Contains(blob, []byte(`"vector"`)) {
		t.Errorf("tool: blob should not embed the vector: %s", blob)
	}
	if len(rawGet(t, store, "vec:test:t1")) == 0 {
		t.Error("vec: key should hold the embedding")
	}

	// A usage bump still works and keeps the blob vector-free.
	store.UpdateToolUsage("test:t1")
	if blob := rawGet(t, store, "tool:test:t1"); bytes.Contains(blob, []byte(`"vector"`)) {
		t.Errorf("tool: blob must stay vector-free after a usage bump: %s", blob)
	}
}
