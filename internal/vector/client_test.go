package vector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func TestNewEmbedderFromConfig(t *testing.T) {
	// disabled
	cfg := &config.Config{}
	cfg.Intelligence.EmbeddingProvider = ""
	if e := NewEmbedderFromConfig(cfg); e != nil {
		t.Error("expected nil")
	}

	// invalid
	cfg.Intelligence.EmbeddingProvider = "invalid"
	cfg.Intelligence.EmbeddingAPIKey = "key"
	if e := NewEmbedderFromConfig(cfg); e != nil {
		t.Error("expected nil")
	}

	// gemini
	cfg.Intelligence.EmbeddingProvider = "gemini"
	cfg.Intelligence.EmbeddingAPIKey = "key"
	if e := NewEmbedderFromConfig(cfg); e == nil || e.Provider() != "gemini" {
		t.Error("expected gemini")
	}

	// openai
	cfg.Intelligence.EmbeddingProvider = "openai"
	if e := NewEmbedderFromConfig(cfg); e == nil || e.Provider() != "openai" {
		t.Error("expected openai")
	}

	// voyage
	cfg.Intelligence.EmbeddingProvider = "voyage"
	if e := NewEmbedderFromConfig(cfg); e == nil || e.Provider() != "voyage" {
		t.Error("expected voyage")
	}

	// ollama
	cfg.Intelligence.EmbeddingProvider = "ollama"
	if e := NewEmbedderFromConfig(cfg); e == nil || e.Provider() != "ollama" {
		t.Error("expected ollama")
	}
}

func TestOllamaEmbedder(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"embeddings": [[0.1, 0.2, 0.3]]}`))
	}))
	defer ts.Close()

	e := &OllamaEmbedder{
		apiURL: ts.URL,
		model:  "test-model",
		apiKey: "test-key",
	}

	res, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(res) != 3 {
		t.Errorf("expected 3 dims, got %d", len(res))
	}
}

func TestGeminiEmbedder(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"embedding": {"values": [0.1, 0.2, 0.3]}}`))
	}))
	defer ts.Close()

	origClient := httpClient
	httpClient = ts.Client()
	defer func() { httpClient = origClient }()

	e := &GeminiEmbedder{
		apiKey: "key",
		model:  "embedding-2",
		dims:   3,
	}

	// Because Gemini URL is hardcoded, we can't easily override it via struct fields.
	// But we can intercept the request if we mock the RoundTripper on the client.
	// Let's create a custom transport to rewrite the URL.
	transport := &customTransport{base: ts.URL}
	httpClient = &http.Client{Transport: transport}

	ctx := WithTaskType(context.Background(), "RETRIEVAL_DOCUMENT")
	res, err := e.Embed(ctx, "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(res) != 3 {
		t.Errorf("expected 3 dims, got %d", len(res))
	}
}

func TestOpenAIEmbedder(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": [{"embedding": [0.1, 0.2, 0.3]}]}`))
	}))
	defer ts.Close()

	origClient := httpClient
	transport := &customTransport{base: ts.URL}
	httpClient = &http.Client{Transport: transport}
	defer func() { httpClient = origClient }()

	e := &OpenAIEmbedder{
		apiKey: "key",
		model:  "text-embedding-3-small",
		dims:   3,
	}

	res, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(res) != 3 {
		t.Errorf("expected 3 dims, got %d", len(res))
	}
}

func TestVoyageEmbedder(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"data": [{"embedding": [0.1, 0.2, 0.3]}]}`))
	}))
	defer ts.Close()

	origClient := httpClient
	transport := &customTransport{base: ts.URL}
	httpClient = &http.Client{Transport: transport}
	defer func() { httpClient = origClient }()

	e := &VoyageEmbedder{
		apiKey: "key",
		model:  "voyage-3",
	}

	res, err := e.Embed(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(res) != 3 {
		t.Errorf("expected 3 dims, got %d", len(res))
	}
}

type customTransport struct {
	base string
}

func (c *customTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite URL to test server
	newReq := req.Clone(req.Context())
	newReq.URL.Scheme = "http"
	newReq.URL.Host = c.base[7:] // remove "http://"

	// Keep path and query params, but send to our test server
	return http.DefaultTransport.RoundTrip(newReq)
}
