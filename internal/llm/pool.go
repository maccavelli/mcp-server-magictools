package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/maccavelli/mcplib/llmprovider"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

// Typed errors for programmatic classification (go.dev/doc/effective-go).
// Core provider errors are re-exported from the shared library.
var (
	ErrRateLimited         = llmprovider.ErrRateLimited
	ErrProviderUnavailable = llmprovider.ErrProviderUnavailable
	ErrAuthFailure         = llmprovider.ErrAuthFailure
	ErrTokenThreshold      = errors.New("llm: server token threshold exceeded")
	ErrThinkingDisabled    = errors.New("llm: thinking tier not configured")
)

// AuditEntry records a single LLM call for the ring buffer.
type AuditEntry struct {
	Timestamp  time.Time `json:"ts"`
	Server     string    `json:"server"`
	Tier       string    `json:"tier"`
	Model      string    `json:"model"`
	LatencyMs  int64     `json:"latency_ms"`
	TokensUsed int       `json:"tokens_used"`
	Error      string    `json:"error,omitempty,omitzero"`
}

// Request is the payload for Pool.Generate.
type Request struct {
	Prompt     string `json:"prompt"`
	MaxTokens  int    `json:"max_tokens,omitempty,omitzero"`
	ServerName string `json:"server_name"`
}

// Response is the result from Pool.Generate.
type Response struct {
	Text       string `json:"text"`
	TokensUsed int    `json:"tokens_used"`
	Model      string `json:"model"`
	LatencyMs  int64  `json:"latency_ms"`
}

// PoolMetrics exposes pool state for self_check and /llm/status.
type PoolMetrics struct {
	BackplaneStatus  string           `json:"backplane_status"`
	FastModel        string           `json:"fast_model"`
	ThinkingModel    string           `json:"thinking_model"`
	TotalTokens      int64            `json:"total_tokens_consumed"`
	TokenSpendThresh int64            `json:"token_spend_thresh"`
	PerServer        map[string]int64 `json:"per_server_tokens"`
	RecentAudit      []AuditEntry     `json:"recent_audit"`
}

// Pool centralizes LLM provider management with burst-aware rate limiting,
// per-server token tracking, and an audit ring buffer.
type Pool struct {
	fast       Provider   // existing provider/api_key
	fastModel  string     // specific model name
	thinking   Provider   // thinking_provider/key (may be nil)
	thinkModel string     // specific thinking model name
	fallbacks  []Provider // pre-created fallback models (same provider, different models)

	client      *http.Client  // shared across both tiers
	limiter     *rate.Limiter // burst-aware RPM (hydrator + sub-servers)
	sem         chan struct{} // concurrency semaphore (sub-server only)
	poolTimeout time.Duration // per-request context ceiling (default 2.5m)

	tokens map[string]*atomic.Int64 // per-server tracking
	total  atomic.Int64             // global counter
	thresh int                      // per-server soft token threshold (advisory, not enforced globally)

	audit    [100]AuditEntry
	auditIdx atomic.Int32
	mu       sync.RWMutex // RLock for provider reads, Lock for provider swap + getServerTokens
}

// NewPool creates a pool from the config's IntelligenceEngine settings.
func NewPool(cfg *config.Config) (*Pool, error) {
	ie := cfg.Intelligence

	transport := &http.Transport{
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   2 * time.Minute, // Tightest timeout bound — bounds external provider API call
	}

	// Fast tier (required)
	fast, err := createProvider(ie.Provider, ie.APIKey, ie.Model, tierOpts(httpClient, ie.APIURL)...)
	if err != nil {
		return nil, fmt.Errorf("llm pool: fast tier init failed: %w", err)
	}

	// Thinking tier (optional — nil if unconfigured)
	var thinking Provider
	if ie.ThinkingProvider != "" && ie.ThinkingAPIKey != "" && ie.ThinkingModel != "" {
		thinking, err = createProvider(ie.ThinkingProvider, ie.ThinkingAPIKey, ie.ThinkingModel, tierOpts(httpClient, ie.ThinkingAPIURL)...)
		if err != nil {
			slog.Warn("llm pool: thinking tier init failed, disabled", "error", err)
		}
	} else {
		slog.Info("llm pool: thinking tier not configured, disabled")
	}

	maxConcurrent := ie.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 3
	}

	maxBurst := ie.MaxBurstPerSecond
	if maxBurst <= 0 {
		maxBurst = 2
	}

	maxRPM := ie.MaxRPM
	if maxRPM <= 0 {
		maxRPM = 60 // Default: 1 request/second sustained rate
	}

	thresh := ie.SubServerTokenMax
	if thresh <= 0 {
		thresh = 500000
	}

	poolTimeout := time.Duration(ie.TimeoutSeconds) * time.Second
	if poolTimeout <= 0 {
		poolTimeout = 150 * time.Second // Default 2.5m — intermediate in timeout hierarchy
	}

	// Pre-create fallback providers from config (same provider, different models)
	var fallbacks []Provider
	for _, fbModel := range ie.FallbackModels {
		fb, fbErr := createProvider(ie.Provider, ie.APIKey, fbModel, llmprovider.WithHTTPClient(httpClient))
		if fbErr != nil {
			slog.Warn("llm pool: fallback model init failed, skipping",
				"model", fbModel, "error", fbErr)
			continue
		}
		fallbacks = append(fallbacks, fb)
		slog.Info("llm pool: fallback model registered", "model", fbModel)
	}

	return &Pool{
		fast:        fast,
		fastModel:   ie.Model,
		thinking:    thinking,
		thinkModel:  ie.ThinkingModel,
		fallbacks:   fallbacks,
		client:      httpClient,
		limiter:     rate.NewLimiter(rate.Limit(float64(maxRPM)/60.0), maxBurst), // sustained RPM + burst
		sem:         make(chan struct{}, maxConcurrent),
		poolTimeout: poolTimeout,
		tokens:      make(map[string]*atomic.Int64),
		thresh:      thresh,
	}, nil
}

