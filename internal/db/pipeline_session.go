package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/maccavelli/mcp-server-magictools/internal/util"
)

// Pipeline session statuses (0005's D2 contract, adopted by 0013).
const (
	SessionPlanned   = "PLANNED"
	SessionRunning   = "RUNNING"
	SessionCompleted = "COMPLETED"
	SessionFailed    = "FAILED"
	SessionRejected  = "REJECTED"
)

// PipelineSessionCap is the maximum number of PLANNED sessions kept; the
// oldest is evicted when a new one exceeds the cap (0005's L-a).
const PipelineSessionCap = 50

const (
	pipelineSessionPrefix = "pipeline-session:"
	// plannedLifetime is how long a PLANNED session stays valid (24 hours).
	plannedLifetime = 24 * time.Hour
	// tombstoneGrace keeps terminal sessions readable for one hour past their
	// logical expiry, so a late continuation gets a distinct refusal rather
	// than unknown_session.
	tombstoneGrace = time.Hour
)

// Refusal sentinels. Each maps to one word of the contract's refusal vocabulary;
// the handler translates them into the wire messages.
var (
	ErrSessionUnknown     = errors.New("unknown_session")
	ErrSessionExpired     = errors.New("expired_session")
	ErrSessionRunning     = errors.New("session_running")
	ErrSessionAlreadyRun  = errors.New("session_already_run")
	ErrSessionFailed      = errors.New("session_failed")
	ErrSessionRejected    = errors.New("session_rejected")
	ErrSessionInterrupted = errors.New("session_interrupted")
)

// PipelineStage is one stored plan stage: the fields of handler.PipelineStep
// that a continuation re-runs. Kept here (not imported from handler) so the db
// package carries no handler dependency.
type PipelineStage struct {
	ToolName       string         `json:"name"`
	Role           string         `json:"role"`
	Phase          int            `json:"phase"`
	Purpose        string         `json:"purpose"`
	Args           map[string]any `json:"args,omitempty"`
	InputContract  string         `json:"input_contract,omitempty"`
	OutputContract string         `json:"output_contract,omitempty"`
}

// PipelineSessionRecord is the stored state of one pipeline session.
type PipelineSessionRecord struct {
	SessionID     string          `json:"session_id"`
	Target        string          `json:"target"`
	Intent        string          `json:"intent"`
	PlanHash      string          `json:"plan_hash,omitempty"`
	Status        string          `json:"status"`
	Stages        []PipelineStage `json:"stages,omitempty"`
	Warnings      []string        `json:"warnings,omitempty"`
	RecallContext string          `json:"recall_context,omitempty"`
	Roles         []string        `json:"roles,omitempty"`
	ArtifactURIs  []string        `json:"artifact_uris,omitempty"`
	InstanceNonce string          `json:"instance_nonce,omitempty"`
	CreatedAt     int64           `json:"created_at"`
	UpdatedAt     int64           `json:"updated_at"`
	ExpiresAt     int64           `json:"expires_at"`
}

// PipelineSessionStore persists pipeline sessions under their own key prefix,
// with atomic transitions (0005's D2.5 L-a, D2.6 A-a; 0013 P3). No exported
// method writes a state without its transition rule.
type PipelineSessionStore struct {
	store *Store
}

// NewPipelineSessionStore binds a session store to the opened database.
func NewPipelineSessionStore(s *Store) *PipelineSessionStore {
	return &PipelineSessionStore{store: s}
}

func sessionKey(id string) []byte {
	return []byte(pipelineSessionPrefix + id)
}

// CreatePlanned stores a new PLANNED session with the 24-hour lifetime plus the
// tombstone grace as its Badger TTL, evicting the oldest PLANNED session when
// the cap is exceeded.
func (p *PipelineSessionStore) CreatePlanned(ctx context.Context, rec *PipelineSessionRecord) error {
	if rec.SessionID == "" {
		return fmt.Errorf("pipeline session: empty id")
	}
	rec.Status = SessionPlanned
	now := time.Now()
	if rec.CreatedAt == 0 {
		rec.CreatedAt = now.Unix()
	}
	rec.UpdatedAt = now.Unix()
	if rec.ExpiresAt == 0 {
		rec.ExpiresAt = now.Add(plannedLifetime).Unix()
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
		if err := p.evictOverCap(txn); err != nil {
			return err
		}
		e := badger.NewEntry(sessionKey(rec.SessionID), compressed).WithTTL(plannedLifetime + tombstoneGrace)
		return txn.SetEntry(e)
	})
}

