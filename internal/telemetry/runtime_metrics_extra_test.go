package telemetry

import (
	"os"
	"testing"
)

func TestRuntimeMetrics(t *testing.T) {
	os.Setenv("GOMEMLIMIT", "1GiB")
	defer os.Unsetenv("GOMEMLIMIT")

	snap := CaptureRuntime()
	if snap.GoMemLimitMB != 1024 {
		t.Errorf("expected 1024, got %v", snap.GoMemLimitMB)
	}

	parseMemLimit("256MiB")
	parseMemLimit("128KiB")
	parseMemLimit("1000")
}