// RestoreTokens initializes the atomic counters from the long-term BadgerDB cache on boot.
func (p *Pool) RestoreTokens(total int64, perServer map[string]int64) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.total.Store(total)
	for srv, count := range perServer {
		v := &atomic.Int64{}
		v.Store(count)
		p.tokens[srv] = v
	}
}

// Generate dispatches a prompt to the requested tier with full rate limiting,
// concurrency control, token tracking, and auditing.
func (p *Pool) Generate(ctx context.Context, tier string, req *Request) (*Response, error) {
	// 0. Per-pool context ceiling — intermediate timeout bound (provider 2m < pool 2.5m < client 3m)
	ctx, cancel := context.WithTimeout(ctx, p.poolTimeout)
	defer cancel()

	// 1. Acquire concurrency semaphore
	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return nil, fmt.Errorf("llm pool: semaphore wait cancelled: %w", ctx.Err())
	}

	// 2. Burst-aware rate gate
	if err := p.limiter.Wait(ctx); err != nil {
		return nil, fmt.Errorf("llm pool: rate limiter wait cancelled: %w", err)
	}

	// 3. Per-server soft token threshold check (advisory — not enforced globally)
	serverTokens := p.getServerTokens(req.ServerName)
	if serverTokens.Load() >= int64(p.thresh) {
		return nil, fmt.Errorf("llm pool: server %q exceeded threshold (%d): %w",
			req.ServerName, p.thresh, ErrTokenThreshold)
	}

	// 4. Read current provider under RLock (hot-reload safe)
	p.mu.RLock()
	var provider Provider
	var model string
	switch tier {
	case tierThinking:
		if p.thinking == nil {
			p.mu.RUnlock()
			return nil, ErrThinkingDisabled
		}
		provider = p.thinking
		model = provider.Name() + " (thinking)"
	default:
		provider = p.fast
		model = provider.Name() + " (fast)"
	}
	p.mu.RUnlock()

	// 5. Execute LLM call with context propagation
	slog.Info("llm pool: dispatching request",
		"server", req.ServerName, "tier", tier, "model", model)
	start := time.Now()
	text, err := generateForTier(ctx, provider, tier, req.Prompt)
	latency := time.Since(start).Milliseconds()

	// 5a. Fallback on non-rate-limit errors (5xx, timeout, connectivity)
	// MUST NOT fallback on 429 — Gemini rate limits are per-API-key, not per-model.
	if err != nil && !errors.Is(err, ErrRateLimited) && len(p.fallbacks) > 0 {
		p.mu.RLock()
		fbs := p.fallbacks
		p.mu.RUnlock()

		for i, fb := range fbs {
			slog.Warn("llm pool: primary failed, trying fallback",
				"fallback_index", i, "fallback_model", fb.Name(),
				"primary_error", err)
			start = time.Now()
			text, err = fb.Generate(ctx, req.Prompt)
			latency = time.Since(start).Milliseconds()
			if err == nil {
				model = fb.Name() + " (fallback)"
				slog.Info("llm pool: fallback succeeded",
					"fallback_model", fb.Name(), "latency_ms", latency)
				break
			}
		}
	}

	// 6. Write audit entry (lock-free — atomic index on fixed-size array)
	entry := AuditEntry{
		Timestamp:  time.Now(),
		Server:     req.ServerName,
		Tier:       tier,
		Model:      model, // captured under RLock before the call
		LatencyMs:  latency,
		TokensUsed: estimateTokens(text),
	}
	if err != nil {
		entry.Error = err.Error()
	}
	idx := p.auditIdx.Add(1) % 100
	p.audit[idx] = entry

	if err != nil {
		slog.Warn("llm pool: generate failed",
			"server", req.ServerName, "tier", tier, "latency_ms", latency, "error", err)
		return nil, fmt.Errorf("llm pool: generate failed: %w", err)
	}

	// 7. Increment per-server + global token counters (atomic)
	tokens := entry.TokensUsed
	serverTokens.Add(int64(tokens))
	p.total.Add(int64(tokens))

	slog.Info("llm pool: request completed",
		"server", req.ServerName, "tier", tier, "model", model,
		"latency_ms", latency, "tokens", tokens)

	return &Response{
		Text:       text,
		TokensUsed: tokens,
		Model:      model,
		LatencyMs:  latency,
	}, nil
}

