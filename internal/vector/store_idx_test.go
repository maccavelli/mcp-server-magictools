package vector

import (
	"context"
	"math"
	"testing"
)

// TestAddVector_RejectsBadVectors is the IDX-5 regression: zero-norm and
// non-finite vectors must be rejected (their cosine distance is NaN, which
// corrupts neighbor selection).
func TestAddVector_RejectsBadVectors(t *testing.T) {
	e := &Engine{graph: createHNSWGraph(), embedder: &mockEmbedder{}, expectedDims: 3}
	e.initialized = true

	if err := e.AddVector(context.Background(), "zero", []float32{0, 0, 0}); err == nil {
		t.Error("expected error for zero-norm vector")
	}
	if err := e.AddVector(context.Background(), "nan", []float32{1, float32(math.NaN()), 0}); err == nil {
		t.Error("expected error for NaN vector")
	}
	if err := e.AddVector(context.Background(), "inf", []float32{float32(math.Inf(1)), 0, 0}); err == nil {
		t.Error("expected error for Inf vector")
	}
	// A valid unit vector is still accepted.
	if err := e.AddVector(context.Background(), "ok", []float32{1, 0, 0}); err != nil {
		t.Errorf("valid vector rejected: %v", err)
	}
}
