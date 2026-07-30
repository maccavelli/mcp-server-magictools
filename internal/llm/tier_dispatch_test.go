package llm

import (
	"context"
	"testing"
)

// thinkingTestProvider implements both Generate and GenerateThinking so it satisfies
// llmprovider.ThinkingProvider, recording which path was invoked.
type thinkingTestProvider struct {
	generateCalled bool
	thinkingCalled bool
}

func (p *thinkingTestProvider) Name() string { return "thinking-mock" }

func (p *thinkingTestProvider) Generate(_ context.Context, _ string) (string, error) {
	p.generateCalled = true
	return "fast", nil
}

func (p *thinkingTestProvider) GenerateThinking(_ context.Context, _ string) (string, error) {
	p.thinkingCalled = true
	return "thinking", nil
}

// TestGenerateForTier_ThinkingDispatch verifies the thinking tier invokes GenerateThinking
// on a thinking-capable provider, and the fast tier invokes Generate.
func TestGenerateForTier_ThinkingDispatch(t *testing.T) {
	p := &thinkingTestProvider{}
	out, err := generateForTier(context.Background(), p, "thinking", "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if out != "thinking" || !p.thinkingCalled || p.generateCalled {
		t.Errorf("thinking tier: out=%q thinkingCalled=%v generateCalled=%v", out, p.thinkingCalled, p.generateCalled)
	}

	p2 := &thinkingTestProvider{}
	out, err = generateForTier(context.Background(), p2, "fast", "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if out != "fast" || p2.thinkingCalled || !p2.generateCalled {
		t.Errorf("fast tier: out=%q thinkingCalled=%v generateCalled=%v", out, p2.thinkingCalled, p2.generateCalled)
	}
}

// TestGenerateForTier_NonThinkingProviderFallsBack verifies that a provider lacking
// thinking support uses the standard Generate path even when the thinking tier is asked.
func TestGenerateForTier_NonThinkingProviderFallsBack(t *testing.T) {
	p := &poolTestProvider{name: "plain", response: "plain-out"}
	out, err := generateForTier(context.Background(), p, "thinking", "prompt")
	if err != nil {
		t.Fatal(err)
	}
	if out != "plain-out" {
		t.Errorf("expected fallback to Generate, got %q", out)
	}
}
