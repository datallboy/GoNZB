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

func newIndexerStageResourceGuard(repo databaseStorageStatusReader, storageCfg pgindex.DatabaseStorageGuardConfig, memoryCfg IndexerMemoryGuardConfig) supervisor.StageGateFunc {
	var gates []supervisor.StageGateFunc
	if repo != nil && storageCfg.Enabled {
		guard := &cachedStorageGuard{repo: repo, config: storageCfg}
		gates = append(gates, guard.allowStage)
	}
	if memoryCfg.Enabled {
		guard := &cachedMemoryGuard{config: memoryCfg}
		gates = append(gates, guard.allowStage)
	}
	if len(gates) == 0 {
		return nil
	}
	return func(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
		for _, gate := range gates {
			decision, err := gate(ctx, stage, trigger)
			if err != nil {
				return supervisor.StageGateDecision{}, err
			}
			if !decision.Allowed {
				return decision, nil
			}
		}
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
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
