package wiring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type databaseStorageStatusReader interface {
	DatabaseStorageStatus(ctx context.Context) (*pgindex.DatabaseStorageStatus, error)
}

type cachedStorageGuard struct {
	repo       databaseStorageStatusReader
	config     pgindex.DatabaseStorageGuardConfig
	lastCheck  time.Time
	lastResult supervisor.StageGateDecision
	mu         sync.Mutex
}

func newIndexerStageStorageGuard(repo databaseStorageStatusReader, cfg pgindex.DatabaseStorageGuardConfig) supervisor.StageGateFunc {
	if repo == nil || !cfg.Enabled {
		return nil
	}
	guard := &cachedStorageGuard{repo: repo, config: cfg}
	return guard.allowStage
}

func (g *cachedStorageGuard) allowStage(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
	if shouldAlwaysAllowOnLowDBSpace(stage.Name) {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	g.mu.Lock()
	if time.Since(g.lastCheck) < 15*time.Second {
		result := g.lastResult
		g.mu.Unlock()
		return result, nil
	}
	g.mu.Unlock()

	status, err := g.repo.DatabaseStorageStatus(ctx)
	if err != nil {
		return supervisor.StageGateDecision{}, fmt.Errorf("check postgres storage status: %w", err)
	}
	evaluation := pgindex.EvaluateDatabaseStorageGuard(*status, g.config)
	decision := supervisor.StageGateDecision{
		Allowed: !evaluation.Blocked,
		Reason:  evaluation.Reason,
	}

	g.mu.Lock()
	g.lastCheck = time.Now()
	g.lastResult = decision
	g.mu.Unlock()
	return decision, nil
}

func shouldAlwaysAllowOnLowDBSpace(name supervisor.StageName) bool {
	switch name {
	case supervisor.StageReleaseGenerateNZB,
		supervisor.StageReleaseArchiveNZB,
		supervisor.StageReleasePurgeArchivedSources,
		supervisor.StageMaintenance:
		return true
	default:
		return false
	}
}
