package telemetry

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

// flusherRunning reports whether a StartMetricsFlusher goroutine is alive.
func flusherRunning() bool {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Contains(string(buf[:n]), "telemetry.StartMetricsFlusher.func1")
}

// useOwnMetricsQueue installs a test-owned proxy metrics queue and, on cleanup, closes it,
// waits for the flusher goroutine bound to it to exit, then restores the package queue.
func useOwnMetricsQueue(t *testing.T) {
	t.Helper()
	orig := proxyMetricsQueue
	q := make(chan ProxyMetricEvent, 16)
	proxyMetricsQueue = q
	t.Cleanup(func() {
		close(q)
		deadline := time.Now().Add(5 * time.Second)
		for flusherRunning() && time.Now().Before(deadline) {
			time.Sleep(5 * time.Millisecond)
		}
		if flusherRunning() {
			t.Error("metrics flusher goroutine did not exit after its queue was closed")
		}
		proxyMetricsQueue = orig
	})
}

func TestProxyMetrics(t *testing.T) {
	useOwnMetricsQueue(t)
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
