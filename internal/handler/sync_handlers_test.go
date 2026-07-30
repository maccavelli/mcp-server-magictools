package handler

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestSyncServers(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	ctx := context.Background()

	// 1. Test Global Sync (empty arguments)
	req1 := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "sync_servers",
			Arguments: json.RawMessage(`{}`),
		},
	}

	res1, err := h.SyncServers(ctx, req1)
	if err != nil {
		t.Fatalf("SyncServers (global) failed: %v", err)
	}
	if len(res1.Content) == 0 {
		t.Fatalf("expected results for global sync, got none")
	}

	// 2. Test Targeted Sync (specific servers)
	req2 := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "sync_servers",
			Arguments: json.RawMessage(`{"names": "magictools"}`),
		},
	}

	res2, err := h.SyncServers(ctx, req2)
	if err != nil {
		t.Fatalf("SyncServers (targeted) failed: %v", err)
	}
	if len(res2.Content) == 0 {
		t.Fatalf("expected results for targeted sync, got none")
	}
}

func TestWakeServers(t *testing.T) {
	h, _, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "wake_servers",
			Arguments: json.RawMessage(`{}`),
		},
	}

	res, err := h.WakeServers(ctx, req)
	if err != nil {
		t.Fatalf("WakeServers failed: %v", err)
	}

	if len(res.Content) == 0 {
		t.Fatalf("expected results, got none")
	}
}

func TestReloadServers(t *testing.T) {
	h, _, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)

	ctx := context.Background()

	// 1. Test full reload
	req := &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "reload_servers",
			Arguments: json.RawMessage(`{}`),
		},
	}

	res, err := h.ReloadServers(ctx, req)
	if err != nil {
		t.Fatalf("ReloadServers failed: %v", err)
	}

	if len(res.Content) == 0 {
		t.Fatalf("expected results, got none")
	}

	// 2. Test selective reload (empty list is fine)
	req = &mcp.CallToolRequest{
		Params: &mcp.CallToolParamsRaw{
			Name:      "reload_servers",
			Arguments: json.RawMessage(`{"names": "test-server"}`),
		},
	}

	if _, err = h.ReloadServers(ctx, req); err != nil {
		t.Fatalf("ReloadServers failed: %v", err)
	}
}

func TestServerEvents(t *testing.T) {
	h, _, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)

	h.OnServerPromoted("test-server")
	h.OnServerDemoted("test-server")
	h.OnServerUpdated("test-server")
}
