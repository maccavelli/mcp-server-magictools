package telemetry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRingBufferLengthPrefixedGauges(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.ring")

	rb, err := NewRingBuffer(path)
	if err != nil {
		t.Fatal(err)
	}
	defer rb.file.Close()

	payload := map[string]any{"search": map[string]any{"total_searches": 42}}
	if err := rb.WriteGauges(payload); err != nil {
		t.Fatal(err)
	}

	gaugeBytes, _, err := ReadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(gaugeBytes) == 0 {
		t.Fatal("expected gauge bytes")
	}
	if gaugeBytes[0] != '{' {
		t.Fatalf("expected JSON object, got %q", string(gaugeBytes[:min(20, len(gaugeBytes))]))
	}
	if !containsString(string(gaugeBytes), "total_searches") {
		t.Fatalf("payload missing total_searches: %s", gaugeBytes)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestRingBufferLegacyNullTerminatedFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "telemetry.ring")

	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0666)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(int64(RingTotalSize)); err != nil {
		t.Fatal(err)
	}
	header := make([]byte, RingHeaderSize)
	copy(header[0:4], []byte("RING"))
	if _, err := file.WriteAt(header, 0); err != nil {
		t.Fatal(err)
	}
	legacyJSON := []byte(`{"legacy":true}`)
	copy(header[16:], legacyJSON)
	if _, err := file.WriteAt(header[16:16+len(legacyJSON)], 16); err != nil {
		t.Fatal(err)
	}
	file.Close()

	gaugeBytes, _, err := ReadState(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(gaugeBytes) != `{"legacy":true}` {
		t.Fatalf("legacy fallback failed: %q", gaugeBytes)
	}
}
