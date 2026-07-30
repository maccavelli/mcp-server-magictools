package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestMcpLogHandlerHandle(t *testing.T) {
	level := new(slog.LevelVar)
	handler := NewMcpLogHandler(level)
	if handler.Enabled(context.Background(), slog.LevelInfo) != true {
		t.Fatal("expected true")
	}

	record := slog.Record{Level: slog.LevelInfo, Message: "test message"}
	handler.Handle(context.Background(), record)
	handler.WithAttrs([]slog.Attr{{Key: "k", Value: slog.StringValue("v")}})
	handler.WithGroup("g")
}

func TestMultiHandler(t *testing.T) {
	mh := NewMultiHandler(NewMcpLogHandler(new(slog.LevelVar)))
	mh.Enabled(context.Background(), slog.LevelInfo)
	mh.Handle(context.Background(), slog.Record{Level: slog.LevelInfo, Message: "test message"})
	mh.WithAttrs([]slog.Attr{{Key: "k", Value: slog.StringValue("v")}})
	mh.WithGroup("g")
}
