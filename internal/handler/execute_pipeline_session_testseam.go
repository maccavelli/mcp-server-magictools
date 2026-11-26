//go:build magictoolstestseam

package handler

import (
	"context"
	"log/slog"

	"github.com/maccavelli/mcp-server-magictools/internal/db"
)

// createPlannedForTest, rejectForTest, setPlanHashForTest, setStagesForTest are
// test-only helpers reached through the handler's bound store. They are plain
// functions (not methods on the non-local db type) and are called only from
// _test.go files.
func createPlannedForTest(ctx context.Context, p *db.PipelineSessionStore, rec *pipelineSessionRecord) error {
	return p.CreatePlanned(ctx, rec)
}

func rejectForTest(ctx context.Context, p *db.PipelineSessionStore, id string) (*pipelineSessionRecord, error) {
	return p.Reject(ctx, id, "test-instance")
}

func setPlanHashForTest(ctx context.Context, p *db.PipelineSessionStore, id, hash string) {
	rec, err := p.Get(ctx, id)
	if err != nil {
		return
	}
	rec.PlanHash = hash
	if err := p.OverwriteForTest(ctx, rec); err != nil {
		slog.Warn("execute_pipeline test helper: overwrite failed", keyError, err)
	}
}

func setStagesForTest(ctx context.Context, p *db.PipelineSessionStore, id string, stages []PipelineStep) {
	rec, err := p.Get(ctx, id)
	if err != nil {
		return
	}
	rec.Stages = toDBStages(stages)
	if err := p.OverwriteForTest(ctx, rec); err != nil {
		slog.Warn("execute_pipeline test helper: overwrite failed", keyError, err)
	}
}
