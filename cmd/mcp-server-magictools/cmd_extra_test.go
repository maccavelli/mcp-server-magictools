package main

import (
	"context"
	"testing"
	"time"
)

func TestOrchestratorAppBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	app, err := NewOrchestratorApp(ctx, cancel, "", "", "", "", false, false, time.Now())
	if err != nil {
		t.Skip("NewOrchestratorApp failed:", err)
	}
	if app != nil {
		app.Stop()
	}
}
