package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"

	"golang.org/x/time/rate"
)

// poolTestProvider implements Provider for pool tests without real API calls.
type poolTestProvider struct {
	name      string
	latencyMs int
	response  string
	failErr   error
}

func (m *poolTestProvider) Name() string { return m.name }
func (m *poolTestProvider) Generate(_ context.Context, prompt string) (string, error) {
	if m.latencyMs > 0 {
		time.Sleep(time.Duration(m.latencyMs) * time.Millisecond)
	}
	if m.failErr != nil {
		return "", m.failErr
	}
	return m.response, nil
}

// newTestPool creates a Pool with mock providers for testing.
func newTestPool(fast, thinking *poolTestProvider, maxConcurrent, burstPerSec, threshold int) *Pool {
	var thinkingProvider Provider
	var thinkModel string
	if thinking != nil {
		thinkingProvider = thinking
		thinkModel = thinking.name
	}

	var fastModel string
	if fast != nil {
		fastModel = fast.name
	}

	p := &Pool{
		fast:        fast,
		fastModel:   fastModel,
		thinking:    thinkingProvider,
		thinkModel:  thinkModel,
		client:      &http.Client{},
		limiter:     rate.NewLimiter(rate.Every(time.Second), burstPerSec),
		sem:         make(chan struct{}, maxConcurrent),
		poolTimeout: 30 * time.Second, // Test-appropriate timeout
		tokens:      make(map[string]*atomic.Int64),
		thresh:      threshold,
	}
	return p
}

func TestPoolGenerate_FastTier(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "Hello from fast tier"}
	pool := newTestPool(fast, nil, 3, 10, 500000)

	resp, err := pool.Generate(context.Background(), "fast", &Request{
		Prompt:     "test prompt",
		ServerName: "socratic-thinker",
	})
	if err != nil {
		t.Fatalf("Generate fast failed: %v", err)
	}
	if resp.Text != "Hello from fast tier" {
		t.Errorf("unexpected response text: %q", resp.Text)
	}
	if resp.Model != "test-gemini (fast)" {
		t.Errorf("unexpected model: %q", resp.Model)
	}
}

func TestPoolGenerate_ThinkingTier(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "fast"}
	thinking := &poolTestProvider{name: "test-claude", response: "deep analysis"}
	pool := newTestPool(fast, thinking, 3, 10, 500000)

	resp, err := pool.Generate(context.Background(), "thinking", &Request{
		Prompt:     "analyze this code",
		ServerName: "evolve-plan",
	})
	if err != nil {
		t.Fatalf("Generate thinking failed: %v", err)
	}
	if resp.Text != "deep analysis" {
		t.Errorf("unexpected text: %q", resp.Text)
	}
	if resp.Model != "test-claude (thinking)" {
		t.Errorf("unexpected model: %q", resp.Model)
	}
}

func TestPoolGenerate_ThinkingDisabled(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "fast"}
	pool := newTestPool(fast, nil, 3, 10, 500000)

	_, err := pool.Generate(context.Background(), "thinking", &Request{
		Prompt:     "test",
		ServerName: "test-server",
	})
	if err == nil {
		t.Fatal("expected ErrThinkingDisabled")
	}
	if !errors.Is(err, ErrThinkingDisabled) {
		t.Errorf("expected ErrThinkingDisabled, got: %v", err)
	}
}

func TestPoolGenerate_TokenThreshold(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "ok"}
	pool := newTestPool(fast, nil, 3, 10, 100) // low threshold

	// Pre-load tokens to exceed threshold
	counter := pool.getServerTokens("heavy-server")
	counter.Store(200)

	_, err := pool.Generate(context.Background(), "fast", &Request{
		Prompt:     "test",
		ServerName: "heavy-server",
	})
	if err == nil {
		t.Fatal("expected ErrTokenThreshold")
	}
}

func TestPoolMetrics(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "ok"}
	thinking := &poolTestProvider{name: "test-claude", response: "deep"}
	pool := newTestPool(fast, thinking, 3, 10, 500000)

	// Generate some calls to populate audit
	for range 3 {
		_, _ = pool.Generate(context.Background(), "fast", &Request{
			Prompt:     "test",
			ServerName: "server-a",
		})
	}
	_, _ = pool.Generate(context.Background(), "thinking", &Request{
		Prompt:     "analyze",
		ServerName: "server-b",
	})

	metrics := pool.Metrics()
	if metrics.BackplaneStatus != "ENABLED" {
		t.Errorf("expected ENABLED status, got %q", metrics.BackplaneStatus)
	}
	if metrics.FastModel != "test-gemini" {
		t.Errorf("unexpected fast model: %q", metrics.FastModel)
	}
	if metrics.ThinkingModel != "test-claude" {
		t.Errorf("unexpected thinking model: %q", metrics.ThinkingModel)
	}
	if metrics.TotalTokens == 0 {
		t.Error("expected non-zero total tokens")
	}
	if len(metrics.PerServer) != 2 {
		t.Errorf("expected 2 server entries, got %d", len(metrics.PerServer))
	}
	if len(metrics.RecentAudit) != 4 {
		t.Errorf("expected 4 audit entries, got %d", len(metrics.RecentAudit))
	}
}

