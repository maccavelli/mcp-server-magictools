package hnsw

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

func randVec(rng *rand.Rand, dim int) []float32 {
	v := make([]float32, dim)
	v[0] = 1 // guarantee non-zero norm (CosineDistance of a zero vector is NaN)
	for i := 1; i < dim; i++ {
		v[i] = rng.Float32()
	}
	return v
}

// TestDelete_EmptyThenSearch_NoPanic is the HNW-4 regression: deleting every
// node must trim empty layers so a subsequent Search returns empty cleanly
// instead of dereferencing a nil layer entry.
func TestDelete_EmptyThenSearch_NoPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	g := NewGraph[string]()
	g.Rng = rand.New(rand.NewSource(7))

	const n = 40
	keys := make([]string, n)
	for i := 0; i < n; i++ {
		keys[i] = fmt.Sprintf("k%d", i)
		g.Add(Node[string]{Key: keys[i], Value: randVec(rng, 4)})
	}

	// Delete every node.
	for _, k := range keys {
		g.Delete(k)
	}
	if g.Len() != 0 {
		t.Fatalf("expected empty graph, got Len=%d", g.Len())
	}

	// Must not panic and must return no results.
	res := g.Search(randVec(rng, 4), 5)
	if len(res) != 0 {
		t.Errorf("expected no results from empty graph, got %d", len(res))
	}

	// Searching after partial deletes (mixed layer occupancy) must also be safe.
	for i := 0; i < n; i++ {
		g.Add(Node[string]{Key: keys[i], Value: randVec(rng, 4)})
	}
	for i := 0; i < n; i += 2 {
		g.Delete(keys[i])
	}
	_ = g.Search(randVec(rng, 4), 5) // no panic
}

// TestImport_RejectsOversizedLength is the HNW-3 regression: a corrupt length
// in the blob must produce a clean error, never an unbounded allocation.
func TestImport_RejectsOversizedLength(t *testing.T) {
	name, ok := distanceFuncToName(CosineDistance)
	if !ok {
		t.Fatal("cosine distance not registered")
	}

	var buf bytes.Buffer
	if _, err := multiBinaryWrite(&buf, encodingVersion, 16, float64(0.25), 20, name); err != nil {
		t.Fatal(err)
	}
	// Absurd layer count — must be rejected by the decode ceiling.
	if _, err := binaryWrite(&buf, 1<<40); err != nil {
		t.Fatal(err)
	}

	g := NewGraph[string]()
	if err := g.Import(&buf); err == nil {
		t.Error("expected an out-of-bounds error for an oversized layer count, got nil")
	}
}

// TestImport_TruncatedBlob_NoPanic is the HNW-3 robustness check: importing a
// truncated export at any cut point must return an error, not panic.
func TestImport_TruncatedBlob_NoPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	g := NewGraph[string]()
	for i := 0; i < 60; i++ {
		g.Add(Node[string]{Key: fmt.Sprintf("k%d", i), Value: randVec(rng, 4)})
	}
	var full bytes.Buffer
	if err := g.Export(&full); err != nil {
		t.Fatal(err)
	}
	b := full.Bytes()

	step := len(b) / 25
	if step < 1 {
		step = 1
	}
	for cut := 1; cut < len(b); cut += step {
		g2 := NewGraph[string]()
		// A panic here fails the test (which is the point). An error is acceptable.
		_ = g2.Import(bytes.NewReader(b[:cut]))
	}
}

// TestExport_Deterministic is the HNW-12 regression: exporting the same graph
// twice must produce byte-identical blobs despite Go's randomized map iteration.
// Determinism is what lets a content hash over the export be a stable rebuild
// sentinel.
func TestExport_Deterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(99))
	g := NewGraph[string]()
	g.Rng = rand.New(rand.NewSource(99))
	for i := 0; i < 200; i++ {
		g.Add(Node[string]{Key: fmt.Sprintf("k%d", i), Value: randVec(rng, 8)})
	}

	var a, b bytes.Buffer
	if err := g.Export(&a); err != nil {
		t.Fatal(err)
	}
	if err := g.Export(&b); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Fatalf("two exports of the same graph differ: %d vs %d bytes", a.Len(), b.Len())
	}

	// A round-trip through Import must re-export identically too — the decoded
	// graph holds the same nodes/neighbors, so sorted encoding reproduces the blob.
	g2 := NewGraph[string]()
	if err := g2.Import(bytes.NewReader(a.Bytes())); err != nil {
		t.Fatal(err)
	}
	var c bytes.Buffer
	if err := g2.Export(&c); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), c.Bytes()) {
		t.Fatalf("re-export after import differs from original: %d vs %d bytes", a.Len(), c.Len())
	}
}

// TestAdd_MultiLayerReplace_NoPanic is the HNW-2 regression: re-adding existing
// keys that live in multiple layers must scope the replace to each layer without
// dangling the elevator (which previously nil-dereferenced).
func TestAdd_MultiLayerReplace_NoPanic(t *testing.T) {
	rng := rand.New(rand.NewSource(42))
	g := NewGraph[string]()
	g.Rng = rand.New(rand.NewSource(42))

	const n = 500
	for i := 0; i < n; i++ {
		g.Add(Node[string]{Key: fmt.Sprintf("k%d", i), Value: randVec(rng, 8)})
	}
	if len(g.layers) < 2 {
		t.Fatalf("test setup did not produce a multi-layer graph (layers=%d)", len(g.layers))
	}
	want := g.Len()

	// Re-add every key with a fresh vector — replaces across all occupied layers.
	for i := 0; i < n; i++ {
		g.Add(Node[string]{Key: fmt.Sprintf("k%d", i), Value: randVec(rng, 8)})
	}
	if g.Len() != want {
		t.Errorf("Len changed across replace: got %d want %d", g.Len(), want)
	}

	// Graph remains searchable.
	res := g.Search(randVec(rng, 8), 10)
	if len(res) == 0 {
		t.Error("expected search results after multi-layer replace")
	}
}
