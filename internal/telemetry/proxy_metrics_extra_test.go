package telemetry

import (
	"testing"
	"time"
)

func TestProxyMetrics(t *testing.T) {
	DispatchProxyMetric(ProxyMetricEvent{
		ProxyCorrelationID: "test-id",
		ToolURN:            "test-urn",
		Timestamp:          time.Now(),
	})

	saved := make(chan struct{}, 1)
	save := func(key string, val []byte, ttl time.Duration) error {
		select {
		case saved <- struct{}{}:
		default:
		}
		return nil
	}

	StartMetricsFlusher(save)
	select {
	case <-saved:
	case <-time.After(time.Second):
		t.Fatal("expected save function to be called")
	}
}
