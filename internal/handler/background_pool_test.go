package handler

import (
	"sync/atomic"
	"testing"
	"time"
)

// TestSubmitBackground_RunsTask verifies the PROXY-L6 pool actually executes
// submitted tasks (and survives a panicking one without taking down a worker).
func TestSubmitBackground_RunsTask(t *testing.T) {
	// A panicking task must not kill the worker.
	submitBackground(func() { panic("boom") })

	var ran atomic.Bool
	done := make(chan struct{})
	submitBackground(func() {
		ran.Store(true)
		close(done)
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("background task did not run within 2s")
	}
	if !ran.Load() {
		t.Error("task flag not set")
	}
}
