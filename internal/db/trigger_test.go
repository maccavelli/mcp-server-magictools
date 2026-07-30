package db

import (
	"context"
	"testing"
)

func TestTriggers(t *testing.T) {
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer s.Close()

	// 1. Save Triggers
	if err := s.SaveTrigger("refactor", "go-modernizer"); err != nil {
		t.Fatalf("SaveTrigger failed: %v", err)
	}
	if err := s.SaveTrigger("git", "glab"); err != nil {
		t.Fatalf("SaveTrigger failed: %v", err)
	}

	// 2. Get Triggers
	triggers, err := s.GetTriggers()
	if err != nil {
		t.Fatalf("GetTriggers failed: %v", err)
	}
	if len(triggers) != 2 {
		t.Errorf("expected 2 triggers, got %d", len(triggers))
	}
	if triggers["refactor"] != "go-modernizer" {
		t.Errorf("expected go-modernizer, got %s", triggers["refactor"])
	}

	// 3. Analyze Intent
	// Word boundaries test
	servers := s.AnalyzeIntent(context.Background(), "Can you refactor this code?")
	if len(servers) != 1 || servers[0] != "go-modernizer" {
		t.Errorf("expected [go-modernizer], got %v", servers)
	}

	// Match multiple
	servers = s.AnalyzeIntent(context.Background(), "refactor this and push to git")
	if len(servers) != 2 {
		t.Errorf("expected 2 servers, got %d (%v)", len(servers), servers)
	}

	// No match
	servers = s.AnalyzeIntent(context.Background(), "hello world")
	if len(servers) != 0 {
		t.Errorf("expected 0 servers, got %v", servers)
	}

	// Case insensitivity
	servers = s.AnalyzeIntent(context.Background(), "REFACTOR")
	if len(servers) != 1 || servers[0] != "go-modernizer" {
		t.Errorf("expected [go-modernizer] (case insensitive), got %v", servers)
	}
}
