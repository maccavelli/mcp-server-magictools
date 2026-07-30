package telemetry

import (
	"sync/atomic"
)

// LifecycleCounters tracks sub-server orchestration events.
type LifecycleCounters struct {
	RestartsHealth      atomic.Int64
	RestartsOOM         atomic.Int64
	EvictionsEviction   atomic.Int64
	Reconnections       atomic.Int64
	ConfigReloads       atomic.Int64
	BackpressurePending atomic.Int64 // gauge for active queue
	BackpressureReject  atomic.Int64 // count for dropped
}

// LifecycleEvents is the global event counter.
var LifecycleEvents = &LifecycleCounters{}
