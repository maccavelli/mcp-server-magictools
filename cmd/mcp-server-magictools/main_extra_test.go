package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func TestMiddlewaresAndHandlers(t *testing.T) {
	cfg, _ := config.New("", "")
	app := &OrchestratorApp{
		reg: client.NewWarmRegistry("", nil, nil),
		cfg: cfg,
	}

	// Test healthHandler
	app.reg.IsSynced.Store(true)
	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	app.healthHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}

	// Test middlewares
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})

	authHandler := app.polymorphicAuthMiddleware(next, "test-token")
	reqAuth := httptest.NewRequest("GET", "/", nil)
	rrAuth := httptest.NewRecorder()
	authHandler.ServeHTTP(rrAuth, reqAuth)

	llmAuthHandler := app.llmAuthMiddleware(next, "test-token")
	reqLLMAuth := httptest.NewRequest("GET", "/", nil)
	rrLLMAuth := httptest.NewRecorder()
	llmAuthHandler.ServeHTTP(rrLLMAuth, reqLLMAuth)

	readinessHandler := app.readinessMiddleware(next)
	reqReadiness := httptest.NewRequest("GET", "/", nil)
	rrReadiness := httptest.NewRecorder()
	readinessHandler.ServeHTTP(rrReadiness, reqReadiness)

	telemetryHandler := app.ideTelemetryMiddleware(next)
	reqTelemetry := httptest.NewRequest("GET", "/", nil)
	rrTelemetry := httptest.NewRecorder()
	telemetryHandler.ServeHTTP(rrTelemetry, reqTelemetry)
}
