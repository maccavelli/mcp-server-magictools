package intelligence

import (
	"context"
	"log/slog"
	"time"

	"github.com/maccavelli/mcp-server-magictools/internal/config"
	"github.com/maccavelli/mcp-server-magictools/internal/db"
	"github.com/maccavelli/mcp-server-magictools/internal/llm"
)

// StartHydratorDaemon spins up the permanently resident background worker that
// exclusively owns the intelligence execution lifecycle. It strictly serializes
// LLM tool sweep batches and contextual empirical database updates, preventing
// overlapping lock contention and memory exhaustion.
func StartHydratorDaemon(ctx context.Context, store *db.Store, cfg *config.Config, rc RecallMiner, triggerChan <-chan struct{}, pool *llm.Pool) {
	slog.Info("hydrator daemon initialized", "component", "hydrator_daemon")

	// Load providers once completely bypassing constant initialization overhead.
	// When pool is available, use its rate-limited provider for isolation.
	providers := initProviders(ctx, cfg, pool)

	// Create the calibration ticker for periodic Recall ML mining
	calibrationTicker := time.NewTicker(5 * time.Minute)
	defer calibrationTicker.Stop()

	// Wait for the initial 30s delay before mining recall (mirrors previous startBackgroundWorkers delay)
	recallDelayTimer := time.NewTimer(30 * time.Second)
	defer recallDelayTimer.Stop()
	recallMiningReady := false

	// Continuous event stream processor
	for {
		select {
		case <-ctx.Done():
			slog.Info("hydrator daemon shutting down gracefully", "component", "hydrator_daemon")
			return

		case <-triggerChan:
			// Drain any stacked triggers from channel to deduplicate bursts.
			drainTriggers(triggerChan)

			// Execute the continuous batch processing loop natively
			for {
				hasMore := RunSweep(ctx, store, cfg, providers)
				if !hasMore {
					break
				}

				// Critical Context-Aware Rate Pacing (2.5s) to allow LLM limits to reset
				pacer := time.NewTimer(2500 * time.Millisecond)
				select {
				case <-ctx.Done():
					pacer.Stop()
					return
				case <-pacer.C:
					// Proceed instantly to the next iteration chunk upon tick completion
				}
			}

		case <-recallDelayTimer.C:
			// Initial activation of RecallMiner
			recallMiningReady = true
			if rc != nil && rc.RecallEnabled() {
				slog.Debug("hydrator daemon initiating first recall calibration tick", "component", "hydrator_daemon")
				MineRecallPatterns(ctx, rc, store)
				CalibrateFromRecall(ctx, rc, store)
			}

		case <-calibrationTicker.C:
			if recallMiningReady && rc != nil && rc.RecallEnabled() {
				slog.Debug("hydrator daemon initiating scheduled recall calibration tick", "component", "hydrator_daemon")
				MineRecallPatterns(ctx, rc, store)
				CalibrateFromRecall(ctx, rc, store)
			}
		}
	}
}

// drainTriggers flushes any immediately pending items in the channel without blocking
// to naturally consolidate overlapping sync events.
func drainTriggers(ch <-chan struct{}) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}
