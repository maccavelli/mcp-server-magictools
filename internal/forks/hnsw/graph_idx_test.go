package hnsw

import "testing"

// TestAdd_ReplaceExistingKey is the IDX-4 regression: re-adding an existing key
// is a replace (Delete+reinsert keeps Len constant) and must not panic the
// invariant check.
func TestAdd_ReplaceExistingKey(t *testing.T) {
	g := NewGraph[string]()
	g.Add(Node[string]{Key: "a", Value: []float32{1, 0}})
	g.Add(Node[string]{Key: "b", Value: []float32{0, 1}})

	// Re-add an existing key with a new vector — must not panic.
	g.Add(Node[string]{Key: "a", Value: []float32{0.5, 0.5}})

	if g.Len() != 2 {
		t.Errorf("expected 2 nodes after replace, got %d", g.Len())
	}
	if _, ok := g.Lookup("a"); !ok {
		t.Error("replaced key should still be present")
	}
}
