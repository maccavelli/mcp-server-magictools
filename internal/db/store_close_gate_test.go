package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// holdAdmittedWrite starts a write that the gate admits and that stays inside its transaction until release is
// closed. It returns once the write is inside its transaction, and a channel that yields the write's result.
func holdAdmittedWrite(t *testing.T, s *Store) (release chan struct{}, result <-chan error) {
	t.Helper()
	inWrite := make(chan struct{})
	release = make(chan struct{})
	res := make(chan error, 1)
	go func() {
		res <- s.UpdateWithRetry(func(_ *badger.Txn) error {
			close(inWrite)
			<-release
			return nil
		})
	}()
	<-inWrite
	return release, res
}

// waitGateShut returns once Close has shut the gate (it then waits for admitted writes).
func waitGateShut(t *testing.T, s *Store) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s.gate.mu.Lock()
		shut := s.gate.closed
		s.gate.mu.Unlock()
		if shut {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("Close did not shut the write gate within 10s")
}

// keyPresent reports whether key is in the store, read through the raw handle.
func keyPresent(t *testing.T, s *Store, key string) bool {
	t.Helper()
	err := s.DB.View(func(txn *badger.Txn) error {
		_, e := txn.Get([]byte(key))
		return e
	})
	if err != nil && !errors.Is(err, badger.ErrKeyNotFound) {
		t.Fatalf("reading %s: %v", key, err)
	}
	return err == nil
}

// prefixPresent reports whether any key with prefix is in the store.
func prefixPresent(t *testing.T, s *Store, prefix string) bool {
	t.Helper()
	found := false
	if err := s.DB.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.PrefetchValues = false
		it := txn.NewIterator(opts)
		defer it.Close()
		it.Seek([]byte(prefix))
		found = it.ValidForPrefix([]byte(prefix))
		return nil
	}); err != nil {
		t.Fatalf("scanning %s: %v", prefix, err)
	}
	return found
}

// TestStore_EveryWriteSiteIsRefusedWhileClosing calls every write site of the package while Close is waiting for an
// admitted write: the gate is shut but Badger is still open, so a site that bypassed the gate would write. Each site
// that returns an error must return ErrStoreClosing, and after Close and a reopen nothing any site would have written
// is there, and the keys the purge and wipe sites would have removed are still there.
func TestStore_EveryWriteSiteIsRefusedWhileClosing(t *testing.T) {
	s, dir := openForCloseTest(t)

	// Seeds for the sites that delete.
	seed := func(key string, val []byte) {
		t.Helper()
		if err := s.DB.Update(func(txn *badger.Txn) error { return txn.Set([]byte(key), val) }); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}
	tool, _ := json.Marshal(ToolRecord{URN: "purgesrv:t1", Name: "t1", Server: "purgesrv"})
	seed("tool:purgesrv:t1", tool)
	seed("synergy:success:seeded", []byte("7"))
	seed("keep:me", []byte("v"))
	seeded := &atomic.Int64{}
	seeded.Store(7)
	s.SynergySuccess.Store("seeded", seeded)

	release, held := holdAdmittedWrite(t, s)
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	waitGateShut(t, s)

	withErr := map[string]error{}
	// badger.go:245, RecordSynergy's flush (a store goroutine; no error result).
	s.RecordSynergy("recorded", true)
	// badger.go:365, synergy eviction (no error result).
	s.evictSynergyWeightsBeyondCap(0)
	// badger.go:592, UpdateWithRetryCtx.
	withErr["UpdateWithRetry"] = s.UpdateWithRetry(func(txn *badger.Txn) error { return txn.Set([]byte("w:retry"), []byte("v")) })
	// badger.go:637, BatchSaveTools.
	withErr["BatchSaveTools"] = s.BatchSaveTools([]*ToolRecord{{URN: "batchsrv:t", Name: "t", Server: "batchsrv"}}, nil)
	// badger.go:1023, SaveSchema.
	withErr["SaveSchema"] = s.SaveSchema("h1", map[string]any{"type": "object"})
	// badger.go:1448, SaveRawResource.
	withErr["SaveRawResource"] = s.SaveRawResource("raw1", []byte("v"))
	// badger.go:1632, PurgeServerTools.
	withErr["PurgeServerTools"] = s.PurgeServerTools("purgesrv")
	// badger.go:2119, SaveLog.
	withErr["SaveLog"] = s.SaveLog([]byte("line"), time.Hour)
	// badger.go:2244, SaveTrigger.
	withErr["SaveTrigger"] = s.SaveTrigger("kw", "srv")
	// badger.go:2251, SaveWithTTL.
	withErr["SaveWithTTL"] = s.SaveWithTTL("w:ttl", []byte("v"), time.Hour)
	// badger.go:2328, PopulateDefaultTriggers (no error result).
	s.PopulateDefaultTriggers()
	// badger.go:2476 and :2522, the two DropAll sites.
	withErr["WipeAll"] = s.WipeAll()
	withErr["WipeDatabase"] = s.WipeDatabase()
	// historical.go:28, :93, :119, :142 (no error results).
	FlushMetricBucket(s, map[string]any{"x": 1})
	FlushHFSCTrace(s, HFSCTraceDocument{ID: "trace1"})
	FlushProxyCall(s, ProxyCallRecord{URN: "srv:tool", Timestamp: 1})
	FlushTokenQuotas(s, 5, map[string]int64{"srv": 5})
	// cache.go:242, the L2 response-cache write (a store goroutine; no error result).
	rc := NewResponseCache()
	rc.SetStore(s)
	rc.Set("rc1", &mcp.CallToolResult{}, time.Hour)
	// The two store goroutines above must have run (and been refused) before the held write lets Close go on.
	s.bgWg.Wait()

	for name, err := range withErr {
		if !isStoreClosing(err) {
			t.Errorf("%s while closing returned %v, want an error satisfying errors.Is(err, ErrStoreClosing)", name, err)
		}
	}

	close(release)
	if err := <-held; err != nil {
		t.Errorf("the held write returned %v, want nil", err)
	}
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}

	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	for _, key := range []string{
		"synergy:success:recorded", "w:retry", "tool:batchsrv:t", "schema:h1", "raw:raw1", "trigger:kw", "w:ttl",
		"trigger:github", "telemetry:trace:trace1", "proxy:call:srv:tool:1", "llm:quota:global", "cache:l2:rc1",
	} {
		if keyPresent(t, reopened, key) {
			t.Errorf("%s was written while the store was closing", key)
		}
	}
	for _, prefix := range []string{"log:", "telemetry:bucket:"} {
		if prefixPresent(t, reopened, prefix) {
			t.Errorf("a %s key was written while the store was closing", prefix)
		}
	}
	for _, key := range []string{"tool:purgesrv:t1", "synergy:success:seeded", "keep:me"} {
		if !keyPresent(t, reopened, key) {
			t.Errorf("%s was removed while the store was closing", key)
		}
	}
}

