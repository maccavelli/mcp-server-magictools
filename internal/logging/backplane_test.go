package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestBackplaneLogger(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, nil)
	slog.SetDefault(slog.New(handler))

	l := NewBackplaneLogger()
	l.Log(SPAWN, "test-server", "spawning...")

	if !strings.Contains(buf.String(), "boot milestone") || !strings.Contains(buf.String(), "test-server") {
		t.Errorf("bad log output: %s", buf.String())
	}

	buf.Reset()
	summary := BootSummary{
		TotalAttempted: 1,
		Success:        1,
		StartTime:      time.Now().Add(-1 * time.Second),
	}
	l.Report(summary)

	if !strings.Contains(buf.String(), "global boot sequence complete") || !strings.Contains(buf.String(), "total=1") {
		t.Errorf("bad summary output: %s", buf.String())
	}
}
