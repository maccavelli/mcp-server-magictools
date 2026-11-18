package db

import (
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestStore_CloseWhileStoreGoroutinesStart: the four places that start one of the store's own goroutines
// (RecordSynergy's flush, SaveIntelligence's re-index, the L2 response-cache write, the post-flush tool refresh) keep
// starting while Close runs at a random point. Close must not panic (a start counted after Close's bgWg.Wait began
// panics with "sync: WaitGroup is reused before previous Wait has returned") and nothing may hang.
func TestStore_CloseWhileStoreGoroutinesStart(t *testing.T) {
	_ = captureLogs(t) // the callers log every refusal after Close; keep the test output readable
	const iterations = 100
	for it := 0; it < iterations; it++ {
		s, _ := openForCloseTest(t)
		rc := NewResponseCache()
		rc.SetStore(s)
		stop := make(chan struct{})
		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			starters []string
		)
		start := func(name string, f func(i int)) {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						starters = append(starters, fmt.Sprintf("%s panicked: %v", name, r))
						mu.Unlock()
					}
				}()
				for i := 0; ; i++ {
					select {
					case <-stop:
						return
					default:
					}
					f(i)
				}
			}()
		}
		start("RecordSynergy", func(i int) { s.RecordSynergy(fmt.Sprintf("h%d", i%32), i%2 == 0) })
		start("ResponseCache.Set", func(i int) { rc.Set(fmt.Sprintf("k%d", i%32), &mcp.CallToolResult{}, time.Hour) })
		start("SaveIntelligence", func(i int) { _ = s.SaveIntelligence(fmt.Sprintf("srv:t%d", i%8), &ToolIntelligence{}) })
		start("FlushMetrics", func(i int) {
			s.IncrementToolCalls(fmt.Sprintf("srv:t%d", i%8), 1)
			s.FlushMetrics()
		})

		time.Sleep(time.Duration(rand.Intn(2000)) * time.Microsecond)
		closePanic := func() (p any) {
			defer func() { p = recover() }()
			_ = s.Close()
			return nil
		}()
		close(stop)
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: a caller did not return within 10s of Close", it)
		}
		if closePanic != nil {
			t.Fatalf("iteration %d: Close panicked: %v", it, closePanic)
		}
		mu.Lock()
		if len(starters) > 0 {
			t.Fatalf("iteration %d: %s", it, starters[0])
		}
		mu.Unlock()
	}
}

const refusedStartLog = "database: background work not started: the store is closing"

// TestStore_StartAdmittedBeforeCloseIsCountedFirst: a start that passed the gate's check before Close shut it is
// counted in bgWg before Close can go on. The start is parked just after bgWg.Add (in countStart), inside its
// admission: Close must not get past shutting the gate (let alone return) until the start is released; then both
// complete.
//
// The parked-start part alone does not prove that the Add happens under the gate's mutex: with the Add moved after
// the mutex is released and the hook left inside, it still passes, and only the race detector reports the plant
// (review of 2026-11-18, "add-outside-mutex"). The hook therefore also checks, without -race and deterministically,
// that the mutex is held when the start is counted: countStart carries the hook with the Add, so moving the count
// outside the mutex moves the check with it.
func TestStore_StartAdmittedBeforeCloseIsCountedFirst(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	parked := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	var countedUnlocked bool
	hook := func() {
		once.Do(func() {
			if s.gate.mu.TryLock() { // succeeds only if the start was counted outside the gate's mutex
				s.gate.mu.Unlock()
				countedUnlocked = true
			}
			close(parked)
			<-release
		})
	}
	s.startHook.Store(&hook)

	started := make(chan struct{})
	go func() { s.RecordSynergy("h", true); close(started) }()
	<-parked
	if countedUnlocked {
		close(release)
		t.Fatal("a background start was counted in bgWg outside the gate's mutex")
	}
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned (%v) while a start admitted before it had not yet been released", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	<-started
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close did not return after the parked start was counted")
	}
	if n, _ := logs.count(refusedStartLog); n != 0 {
		t.Error("the start admitted before Close was refused")
	}
}