// TestStore_RefusedWriteIsNotRetried: once the store is closing, UpdateWithRetryCtx returns ErrStoreClosing at once.
// It does not run the transaction function and does not enter its backoff: with a context that is already cancelled,
// a backoff would return the context's error instead.
func TestStore_RefusedWriteIsNotRetried(t *testing.T) {
	s, _ := openForCloseTest(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err := s.UpdateWithRetryCtx(ctx, func(_ *badger.Txn) error {
		calls++
		return nil
	})
	if !errors.Is(err, ErrStoreClosing) || errors.Is(err, context.Canceled) {
		t.Errorf("UpdateWithRetryCtx after Close returned %v, want ErrStoreClosing without a retry", err)
	}
	if err != nil && strings.Contains(err.Error(), "retries") {
		t.Errorf("the refusal went through the retry loop: %v", err)
	}
	if calls != 0 {
		t.Errorf("the transaction function ran %d times after Close, want 0", calls)
	}
}

// TestStore_NestedAdmissionCannotDeadlockClose: a write admitted while Close waits for the write it is nested in is
// refused at once, not blocked; the outer write then ends and Close returns well inside the bound. A gate that made a
// nested admission wait for Close (a read-write lock) would deadlock here until the bound.
func TestStore_NestedAdmissionCannotDeadlockClose(t *testing.T) {
	s, _ := openForCloseTest(t)
	inOuter := make(chan struct{})
	goNested := make(chan struct{})
	nested := make(chan error, 1)
	outer := make(chan error, 1)
	go func() {
		outer <- s.UpdateWithRetry(func(txn *badger.Txn) error {
			close(inOuter)
			<-goNested
			nested <- s.SaveWithTTL("nested", []byte("v"), time.Hour)
			return txn.Set([]byte("outer"), []byte("v"))
		})
	}()
	<-inOuter
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	waitGateShut(t, s)
	close(goNested)

	select {
	case err := <-nested:
		if !isStoreClosing(err) {
			t.Errorf("nested write while closing returned %v, want ErrStoreClosing", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a nested admission blocked while Close was waiting: deadlock")
	}
	if err := <-outer; err != nil {
		t.Errorf("outer write returned %v, want nil", err)
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Errorf("Close: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not return after the outer write ended")
	}
	if s.gate.timedOut.Load() {
		t.Error("Close ran out of its wait although every admitted write ended")
	}
}

// TestStore_ConcurrentCloseIsSafe: two Close calls at once both return nil, neither panics, and neither returns
// before the teardown is done.
func TestStore_ConcurrentCloseIsSafe(t *testing.T) {
	s, _ := openForCloseTest(t)
	release, held := holdAdmittedWrite(t, s)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			defer func() {
				if r := recover(); r != nil {
					results <- errors.New("Close panicked")
				}
			}()
			results <- s.Close()
		}()
	}
	select {
	case err := <-results:
		t.Fatalf("a Close returned (%v) while an admitted write was in flight", err)
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	if err := <-held; err != nil {
		t.Errorf("held write: %v", err)
	}
	for range 2 {
		if err := <-results; err != nil {
			t.Errorf("Close: %v", err)
		}
	}
	if !s.DB.IsClosed() {
		t.Error("both Close calls returned but the database is not closed")
	}
}

// TestStore_ReadsAreNotGated: a read while Close waits (the gate is shut, Badger is open) is served; a read after
// Close gets Badger's own closed-database error, not ErrStoreClosing, as before the gate.
func TestStore_ReadsAreNotGated(t *testing.T) {
	s, _ := openForCloseTest(t)
	if err := s.SaveRawResource("r1", []byte("payload")); err != nil {
		t.Fatalf("seed: %v", err)
	}
	release, held := holdAdmittedWrite(t, s)
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	waitGateShut(t, s)
	if got, err := s.GetRawResource("r1"); err != nil || string(got) != "payload" {
		t.Errorf("read while closing returned (%q, %v), want the payload", got, err)
	}
	close(release)
	<-held
	if err := <-closed; err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := s.GetRawResource("r1")
	if err == nil || isStoreClosing(err) || !errors.Is(err, badger.ErrDBClosed) {
		t.Errorf("read after Close returned %v, want badger.ErrDBClosed", err)
	}
}
