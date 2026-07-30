package db

import (
	"os"
	"testing"
)

func TestHistoricalExtraCoverage(t *testing.T) {
	tmp, _ := os.MkdirTemp("", "hist-*")
	defer os.RemoveAll(tmp)
	store, _ := NewStore(tmp)
	defer store.Close()

	// FlushHFSCTrace
	FlushHFSCTrace(store, HFSCTraceDocument{})

	// FlushProxyCall
	FlushProxyCall(store, ProxyCallRecord{})

	// FlushTokenQuotas
	FlushTokenQuotas(store, 100, map[string]int64{"prompt": 100})

	// LoadTokenQuotas
	LoadTokenQuotas(store)

	// IncrementToolCalls
	store.SaveTool(&ToolRecord{URN: "tool1"})
	store.IncrementToolCalls("tool1", 10)

	// RecordToolError
	store.RecordToolError("tool1", "some_error")

	// ComputeScoreBoard
	store.ComputeScoreBoard(nil)

	// ComputeTrending
	store.ComputeTrending()

	// ComputeDatabaseTrending
	store.ComputeDatabaseTrending()

	// More badger testing
	store.GetStaleServers([]string{"srv1"})
	store.PurgeOrphanedServers([]string{"srv1"})
	store.PruneOrphanedIntelligence("srv1", []string{"urn1"})
	store.PurgeServerIntelligence("srv1")
	store.ReconcileMetrics()
	store.PurgeStaleServerTools("srv1", []string{"urn1"})
	store.PurgeServerTools("srv1")
	store.WarmCache()
	store.ReindexAllTools()
	store.SearchToolsFallback("query", "srv1", "", DomainSystem)

	// HFSCTraceDocument tests
	doc := &HFSCTraceDocument{}
	_ = doc.Type()

	// ResolveGCInterval test
	os.Setenv("MAGICTOOLS_BADGER_GC_INTERVAL", "5s")
	ResolveGCInterval()
	os.Unsetenv("MAGICTOOLS_BADGER_GC_INTERVAL")
	ResolveGCInterval()
}
