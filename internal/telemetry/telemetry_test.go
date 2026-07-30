package telemetry

import (
	"testing"
)

func TestMetrics_Record(t *testing.T) {
	tm := NewTracker()
	tm.AddLatency("devops", 500)
	tm.AddBytes("devops", 100, 1000, 50)
	tm.RecordFault("devops")

	m := tm.GetMetrics("devops")
	if m == nil {
		t.Fatal("expected metrics")
	}
	if m.Calls != 1 || m.TotalSpinupMs != 500 || m.BytesRaw != 1000 || m.BytesMinified != 50 || m.Faults != 1 || m.BytesSent != 100 {
		t.Errorf("metrics did not record properly: %+v", m)
	}
}

func TestMetrics_GetSessionStats(t *testing.T) {
	tm := NewTracker()
	tm.AddLatency("devops", 500)
	tm.AddLatency("plugin", 100)
	tm.AddBytes("devops", 100, 1000, 200) // saved = 800 => 200 tokens.  Used = (100+1000)/4 = 275
	tm.AddBytes("plugin", 0, 2000, 2000)  // saved = 0 => 0 tokens. Used = (0+2000)/4 = 500

	tm.RecordFault("plugin")

	stats := tm.GetSessionStats()
	if stats.Digest.TotalCalls != 2 {
		t.Errorf("expected 2 calls, got %d", stats.Digest.TotalCalls)
	}
	if stats.Digest.TotalFaults != 1 {
		t.Errorf("expected 1 fault, got %d", stats.Digest.TotalFaults)
	}
	if stats.Digest.TokensUsed != 775 {
		t.Errorf("expected 775 tokens used, got %d", stats.Digest.TokensUsed)
	}
	if stats.Digest.TokensSaved != 200 { // 800 / 4
		t.Errorf("expected 200 tokens saved, got %d", stats.Digest.TokensSaved)
	}
}

func TestEMATracker_Record(t *testing.T) {
	tracker := &EMATracker{}
	tracker.Record(100)
	if tracker.Count != 1 || tracker.TotalLatencies != 100 || tracker.EMA != 100 {
		t.Errorf("expected initial EMA to be exact: %+v", tracker)
	}
	tracker.Record(200)
	// Alpha 0.1: (0.1 * 200) + (0.9 * 100) = 20 + 90 = 110
	if tracker.Count != 2 || tracker.TotalLatencies != 300 || tracker.EMA != 110 {
		t.Errorf("expected updated EMA to be 110: %+v", tracker)
	}
}

func TestTracker_RecordSoftFailure(t *testing.T) {
	tm := NewTracker()
	tm.RecordSoftFailure("devops")
	tm.RecordSoftFailure("devops")

	m := tm.GetMetrics("devops")
	if m.SoftFailures != 2 {
		t.Errorf("expected 2 soft failures, got %d", m.SoftFailures)
	}
}

func TestTracker_GetAll(t *testing.T) {
	tm := NewTracker()
	tm.AddLatency("server1", 100)
	tm.AddLatency("server2", 200)

	all := tm.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 metrics in GetAll, got %d", len(all))
	}
	if all["server1"].TotalSpinupMs != 100 {
		t.Errorf("expected server1 spinup ms 100, got %d", all["server1"].TotalSpinupMs)
	}
}

func TestTracker_GetMetrics_NotFound(t *testing.T) {
	tm := NewTracker()
	m := tm.GetMetrics("unknown")
	if m != nil {
		t.Errorf("expected nil metrics for unknown server, got %+v", m)
	}
}
