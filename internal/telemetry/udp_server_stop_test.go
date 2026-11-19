package telemetry

import (
	"sync"
	"testing"
)

// stopCatching calls s.Stop and returns what it panicked with, or nil.
func stopCatching(s *UDPServer) (panicked any) {
	defer func() { panicked = recover() }()
	s.Stop()
	return nil
}

// isClosed reports whether ch is closed, without blocking.
func isClosed(ch chan struct{}) bool {
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// TestUDPServerStopIsIdempotent covers the shutdown that reaches UDPServer.Stop twice (the graceful-shutdown path and
// the application's deferred Stop): the second call used to panic with "close of closed channel" after "graceful
// shutdown complete".
//
// The server value is built without a socket and starts no goroutine, so the test exercises only Stop: it binds no
// port, signals no process and scans none. Run it by name (-run '^TestUDPServerStopIsIdempotent$') to keep the other
// tests of the package, one of which binds a loopback socket, out of the run.
func TestUDPServerStopIsIdempotent(t *testing.T) {
	t.Run("a second call after the first does not panic and the channel stays closed", func(t *testing.T) {
		s := &UDPServer{cancelChan: make(chan struct{})}
		if isClosed(s.cancelChan) {
			t.Fatal("the cancel channel is closed before any Stop")
		}
		if p := stopCatching(s); p != nil {
			t.Fatalf("first Stop panicked: %v", p)
		}
		if !isClosed(s.cancelChan) {
			t.Fatal("the cancel channel is not closed after the first Stop")
		}
		if p := stopCatching(s); p != nil {
			t.Fatalf("second Stop panicked: %v", p)
		}
		if p := stopCatching(s); p != nil {
			t.Fatalf("third Stop panicked: %v", p)
		}
		if !isClosed(s.cancelChan) {
			t.Fatal("the cancel channel is not closed after repeated Stops")
		}
	})

	t.Run("a nil server is safe to stop", func(t *testing.T) {
		var s *UDPServer
		if p := stopCatching(s); p != nil {
			t.Fatalf("Stop on a nil server panicked: %v", p)
		}
	})

	t.Run("concurrent callers close the channel once and each returns after it is closed", func(t *testing.T) {
		const callers = 32
		s := &UDPServer{cancelChan: make(chan struct{})}
		start := make(chan struct{})
		panics := make([]any, callers)
		closedOnReturn := make([]bool, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Add(1)
			go func() {
				defer wg.Done()
				<-start
				panics[i] = stopCatching(s)
				// A caller that returns must be able to rely on Stop having finished, whoever ran it.
				closedOnReturn[i] = isClosed(s.cancelChan)
			}()
		}
		close(start)
		wg.Wait()
		for i := range callers {
			if panics[i] != nil {
				t.Errorf("caller %d panicked: %v", i, panics[i])
			}
			if !closedOnReturn[i] {
				t.Errorf("caller %d returned from Stop before the channel was closed", i)
			}
		}
		if !isClosed(s.cancelChan) {
			t.Fatalf("the cancel channel is not closed after %d concurrent Stops", callers)
		}
	})
}
