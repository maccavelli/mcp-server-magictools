package client

import "testing"

// TestStripReservedWireKeys covers the wire-boundary backstop: _meta is removed from the
// argument payload just before it crosses to a sub-server, for every caller — including
// cascade/DAG steps that bypass the handler dispatch funnel.
func TestStripReservedWireKeys(t *testing.T) {
	t.Run("strips _meta, preserves real args", func(t *testing.T) {
		args := map[string]any{
			"path":  "/tmp",
			"_meta": map[string]any{"proxy_correlation_id": "corr-123"},
		}
		stripReservedWireKeys(args)
		if _, ok := args["_meta"]; ok {
			t.Errorf("_meta must be stripped at the wire boundary, got: %v", args)
		}
		if args["path"] != "/tmp" {
			t.Errorf("non-reserved arguments must be preserved, got: %v", args)
		}
	})

	t.Run("no _meta present is a no-op", func(t *testing.T) {
		args := map[string]any{"path": "/tmp"}
		stripReservedWireKeys(args)
		if len(args) != 1 || args["path"] != "/tmp" {
			t.Errorf("arguments must be untouched, got: %v", args)
		}
	})

	t.Run("nil map is safe", func(t *testing.T) {
		stripReservedWireKeys(nil) // must not panic
	})
}
