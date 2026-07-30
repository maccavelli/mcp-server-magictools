package telemetry

import (
	"maps"
	"sync"
)

var (
	activeSpansMu sync.RWMutex
	// activeSpans mapping Server -> Explicit Correlation UUID bridging multi-hop execution pipelines implicitly
	activeSpans = make(map[string]string)
)

// RecordActiveDispatch assigns an executing payload a TraceID natively
func RecordActiveDispatch(server, corrID string) {
	activeSpansMu.Lock()
	defer activeSpansMu.Unlock()
	activeSpans[server] = corrID
}

// ClearActiveDispatch removes the footprint preventing memory leaks
func ClearActiveDispatch(server string) {
	activeSpansMu.Lock()
	defer activeSpansMu.Unlock()
	delete(activeSpans, server)
}

// GetActiveCascadeParent loops the active maps. Since agents execute single-threads, any active item is identical logically to the causal parent natively.
func GetActiveCascadeParent() string {
	activeSpansMu.RLock()
	defer activeSpansMu.RUnlock()
	for _, uuid := range activeSpans {
		return uuid
	}
	return ""
}

// GetActiveCascadeSource loops the active maps to find the server currently holding the execution lock natively.
func GetActiveCascadeSource() string {
	activeSpansMu.RLock()
	defer activeSpansMu.RUnlock()
	for server := range activeSpans {
		return server
	}
	return "client"
}

// GetAllActiveSpans returns a snapshot of all active correlation UUIDs bridging nodes.
func GetAllActiveSpans() map[string]string {
	activeSpansMu.RLock()
	defer activeSpansMu.RUnlock()
	snapshot := make(map[string]string, len(activeSpans))
	maps.Copy(snapshot, activeSpans)
	return snapshot
}
