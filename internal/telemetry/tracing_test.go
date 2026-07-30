package telemetry

import (
	"context"
	"testing"
)

func TestTracing(t *testing.T) {
	id := NewCorrelationID()
	if id == "" {
		t.Fatal("expected new correlation ID")
	}

	ctx := context.Background()
	ctx = WithCorrelationID(ctx, id)

	id2 := GetCorrelationID(ctx)
	if id2 != id {
		t.Errorf("expected %s, got %s", id, id2)
	}

	// Test empty context
	id3 := GetCorrelationID(context.Background())
	if id3 != "" {
		t.Errorf("expected empty ID, got %s", id3)
	}
}
