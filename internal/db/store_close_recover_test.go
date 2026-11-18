package db

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// warnCapture is a slog.Handler that keeps every record's message and attributes.
type warnCapture struct {
	mu   sync.Mutex
	msgs []string
}

func (c *warnCapture) Enabled(context.Context, slog.Level) bool { return true }
func (c *warnCapture) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	b.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value.Any())
		return true
	})
	c.mu.Lock()
	c.msgs = append(c.msgs, b.String())
	c.mu.Unlock()
	return nil
}
func (c *warnCapture) WithAttrs([]slog.Attr) slog.Handler { return c }
func (c *warnCapture) WithGroup(string) slog.Handler      { return c }

// count returns how many captured messages start with prefix, and the first of them.
func (c *warnCapture) count(prefix string) (int, string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	n, first := 0, ""
	for _, m := range c.msgs {
		if strings.HasPrefix(m, prefix) {
			if n == 0 {
				first = m
			}
			n++
		}
	}
	return n, first
}

func captureLogs(t *testing.T) *warnCapture {
	t.Helper()
	c := &warnCapture{}
	old := slog.Default()
	slog.SetDefault(slog.New(c))
	t.Cleanup(func() { slog.SetDefault(old) })
	return c
}

const recoveredWarning = "database: a write outlived the close wait"

// parkNextTxn makes the next transaction function run through UpdateWithRetryCtx wait, at its start, until the
// returned release is called. entered is closed when it is waiting.
func parkNextTxn(s *Store) (entered <-chan struct{}, release func()) {
	in := make(chan struct{})
	rel := make(chan struct{})
	var once sync.Once
	hook := func() {
		parked := false
		once.Do(func() { parked = true })
		if parked {
			close(in)
			<-rel
		}
	}
	s.txnStartHook.Store(&hook)
	var relOnce sync.Once
	return in, func() { relOnce.Do(func() { close(rel) }) }
}

// outcome is what a write returned, or the value it panicked with.
type outcome struct {
	err      error
	panicked any
}

func runWrite(call func() error) <-chan outcome {
	out := make(chan outcome, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				out <- outcome{panicked: r}
			}
		}()
		out <- outcome{err: call()}
	}()
	return out
}

func waitOutcome(t *testing.T, out <-chan outcome) outcome {
	t.Helper()
	select {
	case o := <-out:
		return o
	case <-time.After(10 * time.Second):
		t.Fatal("the write did not return within 10s")
		return outcome{}
	}
}

// pastBound starts call, parks its first transaction function, lets Close run out of a short wait and close the
// database, then lets the transaction function go on against the closed database.
func pastBound(t *testing.T, s *Store, call func() error) outcome {
	t.Helper()
	s.drainWait = 100 * time.Millisecond
	entered, release := parkNextTxn(s)
	defer release()
	out := runWrite(call)
	<-entered
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !s.gate.timedOut.Load() || !s.DB.IsClosed() {
		t.Fatalf("setup: want the wait run out (%v) and the database closed (%v)", s.gate.timedOut.Load(), s.DB.IsClosed())
	}
	release()
	return waitOutcome(t, out)
}

// wantConverted asserts the closed database's iterator panic came back as the store-closing error, with its text,
// and was logged once.
func wantConverted(t *testing.T, o outcome, logs *warnCapture) {
	t.Helper()
	if o.panicked != nil {
		t.Fatalf("the write panicked past the bound: %v", o.panicked)
	}
	if !errors.Is(o.err, ErrStoreClosing) || !strings.Contains(fmt.Sprint(o.err), badger.ErrDBClosed.Error()) {
		t.Errorf("the write returned %v, want an error satisfying errors.Is(err, ErrStoreClosing) carrying %q", o.err, badger.ErrDBClosed.Error())
	}
	if n, first := logs.count(recoveredWarning); n != 1 || !strings.Contains(first, "panic="+badger.ErrDBClosed.Error()) {
		t.Errorf("want one warning %q naming the panic, got %d (first: %q)", recoveredWarning, n, first)
	}
	if n, _ := logs.count("database: writes still in flight when the close wait ran out"); n != 1 {
		t.Errorf("want one warning from Close naming the writes in flight, got %d", n)
	}
}

// The three write paths that open an iterator inside their transaction: past the bound, each returns the
// store-closing error instead of panicking.
func TestStore_PastBound_PurgeStaleServerToolsReturnsErrStoreClosing(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	wantConverted(t, pastBound(t, s, func() error { return s.PurgeStaleServerTools("srv", nil) }), logs)
}

func TestStore_PastBound_PruneOrphanedIntelligenceReturnsErrStoreClosing(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	wantConverted(t, pastBound(t, s, func() error { return s.PruneOrphanedIntelligence("srv", nil) }), logs)
}

func TestStore_PastBound_PurgeServerIntelligenceReturnsErrStoreClosing(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	wantConverted(t, pastBound(t, s, func() error { return s.PurgeServerIntelligence("srv") }), logs)
}

