package handler

import (
	"encoding/json"
	"testing"
)

func TestInventoryJSON(t *testing.T) {
	var tools []InternalTool
	if err := json.Unmarshal(InternalToolsInventoryJSON, &tools); err != nil {
		t.Fatalf("Failed to unmarshal InternalToolsInventoryJSON: %v", err)
	}

	if len(tools) == 0 {
		t.Error("InternalToolsInventoryJSON is empty")
	}
	if len(tools) != 18 {
		t.Errorf("expected exactly 18 internal tools, got %d — update inventory.go and this test together", len(tools))
	}

	// Verify key tools are present
	foundSync := false
	foundProxy := false
	for _, tool := range tools {
		if tool.Name == "sync_servers" {
			foundSync = true
		}
		if tool.Name == "call_proxy" {
			foundProxy = true
		}
	}

	if !foundSync {
		t.Error("sync_servers not found in inventory")
	}
	if !foundProxy {
		t.Error("call_proxy not found in inventory")
	}
}
