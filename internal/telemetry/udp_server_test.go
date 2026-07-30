package telemetry

import (
	"testing"
	"time"
)

func TestUDPServerLifecycle(t *testing.T) {
	srv, err := NewUDPServer()
	if err != nil {
		t.Skip("unable to bind UDP server", err)
		return
	}
	if srv == nil {
		t.Fatal("expected server")
	}
	ctx := t.Context()
	_ = ctx

	// Start server on random port
	go func() {
		srv.Start(func() map[string]any {
			return map[string]any{"test": "data"}
		})
	}()

	// Wait a moment for it to start
	time.Sleep(50 * time.Millisecond)

	port := srv.BoundPort()
	if port <= 0 {
		t.Error("expected valid bound port")
	}

	ports := GetTelemetryPorts()
	if len(ports) == 0 {
		t.Error("expected >0 bound ports")
	}

	active := srv.ActiveClients()
	if active < 0 {
		t.Error("active clients shouldn't be negative")
	}

	srv.Stop()
	time.Sleep(50 * time.Millisecond)
}