// A write site with no error result (updateOrWarn: FlushProxyCall, FlushHFSCTrace, FlushTokenQuotas, the L2 cache
// write): past the bound the closed database's panic is logged and swallowed; nothing propagates.
func TestStore_PastBound_SiteWithoutErrorResultLogsAndCarriesOn(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	s.drainWait = 100 * time.Millisecond
	inWrite := make(chan struct{})
	release := make(chan struct{})
	out := runWrite(func() error {
		s.updateOrWarn(func(txn *badger.Txn) error {
			close(inWrite)
			<-release
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			it.Close()
			return nil
		})
		return nil
	})
	<-inWrite
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(release)
	if o := waitOutcome(t, out); o.panicked != nil {
		t.Fatalf("a write without an error result panicked past the bound: %v", o.panicked)
	}
	if n, _ := logs.count(recoveredWarning); n != 1 {
		t.Errorf("want one warning %q, got %d", recoveredWarning, n)
	}
	if n, first := logs.count("db: update failed"); n != 1 || !strings.Contains(first, ErrStoreClosing.Error()) {
		t.Errorf("want the site's own warning carrying %q once, got %d (first: %q)", ErrStoreClosing.Error(), n, first)
	}
}

// nonLibraryPanicAtSiteWithoutErrorResult runs, through updateOrWarn, a write whose transaction function waits for
// release and then panics with bug (a bug in a transaction function, not the library's panic).
func nonLibraryPanicAtSiteWithoutErrorResult(s *Store, inWrite, release chan struct{}, bug error) <-chan outcome {
	return runWrite(func() error {
		s.updateOrWarn(func(_ *badger.Txn) error {
			close(inWrite)
			<-release
			panic(bug)
		})
		return nil
	})
}

// A site with no error result (updateOrWarn) logs a failed write, but never swallows a panic: past the bound, with
// Badger closed, a panic that is not the library's reaches the caller with its own value.
func TestStore_Recovery_NonLibraryPanicAtSiteWithoutErrorResultPastTheBoundPropagates(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	s.drainWait = 100 * time.Millisecond
	inWrite, release := make(chan struct{}), make(chan struct{})
	bug := errors.New("a bug at a site without an error result")
	out := nonLibraryPanicAtSiteWithoutErrorResult(s, inWrite, release, bug)
	<-inWrite
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !s.gate.timedOut.Load() || !s.DB.IsClosed() {
		t.Fatalf("setup: want the wait run out (%v) and the database closed (%v)", s.gate.timedOut.Load(), s.DB.IsClosed())
	}
	close(release)
	wantPropagated(t, waitOutcome(t, out), bug, logs)
}

// The same within the bound (Close waiting for the write): the panic reaches the caller, and Close then returns
// without running out of its wait.
func TestStore_Recovery_NonLibraryPanicAtSiteWithoutErrorResultWithinTheBoundPropagates(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	inWrite, release := make(chan struct{}), make(chan struct{})
	bug := errors.New("a bug at a site without an error result")
	out := nonLibraryPanicAtSiteWithoutErrorResult(s, inWrite, release, bug)
	<-inWrite
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	waitGateShut(t, s)
	close(release)
	wantPropagated(t, waitOutcome(t, out), bug, logs)
	if err := <-closed; err != nil {
		t.Errorf("Close: %v", err)
	}
	if s.gate.timedOut.Load() {
		t.Error("Close ran out of its wait although the write ended")
	}
}

// libraryPanic raises a panic inside the database library on an open database: an iterator on a discarded
// transaction (badger panics with ErrDiscardedTxn).
func libraryPanic(txn *badger.Txn) error {
	txn.Discard()
	it := txn.NewIterator(badger.DefaultIteratorOptions)
	it.Close()
	return nil
}

// wantPropagated asserts the write's panic reached the caller with its own value and nothing was converted.
func wantPropagated(t *testing.T, o outcome, want any, logs *warnCapture) {
	t.Helper()
	if o.panicked == nil {
		t.Fatalf("the panic did not propagate: the write returned %v", o.err)
	}
	if o.panicked != want {
		t.Errorf("the panic propagated with %#v, want the original value %#v", o.panicked, want)
	}
	if n, first := logs.count(recoveredWarning); n != 0 {
		t.Errorf("a panic was converted: %q", first)
	}
}

func gateCount(s *Store) int {
	s.gate.mu.Lock()
	defer s.gate.mu.Unlock()
	return s.gate.n
}

// While the store is open, a panic raised inside the library propagates unchanged, and the write is still counted out.
func TestStore_Recovery_LibraryPanicWhileOpenPropagates(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	o := waitOutcome(t, runWrite(func() error { return s.UpdateWithRetry(libraryPanic) }))
	wantPropagated(t, o, badger.ErrDiscardedTxn, logs)
	if n := gateCount(s); n != 0 {
		t.Errorf("after a panicking write the gate still counts %d writes", n)
	}
	if err := s.Close(); err != nil || s.gate.timedOut.Load() {
		t.Errorf("Close after a panicking write: %v (wait ran out: %v)", err, s.gate.timedOut.Load())
	}
}

