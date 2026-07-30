package handler

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/maccavelli/mcp-server-magictools/internal/client"
	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

func TestHandlerRegistration(t *testing.T) {
	tmpDir, _ := os.MkdirTemp("", "handler-test")
	defer os.RemoveAll(tmpDir)

	store, _ := db.NewStore(filepath.Join(tmpDir, "db"))
	defer store.Close()

	pidDir := filepath.Join(tmpDir, "pids")
	_ = os.MkdirAll(pidDir, 0755)
	cfg := &config.Config{}
	reg := client.NewWarmRegistry(pidDir, store, cfg)

	h := NewHandler(store, reg, cfg)
	s := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "1.0"}, &mcp.ServerOptions{})

	// 1. Register
	h.Register(s)

	// Final check: h was initialized
	if h.Registry == nil {
		t.Error("Handler initialization failed")
	}
}
