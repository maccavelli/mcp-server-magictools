package db

import (
	"context"
	"os"
	"testing"

	"github.com/dgraph-io/badger/v4"
)

func TestExtraDBMethods(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "magictools-db-extra-*")
	defer os.RemoveAll(tempDir)

	store, _ := NewStore(tempDir)
	defer store.Close()

	store.PurgeStaleServerTools("server1", []string{"tool1"})
	store.PruneOrphanedIntelligence("server1", []string{"tool1"})
	store.PurgeServerIntelligence("server1")
	store.GetServerToolsNatively("server1", 10)
	store.GetAllToolURNs()
	store.SaveWithTTL("key", []byte("val"), 100)
	store.ComputeScoreBoard(nil)
	store.ComputeTrending()
	store.ComputeDatabaseTrending()
	store.IncrementToolCalls("server1:tool1", 1)
	store.RecordToolError("server1:tool1", "some error")

	store.FlushMetrics()
	store.SearchTools(context.Background(), "query", "all", "", 0.5, 0.5, DomainUserLand, false)
	store.SearchToolsFallback("query", "all", "", DomainUserLand)
	store.GetTool("server1:tool1")
	store.GetSchema("schema")
	store.PurgeOrphanedServers(nil)
	_ = store.UpdateWithRetryCtx(context.Background(), func(txn *badger.Txn) error { return nil })

	store.GetMetrics()
	store.WarmCache()
	store.RecordSynergy("server1:tool1", true)
	store.GetSynergy("server1:tool1")
	store.ReconcileMetrics()
}
