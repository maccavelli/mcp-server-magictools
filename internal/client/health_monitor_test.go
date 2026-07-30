package client

import (
	"context"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
)

func TestMonitorResources_Empty(t *testing.T) {
	m := &WarmRegistry{
		Servers: make(map[string]*SubServer),
	}
	// Should not panic
	m.MonitorResources()
}

func TestPingAll_Empty(t *testing.T) {
	m := &WarmRegistry{
		Servers: make(map[string]*SubServer),
	}
	// Should not panic
	m.PingAll(context.Background())
}

func TestHealthMonitor_Lifecycle(t *testing.T) {
	m := &WarmRegistry{
		Servers: make(map[string]*SubServer),
		Config:  &config.Config{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	// Start with very short interval for coverage
	go m.StartHealthMonitor(ctx, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	cancel()
}
