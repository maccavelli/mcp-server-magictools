package db

import (
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// T6: a write that never returns does not hold Close past the drain bound, and Close does wait for the bound.
func TestStore_CloseDrainBoundIsHonoured(t *testing.T) {
	s, _ := openForCloseTest(t)
	s.drainWait = 300 * time.Millisecond

	inWrite := make(chan struct{})
	stuck := make(chan struct{})
	go func() {
		_ = s.UpdateWithRetry(func(_ *badger.Txn) error {
			close(inWrite)
			<-stuck
			return nil
		})
	}()
	<-inWrite

	closed := make(chan error, 1)
	start := time.Now()
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
		if d := time.Since(start); d < 250*time.Millisecond {
			t.Errorf("Close returned after %v, before the bound: it did not wait", d)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return within 10s: the drain is not bounded")
	}
	if !s.gate.timedOut.Load() {
		t.Error("the wait ran out with a write in flight, but the gate did not record it")
	}
	close(stuck)
}
