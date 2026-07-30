package handler

import (
	"os"
	"testing"
)

func TestOnOverridesUpdatedExtra(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	h.OnOverridesUpdated([]string{"test-server"})
}

func TestSyncHandlersExtraCoverage(t *testing.T) {
	h, store, _, tmpDir := newTestHandler(t)
	defer os.RemoveAll(tmpDir)
	defer store.Close()

	h.OnServerPromoted("test")
	h.OnServerDemoted("test")
	h.OnServerUpdated("test")
}
