package client

import (
	"testing"
	"time"
)

func TestGetLLMTraces_Empty(t *testing.T) {
	reg := &WarmRegistry{}
	traces := reg.GetLLMTraces(10)
	if traces != nil {
		t.Errorf("expected nil, got %d traces", len(traces))
	}
}

func TestGetLLMTraces_WithEntries(t *testing.T) {
	reg := &WarmRegistry{}

	// Simulate handleLLMTrace writing entries
	for i := range 5 {
		reg.llmTraceMu.Lock()
		idx := reg.llmTraceIdx % len(reg.llmTraceRing)
		reg.llmTraceRing[idx] = LLMTraceEntry{
			Server:     "test-server",
			Tier:       "fast",
			Model:      "gemini",
			LatencyMs:  int64(100 + i*10),
			Tokens:     50 + i,
			ReceivedAt: time.Now(),
		}
		reg.llmTraceIdx++
		reg.llmTraceMu.Unlock()
	}

	traces := reg.GetLLMTraces(3)
	if len(traces) != 3 {
		t.Fatalf("expected 3 traces, got %d", len(traces))
	}

	// Most recent first
	if traces[0].LatencyMs != 140 {
		t.Errorf("expected latest entry (140ms), got %d", traces[0].LatencyMs)
	}
}

func TestGetLLMTraces_MaxExceedsAvailable(t *testing.T) {
	reg := &WarmRegistry{}

	reg.llmTraceMu.Lock()
	idx := reg.llmTraceIdx % len(reg.llmTraceRing)
	reg.llmTraceRing[idx] = LLMTraceEntry{
		Server:     "only-one",
		Tier:       "thinking",
		Model:      "claude",
		LatencyMs:  200,
		Tokens:     100,
		ReceivedAt: time.Now(),
	}
	reg.llmTraceIdx++
	reg.llmTraceMu.Unlock()

	traces := reg.GetLLMTraces(50)
	if len(traces) != 1 {
		t.Errorf("expected 1 trace, got %d", len(traces))
	}
}

func TestGetLLMTraces_RingWrap(t *testing.T) {
	reg := &WarmRegistry{}

	// Write more than ring buffer size (50)
	for i := range 55 {
		reg.llmTraceMu.Lock()
		idx := reg.llmTraceIdx % len(reg.llmTraceRing)
		reg.llmTraceRing[idx] = LLMTraceEntry{
			Server:     "wrap-test",
			Tier:       "fast",
			Model:      "gemini",
			LatencyMs:  int64(i),
			Tokens:     i,
			ReceivedAt: time.Now(),
		}
		reg.llmTraceIdx++
		reg.llmTraceMu.Unlock()
	}

	traces := reg.GetLLMTraces(10)
	if len(traces) != 10 {
		t.Fatalf("expected 10 traces, got %d", len(traces))
	}

	// Most recent should be entry 54 (latency=54)
	if traces[0].LatencyMs != 54 {
		t.Errorf("expected latest entry (54ms), got %d", traces[0].LatencyMs)
	}
}
