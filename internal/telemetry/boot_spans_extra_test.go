package telemetry

import (
	"fmt"
	"testing"
	"time"
)

func TestBootSpans(t *testing.T) {
	bt := GlobalBootTracer
	bt.Start()

	if bt.TotalDuration() < 0 {
		t.Error("expected non-negative duration")
	}

	bt.Record("server1", "phase1", time.Millisecond, nil)
	bt.Record("server2", "phase2", time.Millisecond, fmt.Errorf("error"))

	spans := bt.Spans()
	if len(spans) != 2 {
		t.Errorf("expected 2 spans, got %d", len(spans))
	}
}

func TestProxyRelayTracker(t *testing.T) {
	rt := GlobalRelayTracker
	rt.maxSize = 5 // force rotation

	stats := rt.Stats()
	if stats["count"] != 0 {
		t.Error("expected 0 count initially")
	}

	for i := int64(1); i <= 10; i++ {
		rt.Record(i * 10)
	}

	stats = rt.Stats()
	if stats["count"].(int) != 4 {
		t.Errorf("expected 4 count after rotation, got %v", stats["count"])
	}

	avg := stats["avg"].(int64)
	if avg == 0 {
		t.Error("expected non-zero avg")
	}
}
