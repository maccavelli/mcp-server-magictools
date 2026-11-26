//go:build magictoolstestseam

package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

// OverwriteForTest replaces a stored PLANNED record without a transition rule.
// Test seam only: it refuses any record whose stored or supplied status is not
// PLANNED, so no running or terminal state can be rewritten through it (0005's
// P3: no exported way to write a state without its transition rule).
func (p *PipelineSessionStore) OverwriteForTest(ctx context.Context, rec *PipelineSessionRecord) error {
	if rec.Status != SessionPlanned {
		return fmt.Errorf("OverwriteForTest: supplied status %s is not PLANNED", rec.Status)
	}
	val, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	compressed, err := util.Compress(val)
	if err != nil {
		return err
	}
	return p.store.UpdateWithRetryCtx(ctx, func(txn *badger.Txn) error {
		stored, err := p.readTxn(txn, rec.SessionID)
		if err != nil {
			return err
		}
		if stored.Status != SessionPlanned {
			return fmt.Errorf("OverwriteForTest: stored status %s is not PLANNED", stored.Status)
		}
		ttl := time.Until(time.Unix(rec.ExpiresAt, 0)) + tombstoneGrace
		if ttl <= 0 {
			ttl = tombstoneGrace
		}
		return txn.SetEntry(badger.NewEntry(sessionKey(rec.SessionID), compressed).WithTTL(ttl))
	})
}
