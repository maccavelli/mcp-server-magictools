package hfsc

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mockSession struct{}

func (m *mockSession) Log(ctx context.Context, params *mcp.LoggingMessageParams) error { return nil }

func TestExecuteContinuousStreamWithMock(t *testing.T) {
	os.Setenv("MCP_ORCHESTRATOR_OWNED", "true")
	defer os.Setenv("MCP_ORCHESTRATOR_OWNED", "")

	session := &mockSession{}
	_, err := StreamHeavyPayload(context.Background(), session, "test.txt", "proj", "mod", bytes.NewReader([]byte("test content")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Wait a bit or execute executeContinuousStream directly
	executeContinuousStream(context.Background(), session, "session-id", bytes.NewReader([]byte("test content 2")))
}
