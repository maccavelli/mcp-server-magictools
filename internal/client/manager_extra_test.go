package client

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestExtraManagerMethods(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "magictools-manager-extra-*")
	defer os.RemoveAll(tempDir)

	store, _ := db.NewStore(tempDir)
	defer store.Close()

	cfg := &config.Config{}
	wr := NewWarmRegistry(tempDir, store, cfg)

	_ = wr.HasServer("unknown")

	wr.RequestState("unknown", StatusOffline)
	_ = wr.GetFailedServers()

	// CallProxy with an unknown server to trigger fast path exit
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	_, _ = wr.CallProxy(ctx, "unknown", "tool", nil, 1*time.Second)

	wr.Boot(ctx, []config.ServerConfig{{Name: "test", Command: "echo"}})
	wr.AuditGlobalRegistry()
	wr.DisconnectAll()
	wr.PruneOrphans()
}
