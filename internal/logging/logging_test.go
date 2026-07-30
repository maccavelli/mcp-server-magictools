package logging

import (
	"bytes"
	"io"
	"log/slog"
	"testing"
	"time"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"ERROR", slog.LevelError},
		{"WARN", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"INFO", slog.LevelInfo},
		{"DEBUG", slog.LevelDebug},
		{"TRACE", slog.Level(-8)},
		{"UNKNOWN", slog.LevelDebug},
	}

	for _, tt := range tests {
		got := ParseLogLevel(tt.input)
		if got != tt.expected {
			t.Errorf("ParseLogLevel(%q) = %v; want %v", tt.input, got, tt.expected)
		}
	}
}

func TestAsyncWriter(t *testing.T) {
	var buf bytes.Buffer
	aw := NewAsyncWriter(&buf, 100)

	msg := []byte("hello world")
	n, err := aw.Write(msg)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(msg) {
		t.Errorf("expected %d bytes written, got %d", len(msg), n)
	}

	// Wait a bit for the worker to process or just Close it
	_ = aw.Close()

	if buf.String() != string(msg) {
		t.Errorf("got %q; want %q", buf.String(), string(msg))
	}
}

func TestAsyncWriterDrop(t *testing.T) {
	// Use small capacity and slow writer to trigger drop logic
	aw := NewAsyncWriter(&slowWriter{}, 1)
	aw.maxDuration = 10 * time.Millisecond

	// First write fills the channel (or is picked up by worker immediately)
	_, _ = aw.Write([]byte("msg1"))
	// Second write might fill the channel if the first one is still being processed
	_, _ = aw.Write([]byte("msg2"))
	// Third write should trigger the timeout/drop
	_, _ = aw.Write([]byte("msg3"))

	_ = aw.Close()
}

type slowWriter struct{}

func (s *slowWriter) Write(p []byte) (n int, err error) {
	time.Sleep(50 * time.Millisecond)
	return len(p), nil
}

func TestSetupGlobalLogger(t *testing.T) {
	// Should not panic
	levelVar := new(slog.LevelVar)
	levelVar.Set(slog.LevelDebug)

	logger := SetupGlobalLogger(nil, "test.log", "text", levelVar, false, false, io.Discard, nil)
	if logger == nil {
		t.Error("expected logger")
	}

	logger2 := SetupGlobalLogger(nil, "test.log", "json", levelVar, false, false, io.Discard, nil)
	if logger2 == nil {
		t.Error("expected logger")
	}
}
