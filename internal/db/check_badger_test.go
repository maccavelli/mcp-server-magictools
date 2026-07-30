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
		t.Skip("debug harness: set MAGICTOOLS_DEBUG_DB to a live data directory to run")
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
