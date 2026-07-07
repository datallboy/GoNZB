package wiring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

const indexerIntegrityGuardRefreshInterval = 5 * time.Minute

type indexerIntegrityReader interface {
	CheckCriticalIndexerIntegrity(ctx context.Context, ensureExtension bool) (*pgindex.IndexerIntegrityReport, error)
}

type cachedIndexerIntegrityGuard struct {
	repo      indexerIntegrityReader
	mu        sync.Mutex
	lastCheck time.Time
	decision  supervisor.StageGateDecision
}

func newIndexerIntegrityGuard(repo indexerIntegrityReader) supervisor.StageGateFunc {
	if repo == nil {
		return nil
	}
	guard := &cachedIndexerIntegrityGuard{
		repo:     repo,
		decision: supervisor.StageGateDecision{Allowed: true},
	}
	return guard.allowStage
}

func (g *cachedIndexerIntegrityGuard) allowStage(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
	if trigger == "manual" {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if !g.lastCheck.IsZero() && time.Since(g.lastCheck) < indexerIntegrityGuardRefreshInterval {
		return g.decision, nil
	}

	report, err := g.repo.CheckCriticalIndexerIntegrity(ctx, false)
	if err != nil {
		return supervisor.StageGateDecision{}, fmt.Errorf("check critical indexer integrity: %w", err)
	}
	g.lastCheck = time.Now()
	if report != nil && report.HasFailures() {
		g.decision = supervisor.StageGateDecision{
			Allowed: false,
			Reason:  "critical index integrity failed: " + report.FailureSummary(),
		}
		return g.decision, nil
	}
	g.decision = supervisor.StageGateDecision{Allowed: true}
	return g.decision, nil
}
