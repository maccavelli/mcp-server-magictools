package logging

import (
	"log/slog"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMcpLogHandlerSessions(t *testing.T) {
	level := new(slog.LevelVar)
	handler := NewMcpLogHandler(level)
	handler.SetSession(nil)
	handler.AddSession(&mcp.ServerSession{})
	handler.RemoveSession("test")
	// handler.StartReaper(nil) // It might panic if db is nil but let's test if we can
}
