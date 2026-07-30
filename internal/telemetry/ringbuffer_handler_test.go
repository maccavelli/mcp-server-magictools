package telemetry

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"
)

type mockSlogHandler struct{}

func (m *mockSlogHandler) Enabled(ctx context.Context, l slog.Level) bool  { return true }
func (m *mockSlogHandler) Handle(ctx context.Context, r slog.Record) error { return nil }
func (m *mockSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler        { return m }
func (m *mockSlogHandler) WithGroup(name string) slog.Handler              { return m }

func TestRingBufferHandler(t *testing.T) {
	tmpFile := "test_ring_buffer_handler.mmap"
	defer os.Remove(tmpFile)

	rb, err := NewRingBuffer(tmpFile)
	if err != nil {
		t.Fatalf("failed to create ring buffer: %v", err)
	}

	mock := &mockSlogHandler{}
	h := NewRingBufferHandler(mock, rb)

	// Test Enabled
	if !h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected enabled")
	}

	// Test Handle
	rec := slog.Record{
		Time:    time.Now(),
		Level:   slog.LevelInfo,
		Message: "test message",
	}
	rec.AddAttrs(slog.Int("tool_id", 42))

	err = h.Handle(context.Background(), rec)
	if err != nil {
		t.Errorf("Handle failed: %v", err)
	}

	// Test Debounce
	for range 10 {
		h.Handle(context.Background(), rec)
	}

	// Test WithAttrs and WithGroup
	h2 := h.WithAttrs([]slog.Attr{slog.String("key", "val")})
	if h2 == nil {
		t.Error("WithAttrs returned nil")
	}
	h3 := h.WithGroup("group")
	if h3 == nil {
		t.Error("WithGroup returned nil")
	}
}
