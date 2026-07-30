package telemetry

import (
	"context"
	"log/slog"
	"os"
	"testing"
)

func TestRingBufferHandlerExtra(t *testing.T) {
	level := new(slog.LevelVar)
	coreHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})

	f, _ := os.CreateTemp("", "rb-*")
	f.Close()
	defer os.Remove(f.Name())

	rb, _ := NewRingBuffer(f.Name())

	rbh := NewRingBufferHandler(coreHandler, rb)

	// Test Enabled
	if !rbh.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("expected true")
	}

	// Test WithAttrs
	attrHandler := rbh.WithAttrs([]slog.Attr{{Key: "test", Value: slog.StringValue("val")}})
	if attrHandler == nil {
		t.Error("expected handler")
	}

	// Test WithGroup
	groupHandler := rbh.WithGroup("testGroup")
	if groupHandler == nil {
		t.Error("expected handler")
	}

	// Write a few records to trigger shouldDebounce and deduplication logic
	rec1 := slog.Record{Level: slog.LevelInfo, Message: "test msg 1"}
	rec2 := slog.Record{Level: slog.LevelInfo, Message: "test msg 1"}
	rec3 := slog.Record{Level: slog.LevelInfo, Message: "test msg 2"}

	rbh.Handle(context.Background(), rec1)
	rbh.Handle(context.Background(), rec2)
	rbh.Handle(context.Background(), rec3)

	// Trigger duplicate trace for shouldDebounce
	dupRec := slog.Record{Level: slog.LevelInfo, Message: "Processing tool"}
	dupRec.Add("tool_id", slog.IntValue(42))
	rbh.Handle(context.Background(), dupRec)

	dupRec2 := slog.Record{Level: slog.LevelInfo, Message: "Processing tool"}
	dupRec2.Add("tool_id", slog.Float64Value(42.0))
	rbh.Handle(context.Background(), dupRec2)

	// test "SUCCESS" or "FAIL"
	succRec := slog.Record{Level: slog.LevelInfo, Message: "SUCCESS"}
	succRec.Add("tool_id", slog.IntValue(42))
	rbh.Handle(context.Background(), succRec)
}
