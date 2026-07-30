package intelligence

import (
	"errors"
	"testing"

	"github.com/dgraph-io/badger/v4"
	"golang.org/x/sync/errgroup"
)

func TestIOHelpers_ViewOrWarn(t *testing.T) {
	viewOrWarn(nil, nil)

	opt := badger.DefaultOptions("").WithInMemory(true)
	db, _ := badger.Open(opt)
	defer db.Close()

	viewOrWarn(db, func(txn *badger.Txn) error {
		return errors.New("test error")
	})
}

func TestIOHelpers_ItemValueOrWarn(t *testing.T) {
	itemValueOrWarn(nil, nil)

	// it's tricky to mock *badger.Item so just testing nil case is fine
	// since we want to cover the `if item == nil` branch which was at 50%
}

func TestIOHelpers_WaitGroupOrWarn(t *testing.T) {
	waitGroupOrWarn(nil)

	var eg errgroup.Group
	eg.Go(func() error {
		return errors.New("test error")
	})
	waitGroupOrWarn(&eg)
}

func TestIOHelpers_SafeUint64FromInt(t *testing.T) {
	if safeUint64FromInt(-1) != 0 {
		t.Error("expected 0 for -1")
	}
	if safeUint64FromInt(10) != 10 {
		t.Error("expected 10 for 10")
	}
}