// NewRateLimitedProvider returns a Provider clone for the hydrator daemon.
// The clone shares the pool's rate limiter but runs independently.
func (p *Pool) NewRateLimitedProvider() Provider {
	p.mu.Lock()
	defer p.mu.Unlock()
	return &rateLimitedProvider{
		inner:   p.fast,
		limiter: p.limiter,
	}
}

// Metrics returns current pool state for observability.
func (p *Pool) Metrics() PoolMetrics {
	p.mu.Lock()
	defer p.mu.Unlock()

	fastModel := ""
	if p.fast != nil {
		fastModel = p.fastModel
	}
	thinkModel := ""
	if p.thinking != nil {
		thinkModel = p.thinkModel
	}

	perServer := make(map[string]int64)
	for k, v := range p.tokens {
		perServer[k] = v.Load()
	}

	// Collect last 10 audit entries
	var recent []AuditEntry
	idx := int(p.auditIdx.Load())
	for i := range 10 {
		pos := (idx - i + 100) % 100
		e := p.audit[pos]
		if e.Timestamp.IsZero() {
			break
		}
		recent = append(recent, e)
	}

	return PoolMetrics{
		BackplaneStatus:  backplaneEnabled,
		FastModel:        fastModel,
		ThinkingModel:    thinkModel,
		TotalTokens:      p.total.Load(),
		TokenSpendThresh: int64(p.thresh),
		PerServer:        perServer,
		RecentAudit:      recent,
	}
}

// Reload atomically swaps providers from updated config.
// Implements hot-reload support via config.ChangeHandler.
func (p *Pool) Reload(cfg *config.Config) {
	ie := cfg.Intelligence

	newFast, err := createProvider(ie.Provider, ie.APIKey, ie.Model, llmprovider.WithHTTPClient(p.client))
	if err != nil {
		slog.Error("llm pool: hot-reload fast tier failed, keeping old provider", "error", err)
		return
	}

	// Thinking tier: distinguish error (preserve old) from intentional disable (set nil)
	var newThinking Provider
	var thinkingErr error
	if ie.ThinkingProvider != "" && ie.ThinkingAPIKey != "" && ie.ThinkingModel != "" {
		newThinking, thinkingErr = createProvider(ie.ThinkingProvider, ie.ThinkingAPIKey, ie.ThinkingModel, tierOpts(p.client, ie.ThinkingAPIURL)...)
		if thinkingErr != nil {
			slog.Warn("llm pool: hot-reload thinking tier failed, keeping old provider", "error", thinkingErr)
		}
	}

	p.mu.Lock()
	p.fast = newFast
	p.fastModel = ie.Model
	if thinkingErr == nil {
		// Only swap thinking provider on success or intentional disable (newThinking == nil)
		p.thinking = newThinking
		p.thinkModel = ie.ThinkingModel
	}
	// On thinkingErr != nil: preserve the old working thinking provider
	p.mu.Unlock()

	slog.Info("llm pool: providers reloaded from updated config")
}

// Close drains the semaphore, closes connections, and logs shutdown.
func (p *Pool) Close() {
	if tr, ok := p.client.Transport.(*http.Transport); ok {
		tr.CloseIdleConnections()
	}
	slog.Info("llm pool: shutting down")
}

// HandleGenerate is the HTTP handler for POST /llm/generate.
func (p *Pool) HandleGenerate(w http.ResponseWriter, r *http.Request) {
	p.handleLLMRequest(w, r, "fast")
}

