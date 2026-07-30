package telemetry

import (
	"sync"
)

// CollisionEvent captures one align_tools search event with confidence gap data.
type CollisionEvent struct {
	Timestamp int64   `json:"timestamp"`
	Query     string  `json:"query"`
	S1URN     string  `json:"s1_urn"`
	S1Score   float64 `json:"s1_score"`
	S2URN     string  `json:"s2_urn"`
	S2Score   float64 `json:"s2_score"`
	Gap       float64 `json:"gap"`
	Collision bool    `json:"collision"`
}

// CollisionSnapshot is the serializable output written to the ring buffer.
type CollisionSnapshot struct {
	Events        []CollisionEvent       `json:"events"`
	TotalEvents   int64                  `json:"total_events"`
	TotalCollides int64                  `json:"total_collisions"`
	AvgGap        float64                `json:"avg_gap"`
	Trend         string                 `json:"trend"`
	TopPairs      []CollisionPairSummary `json:"top_pairs"`
}

// CollisionPairSummary aggregates how often two tools collide.
type CollisionPairSummary struct {
	URNA  string `json:"urn_a"`
	URNB  string `json:"urn_b"`
	Count int    `json:"count"`
}

// CollisionTracker maintains a bounded sliding window of search events
// with pre-computed trend direction for the CLI dashboard.
type CollisionTracker struct {
	mu          sync.Mutex
	events      []CollisionEvent
	maxEvents   int
	totalEvents int64
	totalColls  int64
	pairCounts  map[string]int // "urnA|urnB" -> count
}

// NewCollisionTracker creates a tracker with a bounded window size.
func NewCollisionTracker(maxEvents int) *CollisionTracker {
	return &CollisionTracker{
		maxEvents:  maxEvents,
		events:     make([]CollisionEvent, 0, maxEvents),
		pairCounts: make(map[string]int),
	}
}

// Record appends a collision event to the sliding window.
func (ct *CollisionTracker) Record(event CollisionEvent) {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.totalEvents++
	if event.Collision {
		ct.totalColls++
	}

	// Track pair frequency
	if event.Collision {
		key := event.S1URN + "|" + event.S2URN
		ct.pairCounts[key]++
	}

	// Bounded append: evict oldest when full.
	if len(ct.events) >= ct.maxEvents {
		// IDX-12: decrement the evicted event's pair count so pairCounts stays
		// bounded alongside the event window (previously it grew unbounded).
		old := ct.events[0]
		if old.Collision {
			key := old.S1URN + "|" + old.S2URN
			if ct.pairCounts[key] <= 1 {
				delete(ct.pairCounts, key)
			} else {
				ct.pairCounts[key]--
			}
		}
		ct.events = ct.events[1:]
	}
	ct.events = append(ct.events, event)
}

// Snapshot returns a serializable copy for the ring buffer.
func (ct *CollisionTracker) Snapshot() CollisionSnapshot {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	snap := CollisionSnapshot{
		Events:        make([]CollisionEvent, len(ct.events)),
		TotalEvents:   ct.totalEvents,
		TotalCollides: ct.totalColls,
		Trend:         ct.trendLocked(),
	}
	copy(snap.Events, ct.events)

	// Compute average gap
	if len(ct.events) > 0 {
		var sum float64
		for _, e := range ct.events {
			sum += e.Gap
		}
		snap.AvgGap = sum / float64(len(ct.events))
	}

	// Extract top collision pairs (up to 5)
	snap.TopPairs = ct.topPairsLocked(5)

	return snap
}

// trendLocked computes trend from the last 10 events (must hold mu).
func (ct *CollisionTracker) trendLocked() string {
	n := len(ct.events)
	if n < 6 {
		return "insufficient_data"
	}

	// Compare average gap of first half vs second half of recent events
	mid := n / 2
	var firstHalf, secondHalf float64
	for i := range mid {
		firstHalf += ct.events[i].Gap
	}
	for i := mid; i < n; i++ {
		secondHalf += ct.events[i].Gap
	}
	firstHalf /= float64(mid)
	secondHalf /= float64(n - mid)

	delta := secondHalf - firstHalf
	switch {
	case delta > 0.01:
		return "improving"
	case delta < -0.01:
		return "degrading"
	default:
		return "stable"
	}
}

// topPairsLocked returns the most frequent collision pairs (must hold mu).
func (ct *CollisionTracker) topPairsLocked(limit int) []CollisionPairSummary {
	if len(ct.pairCounts) == 0 {
		return nil
	}

	pairs := make([]CollisionPairSummary, 0, len(ct.pairCounts))
	for key, count := range ct.pairCounts {
		parts := splitPairKey(key)
		pairs = append(pairs, CollisionPairSummary{
			URNA:  parts[0],
			URNB:  parts[1],
			Count: count,
		})
	}

	// Sort by count descending
	for i := 0; i < len(pairs); i++ {
		for j := i + 1; j < len(pairs); j++ {
			if pairs[j].Count > pairs[i].Count {
				pairs[i], pairs[j] = pairs[j], pairs[i]
			}
		}
	}

	if len(pairs) > limit {
		pairs = pairs[:limit]
	}
	return pairs
}

// splitPairKey splits "urnA|urnB" into [urnA, urnB].
func splitPairKey(key string) [2]string {
	for i := range key {
		if key[i] == '|' {
			return [2]string{key[:i], key[i+1:]}
		}
	}
	return [2]string{key, ""}
}

// Collisions is the global collision tracker instance.
var Collisions = NewCollisionTracker(20)