// TestStore_StartedGoroutineWriteIsRefusedWhileClosing: a store goroutine started before Close shut the gate, whose
// write comes while Close waits, gets the refusal like any write: nothing is written. The goroutine is parked at its
// start, Close is held in its wait by an admitted write, then the goroutine goes on.
func TestStore_StartedGoroutineWriteIsRefusedWhileClosing(t *testing.T) {
	cases := []struct {
		name  string
		start func(s *Store)
		key   string
	}{
		{"RecordSynergy", func(s *Store) { s.RecordSynergy("started", true) }, "synergy:success:started"},
		{"ResponseCache.Set", func(s *Store) {
			rc := NewResponseCache()
			rc.SetStore(s)
			rc.Set("started", &mcp.CallToolResult{}, time.Hour)
		}, "cache:l2:started"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_ = captureLogs(t)
			s, dir := openForCloseTest(t)
			parked := make(chan struct{})
			goOn := make(chan struct{})
			var once sync.Once
			hook := func() { once.Do(func() { close(parked); <-goOn }) }
			s.runHook.Store(&hook)
			c.start(s)
			<-parked
			release, held := holdAdmittedWrite(t, s)
			closed := make(chan error, 1)
			go func() { closed <- s.Close() }()
			waitGateShut(t, s)
			close(goOn)    // the started goroutine writes now, while Close waits for the held write,
			s.bgWg.Wait()  // and has finished before Close goes on (no start can Add: the gate is shut)
			close(release) // then Close goes on
			<-held
			if err := <-closed; err != nil {
				t.Fatalf("Close: %v", err)
			}
			reopened, err := NewStore(dir)
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			defer reopened.Close()
			if keyPresent(t, reopened, c.key) {
				t.Errorf("%s was written while the store was closing", c.key)
			}
		})
	}
}

// neverLogged reports whether no message starting with prefix is logged within d: a refused start must not run its
// work, and the work runs on its own goroutine, so absence is checked over a short window.
func neverLogged(logs *warnCapture, prefix string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if n, _ := logs.count(prefix); n != 0 {
			return false
		}
		time.Sleep(5 * time.Millisecond)
	}
	n, _ := logs.count(prefix)
	return n == 0
}

// TestStore_BackgroundStartsAreRefusedOnceClosing: once Close has shut the gate, none of the four start sites starts its
// goroutine; each logs the refusal (once per store, at info level, naming the work) and does none of the work.
func TestStore_BackgroundStartsAreRefusedOnceClosing(t *testing.T) {
	// closeWhile runs Close while a write is held, calls site once the gate is shut, then lets Close finish.
	closeWhile := func(t *testing.T, s *Store, site func()) {
		t.Helper()
		release, held := holdAdmittedWrite(t, s)
		closed := make(chan error, 1)
		go func() { closed <- s.Close() }()
		waitGateShut(t, s)
		site()
		close(release)
		<-held
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
	}
	wantRefused := func(t *testing.T, logs *warnCapture, work string) {
		t.Helper()
		n, first := logs.count(refusedStartLog)
		if n != 1 || !strings.Contains(first, "work="+work) {
			t.Errorf("want one %q naming %q, got %d (first: %q)", refusedStartLog, work, n, first)
		}
	}

	t.Run("RecordSynergy", func(t *testing.T) {
		logs := captureLogs(t)
		s, _ := openForCloseTest(t)
		closeWhile(t, s, func() { s.RecordSynergy("h", true) })
		wantRefused(t, logs, "synergy flush")
		if !neverLogged(logs, "database: failed to persist RRF synergy weight", 500*time.Millisecond) {
			t.Error("the refused synergy flush still ran (its failed write was logged)")
		}
	})
	t.Run("ResponseCache.Set", func(t *testing.T) {
		logs := captureLogs(t)
		s, _ := openForCloseTest(t)
		rc := NewResponseCache()
		rc.SetStore(s)
		closeWhile(t, s, func() { rc.Set("k", &mcp.CallToolResult{}, time.Hour) })
		wantRefused(t, logs, "L2 cache write")
		if !neverLogged(logs, "db: update failed", 500*time.Millisecond) {
			t.Error("the refused L2 cache write still ran (its failed write was logged)")
		}
	})
	t.Run("SaveIntelligence", func(t *testing.T) {
		logs := captureLogs(t)
		s, _ := openForCloseTest(t)
		// The write must land before the gate shuts and the re-index start come after: park the write, shut the
		// gate, release the write.
		entered, release := parkNextTxn(s)
		saved := make(chan error, 1)
		go func() { saved <- s.SaveIntelligence("srv:t", &ToolIntelligence{AnalysisStatus: "hydrated"}) }()
		<-entered
		closed := make(chan error, 1)
		go func() { closed <- s.Close() }()
		waitGateShut(t, s)
		release()
		if err := <-saved; err != nil {
			t.Fatalf("SaveIntelligence: the write was admitted before Close and must succeed, got %v", err)
		}
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
		wantRefused(t, logs, "intelligence re-index")
	})
	t.Run("FlushMetrics", func(t *testing.T) {
		logs := captureLogs(t)
		s, _ := openForCloseTest(t)
		s.IncrementToolCalls("srv:t", 5)
		entered, release := parkNextTxn(s)
		flushed := make(chan struct{})
		go func() { s.FlushMetrics(); close(flushed) }()
		<-entered
		closed := make(chan error, 1)
		go func() { closed <- s.Close() }()
		waitGateShut(t, s)
		release()
		<-flushed
		if err := <-closed; err != nil {
			t.Fatalf("Close: %v", err)
		}
		if n, _ := logs.count("failed to flush metrics batch"); n != 0 {
			t.Fatalf("setup: the metrics flush was admitted before Close and must succeed")
		}
		wantRefused(t, logs, "post-flush tool refresh")
	})
}
