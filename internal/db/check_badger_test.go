//go:build debuglive

// This file is a manual debug harness, built only with `-tags debuglive`; a normal `go test` does not compile it.
//
// What it does: TestDebugLiveBadger opens the store at the directory named by MAGICTOOLS_DEBUG_DB with NewStore and
// prints every key with the prefix "tool:".
//
// NEVER point it at a live service's directory. NewStore runs the pid-file cleanup (cleanupStaleProcess) first: it
// reads server.pid in that directory and sends SIGTERM, then SIGKILL, to the recorded pid if it is alive, so opening a
// live service's store stops the live service. Use a copy of the data directory instead.
//
// See docs/guides/testing-and-process-safety.md.

package db

import (
	"fmt"
	"os"
	"testing"

	"github.com/dgraph-io/badger/v4"
)

func TestDebugLiveBadger(t *testing.T) {
	path := os.Getenv("MAGICTOOLS_DEBUG_DB")
	if path == "" {
		t.Skip("debug harness: set MAGICTOOLS_DEBUG_DB to a COPY of a data directory (never a live service's) to run")
	}
	store, err := NewStore(path)
	if err != nil {
		t.Fatalf("Error opening DB: %v\n", err)
	}
	defer store.Close()

	store.DB.View(func(txn *badger.Txn) error {
		it := txn.NewIterator(badger.DefaultIteratorOptions)
		defer it.Close()
		prefix := []byte("tool:")
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			key := string(item.Key())
			fmt.Println("Found key:", key)
		}
		return nil
	})
}
