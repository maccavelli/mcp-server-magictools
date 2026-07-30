package telemetry

import (
	"testing"
)

func TestIPCMetrics(t *testing.T) {
	TokenVelocity.LastTick.Store(0)
	v := ComputeTokenVelocity(100)
	if v != 0.0 {
		t.Errorf("Expected 0 on first call, got %v", v)
	}

	IDEThroughput.LastTick.Store(0)
	v2 := ComputeIDEThroughput(100)
	if v2 != 0.0 {
		t.Errorf("Expected 0 on first call, got %v", v2)
	}
}