// evictOverCap removes the oldest PLANNED sessions beyond the cap. Runs inside
// the create transaction so the count and the eviction are atomic.
func (p *PipelineSessionStore) evictOverCap(txn *badger.Txn) error {
	type entry struct {
		key       []byte
		createdAt int64
	}
	var planned []entry
	it := txn.NewIterator(badger.DefaultIteratorOptions)
	defer it.Close()
	prefix := []byte(pipelineSessionPrefix)
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		var rec PipelineSessionRecord
		if err := item.Value(func(val []byte) error {
			raw, derr := util.Decompress(val)
			if derr != nil {
				return derr
			}
			return json.Unmarshal(raw, &rec)
		}); err != nil {
			return err
		}
		if rec.Status == SessionPlanned {
			key := append([]byte(nil), item.Key()...)
			planned = append(planned, entry{key: key, createdAt: rec.CreatedAt})
		}
	}
	// After this create there will be len(planned)+1; evict down to cap-1.
	for len(planned)+1 > PipelineSessionCap {
		oldest := 0
		for i := range planned {
			if planned[i].createdAt < planned[oldest].createdAt {
				oldest = i
			}
		}
		if err := txn.Delete(planned[oldest].key); err != nil {
			return err
		}
		planned = append(planned[:oldest], planned[oldest+1:]...)
	}
	return nil
}

// Get reads a session without any instance check. A stored entry past its
// expires_at is still returned (inside the TTL grace); the transition methods
// refuse it with ErrSessionExpired.
func (p *PipelineSessionStore) Get(ctx context.Context, id string) (*PipelineSessionRecord, error) {
	return p.readRecord(id)
}

func (p *PipelineSessionStore) readRecord(id string) (*PipelineSessionRecord, error) {
	var rec PipelineSessionRecord
	err := p.store.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get(sessionKey(id))
		if errors.Is(err, badger.ErrKeyNotFound) {
			return ErrSessionUnknown
		}
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			raw, derr := util.Decompress(val)
			if derr != nil {
				return derr
			}
			return json.Unmarshal(raw, &rec)
		})
	})
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetForInstance reads a session and reports ErrSessionInterrupted when a
// RUNNING session was claimed by a different server instance (0005's
// interrupted-run rule). Non-RUNNING sessions are returned unchanged.
func (p *PipelineSessionStore) GetForInstance(ctx context.Context, id, instanceNonce string) (*PipelineSessionRecord, error) {
	rec, err := p.readRecord(id)
	if err != nil {
		return nil, err
	}
	if rec.Status == SessionRunning && rec.InstanceNonce != instanceNonce {
		return nil, ErrSessionInterrupted
	}
	return rec, nil
}

// ClaimRunning atomically transitions PLANNED → RUNNING, recording the claiming
// instance's nonce. Every refusal is distinct: unknown, expired, running,
// already-run, failed, rejected. The loser of a race reads the winner's state.
func (p *PipelineSessionStore) ClaimRunning(ctx context.Context, id, instanceNonce string) (*PipelineSessionRecord, error) {
	var claimed PipelineSessionRecord
	err := p.store.UpdateWithRetryCtx(ctx, func(txn *badger.Txn) error {
		rec, err := p.readTxn(txn, id)
		if err != nil {
			return err
		}
		if err := checkTransitionable(rec); err != nil {
			return err
		}
		rec.Status = SessionRunning
		rec.InstanceNonce = instanceNonce
		rec.UpdatedAt = time.Now().Unix()
		claimed = *rec
		return p.writeTxn(txn, rec)
	})
	if err != nil {
		return nil, err
	}
	return &claimed, nil
}

