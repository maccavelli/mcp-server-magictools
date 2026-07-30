package handler

import (
	"encoding/json"
	"testing"
)

func TestInternalToolsInventoryJSON(t *testing.T) {
	var inventory []map[string]any
	err := json.Unmarshal(InternalToolsInventoryJSON, &inventory)
	if err != nil {
		t.Fatalf("Failed to parse InternalToolsInventoryJSON: %v", err)
	}

	if len(inventory) == 0 {
		t.Error("Inventory should not be empty")
	}

	for _, tool := range inventory {
		if _, ok := tool["name"].(string); !ok {
			t.Error("Tool missing valid name field")
		}
	}
}
