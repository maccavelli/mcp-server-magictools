package handler

import (
	"log/slog"
	"sync"
)

// PROXY-L6: a bounded worker pool for fire-and-forget persistence tasks (usage
// counters, error metrics, raw-response caching). These previously each spawned
// an unbounded `go` per proxy call, which piled up under burst. The pool caps
// concurrency and sheds tasks when saturated (mirroring the proxy-metrics queue)
// rather than letting goroutines accumulate.
const (
	bgQueueSize = 2048
	bgWorkers   = 8
)

var (
	bgQueue     chan func()
	bgQueueOnce sync.Once
)

func bgPool() chan func() {
	bgQueueOnce.Do(func() {
		bgQueue = make(chan func(), bgQueueSize)
		for range bgWorkers {
			go func() {
				for task := range bgQueue {
					runBackgroundTask(task)
				}
			}()
		}
	})
	return bgQueue
}

func runBackgroundTask(task func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("gateway: background persistence task panicked", "panic", r)
		}
	}()
	task()
}

// submitBackground runs fn on the bounded persistence worker pool. If the queue
// is saturated the task is shed (logged) instead of spawning an unbounded
// goroutine. Never blocks the caller — safe to call from the dispatch path.
func submitBackground(fn func()) {
	select {
	case bgPool() <- fn:
	default:
		slog.Warn("gateway: background persistence queue full, task shed")
	}
}
