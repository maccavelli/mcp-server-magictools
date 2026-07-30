package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/maccavelli/mcplib/llmprovider"
)

func TestGenerateWithRetry_Cancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	// mock server returning error to force loop
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	defer ts.Close()

	p, _ := llmprovider.NewClaude("key", "model", llmprovider.WithBaseURL(ts.URL))

	// cancel immediately to hit the ctx timeout in the select block
	cancel()

	_, err := GenerateWithRetry(ctx, p, "say hello", 1, 10*time.Millisecond)
	if err == nil || !errors.Is(err, context.Canceled) {
		t.Errorf("expected context canceled, got %v", err)
	}
}
