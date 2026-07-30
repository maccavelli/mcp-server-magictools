package ui

import "testing"

func TestEnableVirtualTerminalProcessing(t *testing.T) {
	if err := EnableVirtualTerminalProcessing(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
