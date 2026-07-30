package telemetry

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestProxyMetrics(t *testing.T) {
	DispatchProxyMetric(ProxyMetricEvent{
		ProxyCorrelationID: "test-id",
		ToolURN:            "test-urn",
		Timestamp:          time.Now(),
	})

	// atomic: the background flusher goroutine writes this while the test reads it.
	var called atomic.Bool
	save := func(key string, val []byte, ttl time.Duration) error {
		called.Store(true)
		return nil
	}

	StartMetricsFlusher(save)
	time.Sleep(10 * time.Millisecond) // let the flusher run

	if !called.Load() {
		t.Error("expected save function to be called")
	}
}