// Reject atomically transitions PLANNED → REJECTED and discards the stored
// stages. A repeated reject returns the same REJECTED record (idempotent).
func (p *PipelineSessionStore) Reject(ctx context.Context, id string) (*PipelineSessionRecord, error) {
	var rejected PipelineSessionRecord
	err := p.store.UpdateWithRetryCtx(ctx, func(txn *badger.Txn) error {
		rec, err := p.readTxn(txn, id)
		if err != nil {
			return err
		}
		if rec.Status == SessionRejected {
			rejected = *rec
			return nil
		}
		if err := checkTransitionable(rec); err != nil {
			return err
		}
		rec.Status = SessionRejected
		rec.Stages = nil
		rec.UpdatedAt = time.Now().Unix()
		rejected = *rec
		return p.writeTxn(txn, rec)
	})
	if err != nil {
		return nil, err
	}
	return &rejected, nil
}

// Complete transitions RUNNING → COMPLETED, storing the artifact URIs.
func (p *PipelineSessionStore) Complete(ctx context.Context, id string, artifactURIs []string) error {
	return p.finish(ctx, id, SessionCompleted, artifactURIs)
}

// Fail transitions RUNNING → FAILED.
func (p *PipelineSessionStore) Fail(ctx context.Context, id string) error {
	return p.finish(ctx, id, SessionFailed, nil)
}

func (p *PipelineSessionStore) finish(ctx context.Context, id, status string, uris []string) error {
	return p.store.UpdateWithRetryCtx(ctx, func(txn *badger.Txn) error {
		rec, err := p.readTxn(txn, id)
		if err != nil {
			return err
		}
		if rec.Status != SessionRunning {
			return fmt.Errorf("pipeline session %s: cannot complete from %s", id, rec.Status)
		}
		rec.Status = status
		rec.ArtifactURIs = uris
		rec.UpdatedAt = time.Now().Unix()
		return p.writeTxn(txn, rec)
	})
}

// checkTransitionable refuses a claim/reject of a session that is not PLANNED,
// with the distinct refusal for each state. Expiry is checked first: a past
// expires_at is expired_session regardless of stored status.
func checkTransitionable(rec *PipelineSessionRecord) error {
	if rec.ExpiresAt > 0 && time.Now().Unix() > rec.ExpiresAt {
		return ErrSessionExpired
	}
	switch rec.Status {
	case SessionPlanned:
		return nil
	case SessionRunning:
		return ErrSessionRunning
	case SessionCompleted:
		return ErrSessionAlreadyRun
	case SessionFailed:
		return ErrSessionFailed
	case SessionRejected:
		return ErrSessionRejected
	default:
		return fmt.Errorf("pipeline session %s: unknown status %s", rec.SessionID, rec.Status)
	}
}

func (p *PipelineSessionStore) readTxn(txn *badger.Txn, id string) (*PipelineSessionRecord, error) {
	item, err := txn.Get(sessionKey(id))
	if errors.Is(err, badger.ErrKeyNotFound) {
		return nil, ErrSessionUnknown
	}
	if err != nil {
		return nil, err
	}
	var rec PipelineSessionRecord
	if err := item.Value(func(val []byte) error {
		raw, derr := util.Decompress(val)
		if derr != nil {
			return derr
		}
		return json.Unmarshal(raw, &rec)
	}); err != nil {
		return nil, err
	}
	return &rec, nil
}

func (p *PipelineSessionStore) writeTxn(txn *badger.Txn, rec *PipelineSessionRecord) error {
	val, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	compressed, err := util.Compress(val)
	if err != nil {
		return err
	}
	// Preserve the remaining TTL: rewrite with the original expiry's grace.
	ttl := time.Until(time.Unix(rec.ExpiresAt, 0)) + tombstoneGrace
	if ttl <= 0 {
		ttl = tombstoneGrace
	}
	e := badger.NewEntry(sessionKey(rec.SessionID), compressed).WithTTL(ttl)
	return txn.SetEntry(e)
}

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

// rawValue reads the stored compressed bytes of a session (test seam for the
// prefix-isolation and compression assertions).
func (p *PipelineSessionStore) rawValue(id string) ([]byte, error) {
	var out []byte
	err := p.store.DB.View(func(txn *badger.Txn) error {
		item, err := txn.Get(sessionKey(id))
		if err != nil {
			return err
		}
		return item.Value(func(val []byte) error {
			out = append([]byte(nil), val...)
			return nil
		})
	})
	return out, err
}
