package db

import (
	"fmt"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dgraph-io/badger/v4"
)

// openForCloseTest opens a real store in a temporary directory that the test owns.
func openForCloseTest(t *testing.T) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "db")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s, dir
}

// T2: writers keep saving while Close runs at a random point. No writer may panic or hang, every refusal must
// be ErrStoreClosing, and every save that returned nil must be readable after the directory is reopened.
func TestStore_WriteDuringCloseNeverPanicsOrHangs(t *testing.T) {
	const iterations = 25
	payload := []byte(strings.Repeat("x", 4096))
	for it := 0; it < iterations; it++ {
		s, dir := openForCloseTest(t)

		var (
			wg     sync.WaitGroup
			mu     sync.Mutex
			acked  []string
			failed []string
		)
		for w := 0; w < 4; w++ {
			wg.Add(1)
			go func(w int) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						mu.Lock()
						failed = append(failed, fmt.Sprintf("panic: %v", r))
						mu.Unlock()
					}
				}()
				for i := 0; ; i++ {
					id := fmt.Sprintf("w%d-%d", w, i)
					err := s.SaveRawResource(id, payload)
					if err == nil {
						mu.Lock()
						acked = append(acked, id)
						mu.Unlock()
						continue
					}
					if !isStoreClosing(err) {
						mu.Lock()
						failed = append(failed, fmt.Sprintf("refusal is not ErrStoreClosing: %v", err))
						mu.Unlock()
					}
					return
				}
			}(w)
		}
		time.Sleep(time.Duration(rand.Intn(5000)) * time.Microsecond)
		if err := s.Close(); err != nil {
			t.Fatalf("iteration %d: Close: %v", it, err)
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: a writer did not return within 10s of Close", it)
		}
		mu.Lock()
		if len(failed) > 0 {
			t.Fatalf("iteration %d: %d writer failures, first: %s", it, len(failed), failed[0])
		}
		ids := append([]string(nil), acked...)
		mu.Unlock()

		reopened, err := NewStore(dir)
		if err != nil {
			t.Fatalf("iteration %d: reopen: %v", it, err)
		}
		for _, id := range ids {
			if _, err := reopened.GetRawResource(id); err != nil {
				t.Fatalf("iteration %d: acknowledged save %s missing after reopen: %v", it, id, err)
			}
		}
		if err := reopened.Close(); err != nil {
			t.Fatalf("iteration %d: close after reopen: %v", it, err)
		}
	}
}

// T3: after Close returns, every write entry point returns ErrStoreClosing and writes nothing.
func TestStore_WriteAfterCloseReturnsErrStoreClosing(t *testing.T) {
	s, _ := openForCloseTest(t)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	cases := map[string]error{
		"SaveRawResource": s.SaveRawResource("after-close", []byte("v")),
		"SaveWithTTL":     s.SaveWithTTL("after-close", []byte("v"), time.Hour),
		"UpdateWithRetry": s.UpdateWithRetry(func(txn *badger.Txn) error { return txn.Set([]byte("after-close"), []byte("v")) }),
	}
	for name, err := range cases {
		if !isStoreClosing(err) {
			t.Errorf("%s after Close returned %v, want an error satisfying isStoreClosing(err)", name, err)
		}
	}
}

// T4: a second Close is a no-op.
func TestStore_CloseIsIdempotent(t *testing.T) {
	s, _ := openForCloseTest(t)
	if err := s.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("second Close panicked: %v", r)
		}
	}()
	if err := s.Close(); err != nil {
		t.Errorf("second Close returned %v, want nil", err)
	}
}

// T5: a write that was admitted holds Close until the write returns, and the write itself succeeds.
func TestStore_CloseWaitsForAnAdmittedWrite(t *testing.T) {
	s, dir := openForCloseTest(t)
	inWrite := make(chan struct{})
	release := make(chan struct{})
	writeErr := make(chan error, 1)
	go func() {
		writeErr <- s.UpdateWithRetry(func(txn *badger.Txn) error {
			close(inWrite)
			<-release
			return txn.Set([]byte("held-write"), []byte("v"))
		})
	}()
	<-inWrite

	closed := make(chan error, 1)
	go func() { closed <- s.Close() }()
	select {
	case err := <-closed:
		t.Fatalf("Close returned (%v) while an admitted write was still in flight", err)
	case <-time.After(750 * time.Millisecond):
	}
	close(release)
	if err := <-writeErr; err != nil {
		t.Errorf("admitted write returned %v, want nil", err)
	}
	if err := <-closed; err != nil {
		t.Errorf("Close: %v", err)
	}

	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	err = reopened.DB.View(func(txn *badger.Txn) error {
		_, e := txn.Get([]byte("held-write"))
		return e
	})
	if err != nil {
		t.Errorf("the admitted write is not in the store after reopen: %v", err)
	}
}
