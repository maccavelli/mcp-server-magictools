package logging

import (
	"context"
	"log/slog"
	"testing"
)

func TestMcpLogHandlerSessionsReaper(t *testing.T) {
	level := new(slog.LevelVar)
	handler := NewMcpLogHandler(level)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	handler.StartReaper(ctx)
}