// HandleThinking is the HTTP handler for POST /llm/generate-thinking.
func (p *Pool) HandleThinking(w http.ResponseWriter, r *http.Request) {
	p.handleLLMRequest(w, r, tierThinking)
}

// HandleStatus is the HTTP handler for GET /llm/status.
func (p *Pool) HandleStatus(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	encodeJSONOrWarn(w, p.Metrics())
}

func (p *Pool) handleLLMRequest(w http.ResponseWriter, r *http.Request, tier string) {
	// Request body limit: 256KB
	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := p.Generate(r.Context(), tier, &req)
	if err != nil {
		switch {
		case errors.Is(err, ErrRateLimited):
			w.Header().Set("Retry-After", "2")
			writeJSONErrorTyped(w, http.StatusTooManyRequests, err.Error(), "rate_limited", 2)
		case errors.Is(err, ErrTokenThreshold):
			writeJSONErrorTyped(w, http.StatusTooManyRequests, err.Error(), "rate_limited", 0)
		case errors.Is(err, ErrAuthFailure):
			writeJSONErrorTyped(w, http.StatusUnauthorized, err.Error(), "auth_failure", 0)
		case errors.Is(err, ErrThinkingDisabled):
			writeJSONError(w, http.StatusNotFound, err.Error())
		case errors.Is(err, ErrProviderUnavailable):
			writeJSONErrorTyped(w, http.StatusServiceUnavailable, err.Error(), "unavailable", 0)
		default:
			writeJSONErrorTyped(w, http.StatusInternalServerError, err.Error(), "error", 0)
		}
		return
	}

	w.Header().Set("Content-Type", "application/json")
	encodeJSONOrWarn(w, resp)
}

// getServerTokens returns the atomic counter for a server, creating it lazily.
func (p *Pool) getServerTokens(server string) *atomic.Int64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if v, ok := p.tokens[server]; ok {
		return v
	}
	v := &atomic.Int64{}
	p.tokens[server] = v
	return v
}

// rateLimitedProvider wraps a Provider with the pool's shared rate limiter.
type rateLimitedProvider struct {
	inner   Provider
	limiter *rate.Limiter
}

func (r *rateLimitedProvider) Name() string { return r.inner.Name() }
func (r *rateLimitedProvider) Generate(ctx context.Context, prompt string) (string, error) {
	if err := r.limiter.Wait(ctx); err != nil {
		return "", fmt.Errorf("rate limiter wait: %w", err)
	}
	return r.inner.Generate(ctx, prompt)
}

// createProvider constructs a Provider from config strings.
func createProvider(providerName, apiKey, model string, opts ...llmprovider.ProviderOption) (Provider, error) {
	return llmprovider.NewProvider(providerName, apiKey, model, opts...)
}

// tierOpts builds the provider options for one tier. The endpoint is honoured
// when configured: Ollama and the four gateway providers are reached at an
// address the user chooses, and the wizard has always collected it. It was
// never passed through until MADR 0004 deviation D6.
func tierOpts(client *http.Client, baseURL string) []llmprovider.ProviderOption {
	opts := []llmprovider.ProviderOption{llmprovider.WithHTTPClient(client)}
	if u := strings.TrimSpace(baseURL); u != "" {
		opts = append(opts, llmprovider.WithBaseURL(u))
	}
	return opts
}

// generateForTier dispatches to the provider's extended-thinking path for the thinking
// tier when the provider supports it (ThinkingProvider), so the thinking model genuinely
// reasons with thinking enabled rather than running a plain completion. The fast tier and
// any provider lacking thinking support use the standard Generate path.
func generateForTier(ctx context.Context, provider Provider, tier, prompt string) (string, error) {
	if tier == tierThinking {
		if tp, ok := provider.(llmprovider.ThinkingProvider); ok {
			return tp.GenerateThinking(ctx, prompt)
		}
	}
	return provider.Generate(ctx, prompt)
}

// estimateTokens provides a rough token count (~4 chars per token).
func estimateTokens(text string) int {
	return len(text) / 4
}

func writeJSONError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	encodeJSONOrWarn(w, map[string]string{keyError: msg})
}

// writeJSONErrorTyped includes type and retry_after fields for programmatic
// error classification by the shared client (5-class error taxonomy).
func writeJSONErrorTyped(w http.ResponseWriter, code int, msg, errType string, retryAfter int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	encodeJSONOrWarn(w, map[string]any{
		keyError:      msg,
		"type":        errType,
		"retry_after": retryAfter,
	})
}