func TestPoolConcurrency(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", latencyMs: 10, response: "ok"}
	pool := newTestPool(fast, nil, 2, 100, 500000) // max 2 concurrent

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	for range 5 {
		wg.Go(func() {
			_, err := pool.Generate(context.Background(), "fast", &Request{
				Prompt:     "concurrent test",
				ServerName: "test-server",
			})
			if err != nil {
				errors <- err
			}
		})
	}
	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent generate failed: %v", err)
	}
}

// ── HTTP Handler Tests ──────────────────────────────────────────────────

func TestHandleStatus(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "ok"}
	pool := newTestPool(fast, nil, 3, 10, 500000)

	rec := httptest.NewRecorder()
	pool.HandleStatus(rec, httptest.NewRequest("GET", "/llm/status", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var metrics PoolMetrics
	if err := json.NewDecoder(rec.Body).Decode(&metrics); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if metrics.BackplaneStatus != "ENABLED" {
		t.Errorf("expected ENABLED, got %q", metrics.BackplaneStatus)
	}
}

func TestHandleGenerate(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "generated text"}
	pool := newTestPool(fast, nil, 3, 10, 500000)

	body, _ := json.Marshal(Request{Prompt: "test prompt", ServerName: "test-server"})
	req := httptest.NewRequest("POST", "/llm/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	pool.HandleGenerate(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp Response
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp.Text != "generated text" {
		t.Errorf("unexpected text: %q", resp.Text)
	}
}

func TestHandleThinking_Disabled(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "ok"}
	pool := newTestPool(fast, nil, 3, 10, 500000) // no thinking tier

	body, _ := json.Marshal(Request{Prompt: "test", ServerName: "test-server"})
	req := httptest.NewRequest("POST", "/llm/generate-thinking", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	pool.HandleThinking(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleGenerate_TokenThreshold(t *testing.T) {
	fast := &poolTestProvider{name: "test-gemini", response: "ok"}
	pool := newTestPool(fast, nil, 3, 10, 100)

	// Exhaust token budget
	counter := pool.getServerTokens("budget-server")
	counter.Store(200)

	body, _ := json.Marshal(Request{Prompt: "test", ServerName: "budget-server"})
	req := httptest.NewRequest("POST", "/llm/generate", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	pool.HandleGenerate(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestPoolReload(t *testing.T) {
	// Verify reload doesn't crash and updates the provider
	fast := &poolTestProvider{name: "test-gemini", response: "old"}
	pool := newTestPool(fast, nil, 3, 10, 500000)

	// Reload is expected to fail (no real API key) but should not panic
	// We just verify it doesn't crash
	metrics := pool.Metrics()
	if metrics.FastModel != "test-gemini" {
		t.Errorf("pre-reload model mismatch: %q", metrics.FastModel)
	}

	// Try reload with empty config, it should skip
	importConfig := &config.Config{}
	pool.Reload(importConfig)
}

func TestNewPool(t *testing.T) {
	cfg := &config.Config{}
	cfg.Intelligence.Provider = "gemini"
	cfg.Intelligence.Model = "gemini-2.5-flash"
	cfg.Intelligence.APIKey = "fake-key"

	pool, _ := NewPool(cfg)
	if pool == nil {
		t.Fatal("expected pool")
	}
	defer pool.Close()
}

func TestRestoreTokens(t *testing.T) {
	pool := newTestPool(nil, nil, 1, 1, 100)
	pool.getServerTokens("test-server").Store(200)

	pool.RestoreTokens(1000, map[string]int64{"test-server": 50})

	if val := pool.getServerTokens("test-server").Load(); val != 50 {
		t.Errorf("expected 50 tokens, got %d", val)
	}
}

func TestNewRateLimitedProvider(t *testing.T) {
	fast := &poolTestProvider{name: "test"}
	pool := newTestPool(fast, nil, 1, 1, 100)
	provider := pool.NewRateLimitedProvider()
	if provider == nil {
		t.Fatal("expected provider")
	}
	if provider.Name() != "test" {
		t.Errorf("unexpected name: %s", provider.Name())
	}

	_, err := provider.Generate(context.Background(), "test prompt")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPoolClose(t *testing.T) {
	pool := newTestPool(nil, nil, 1, 1, 100)
	pool.Close()
	// Should not panic
}

func TestEstimateTokens(t *testing.T) {
	tests := []struct {
		input    string
		expected int
	}{
		{"", 0},
		{"test", 1},
		{"hello world this is a longer text", 8},
	}

	for _, tt := range tests {
		got := estimateTokens(tt.input)
		if got != tt.expected {
			t.Errorf("estimateTokens(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}