// While Close is waiting (within the bound), a library panic in an admitted write propagates unchanged, and Close
// then returns without running out of its wait.
func TestStore_Recovery_LibraryPanicWithinTheBoundPropagates(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	entered, release := parkNextTxn(s)
	out := runWrite(func() error { return s.UpdateWithRetry(libraryPanic) })
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	waitGateShut(t, s)
	release()
	wantPropagated(t, waitOutcome(t, out), badger.ErrDiscardedTxn, logs)
	if err := <-closed; err != nil {
		t.Errorf("Close: %v", err)
	}
	if s.gate.timedOut.Load() {
		t.Error("Close ran out of its wait although the write ended")
	}
}

// The database closed behind the gate's back (Close's wait never ran out): the closed database's panic propagates.
func TestStore_Recovery_ClosedDatabaseWithoutTimeoutPropagates(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	entered, release := parkNextTxn(s)
	out := runWrite(func() error {
		return s.UpdateWithRetry(func(txn *badger.Txn) error {
			it := txn.NewIterator(badger.DefaultIteratorOptions)
			it.Close()
			return nil
		})
	})
	<-entered
	// The store's own background reindex (started by NewStore) reads Badger; let it finish before Badger is closed
	// behind the store's back, or its iterator panics on the closed database.
	s.bgWg.Wait()
	if err := s.DB.Close(); err != nil {
		t.Fatalf("closing Badger directly: %v", err)
	}
	release()
	wantPropagated(t, waitOutcome(t, out), badger.ErrDBClosed, logs)
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}

// Close's wait ran out but the database is not closed yet (Close is held at its next step): a library panic in the
// late write is not the closed database's, and propagates.
func TestStore_Recovery_TimedOutButDatabaseOpenPropagates(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	s.drainWait = 100 * time.Millisecond
	s.bgWg.Add(1) // holds Close after its wait, before it closes Badger
	entered, release := parkNextTxn(s)
	out := runWrite(func() error { return s.UpdateWithRetry(libraryPanic) })
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	deadline := time.Now().Add(10 * time.Second)
	for !s.gate.timedOut.Load() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if !s.gate.timedOut.Load() || s.DB.IsClosed() {
		t.Fatalf("setup: want the wait run out (%v) with the database open (closed: %v)", s.gate.timedOut.Load(), s.DB.IsClosed())
	}
	release()
	wantPropagated(t, waitOutcome(t, out), badger.ErrDiscardedTxn, logs)
	s.bgWg.Done()
	if err := <-closed; err != nil {
		t.Errorf("Close: %v", err)
	}
}

// Past the bound with the database closed, a panic that the library did not raise (a bug in a transaction
// function) propagates with its own value.
func TestStore_Recovery_NonLibraryPanicPastTheBoundPropagates(t *testing.T) {
	logs := captureLogs(t)
	s, _ := openForCloseTest(t)
	bug := errors.New("a bug in a transaction function")
	o := pastBound(t, s, func() error {
		return s.UpdateWithRetry(func(_ *badger.Txn) error { panic(bug) })
	})
	wantPropagated(t, o, bug, logs)
}

//go:noinline
func sendOnClosedChannel() {
	c := make(chan int, 1)
	close(c)
	c <- 1
}

//go:noinline
func derefNil(p *int) int {
	return *p
}

func panicOriginOf(f func()) (origin string, ok bool) {
	defer func() {
		origin, ok = panicOrigin(2)
		_ = recover()
	}()
	f()
	return "", false
}

// panicOrigin names the function that raised the panic, below the runtime's own frames, for a panic raised by the
// runtime on a channel send (the library's commit-window panic is this kind), by a nil pointer, and by the library.
func TestPanicOrigin_NamesTheRaisingFunction(t *testing.T) {
	const pkg = "github.com/maccavelli/mcp-server-magictools/internal/db."
	if fn, ok := panicOriginOf(sendOnClosedChannel); !ok || fn != pkg+"sendOnClosedChannel" {
		t.Errorf("send on closed channel: origin %q (%v), want %s", fn, ok, pkg+"sendOnClosedChannel")
	}
	if fn, ok := panicOriginOf(func() { derefNil(nil) }); !ok || fn != pkg+"derefNil" {
		t.Errorf("nil pointer: origin %q (%v), want %s", fn, ok, pkg+"derefNil")
	}
	s, _ := openForCloseTest(t)
	defer s.Close()
	txn := s.DB.NewTransaction(false)
	txn.Discard()
	fn, ok := panicOriginOf(func() { txn.NewIterator(badger.DefaultIteratorOptions).Close() })
	if !ok || fn != badgerPanicPrefix+".(*Txn).NewIterator" {
		t.Errorf("library: origin %q (%v), want %s", fn, ok, badgerPanicPrefix+".(*Txn).NewIterator")
	}
	if fn, ok := panicOriginOf(func() {}); ok {
		t.Errorf("no panic: origin %q reported", fn)
	}
}
