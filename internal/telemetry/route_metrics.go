package telemetry

import (
	"strings"
	"sync"
	"sync/atomic"
)

// RouteTracker structurally captures cross-server telemetry
type RouteTracker struct {
	routes        sync.Map // map[string]*RouteMetrics, key = "source|target"
	InvalidRoutes atomic.Int64
}

// RouteMetrics holds invocation stats for a specific routing path
type RouteMetrics struct {
	Calls  atomic.Int64
	Faults atomic.Int64
}

// GlobalRouteTracker maintains global cross-server routing statistics natively
var GlobalRouteTracker = &RouteTracker{}

// RecordRoute tracks the invocation of a target server from a source server natively
func (rt *RouteTracker) RecordRoute(source, target string, fault bool) {
	if source == "" {
		source = routeSourceClient
	}
	key := source + "|" + target
	val, _ := rt.routes.LoadOrStore(key, &RouteMetrics{})
	m, ok := val.(*RouteMetrics)
	if !ok {
		return
	}
	m.Calls.Add(1)
	if fault {
		m.Faults.Add(1)
	}
}

// Snapshot structurally maps the exact telemetry block for the dashboard natively
func (rt *RouteTracker) Snapshot() []map[string]any {
	var out []map[string]any
	rt.routes.Range(func(key, value any) bool {
		k, ok := key.(string)
		if !ok {
			return true
		}
		parts := strings.SplitN(k, "|", 2)
		if len(parts) == 2 {
			m, ok := value.(*RouteMetrics)
			if !ok {
				return true
			}
			out = append(out, map[string]any{
				"source": parts[0],
				"target": parts[1],
				"calls":  m.Calls.Load(),
				"faults": m.Faults.Load(),
			})
		}
		return true
	})
	return out
}
