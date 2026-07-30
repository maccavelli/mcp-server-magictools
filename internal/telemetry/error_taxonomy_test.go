package telemetry

import (
	"fmt"
	"testing"
)

func TestErrorRing(t *testing.T) {
	er := NewErrorRing(5)

	er.Record("server1", "id1", "connection reset by peer")
	er.Record("server2", "id2", "context deadline exceeded")
	er.Record("server3", "id3", "unknown error")

	all := er.GetAll()
	if len(all) != 3 {
		t.Errorf("expected 3 errors, got %d", len(all))
	}

	// Test classification
	etc := &ErrorTaxonomyCounters{}
	etc.Classify(fmt.Errorf("timeout occurred"))
	if etc.Timeout.Load() != 1 {
		t.Errorf("expected 1 timeout, got %d", etc.Timeout.Load())
	}

	etc.Classify(fmt.Errorf("VALIDATION_ERROR: bad args"))
	if etc.Validation.Load() != 1 {
		t.Errorf("expected 1 validation error, got %d", etc.Validation.Load())
	}
}
