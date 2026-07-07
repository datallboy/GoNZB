package wiring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

const pipelineBacklogGuardRefreshInterval = 15 * time.Second

type pipelineBacklogReader interface {
	EstimateUnassembledArticleHeaders(ctx context.Context) (int64, error)
	CountPendingYEncRecoveryBinaries(ctx context.Context) (int64, error)
	CountQueuedReleaseFamilySummaries(ctx context.Context) (int, error)
	CountPendingReleaseCandidateFamilies(ctx context.Context) (int64, error)
}

type cachedPipelineBacklogGuard struct {
	settingsStore app.SettingsStore
	repo          pipelineBacklogReader

	mu             sync.Mutex
	cond           *sync.Cond
	evaluating     bool
	lastCheck      time.Time
	lastResults    map[supervisor.StageName]supervisor.StageGateDecision
	refreshBlocked bool
}

func newIndexerPipelineBacklogGuard(appCtx *app.Context) supervisor.StageGateFunc {
	if appCtx == nil || appCtx.SettingsStore == nil || appCtx.PGIndexStore == nil {
		return nil
	}
	repo, ok := appCtx.PGIndexStore.(pipelineBacklogReader)
	if !ok {
		return nil
	}
	guard := &cachedPipelineBacklogGuard{
		settingsStore: appCtx.SettingsStore,
		repo:          repo,
		lastResults:   make(map[supervisor.StageName]supervisor.StageGateDecision),
	}
	guard.cond = sync.NewCond(&guard.mu)
	return guard.allowStage
}

func (g *cachedPipelineBacklogGuard) allowStage(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
	if trigger == "manual" || !pipelineGuardApplies(stage.Name) {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	g.mu.Lock()
	if g.cond == nil {
		g.cond = sync.NewCond(&g.mu)
	}
	for g.evaluating {
		g.cond.Wait()
	}
	if time.Since(g.lastCheck) < pipelineBacklogGuardRefreshInterval {
		if result, ok := g.lastResults[stage.Name]; ok {
			g.mu.Unlock()
			return result, nil
		}
		g.mu.Unlock()
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
	g.evaluating = true
	g.mu.Unlock()

	runtime, err := g.settingsStore.GetRuntimeSettings(ctx)
	if err != nil {
		g.mu.Lock()
		g.evaluating = false
		g.cond.Broadcast()
		g.mu.Unlock()
		return supervisor.StageGateDecision{}, fmt.Errorf("load runtime settings for pipeline backlog guard: %w", err)
	}
	results, err := g.evaluate(ctx, runtime)
	if err != nil {
		g.mu.Lock()
		g.evaluating = false
		g.cond.Broadcast()
		g.mu.Unlock()
		return supervisor.StageGateDecision{}, err
	}

	g.mu.Lock()
	g.lastCheck = time.Now()
	g.lastResults = results
	result, ok := results[stage.Name]
	g.evaluating = false
	g.cond.Broadcast()
	g.mu.Unlock()
	if !ok {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
	return result, nil
}

func (g *cachedPipelineBacklogGuard) evaluate(ctx context.Context, runtime *app.RuntimeSettings) (map[supervisor.StageName]supervisor.StageGateDecision, error) {
	results := make(map[supervisor.StageName]supervisor.StageGateDecision)
	if runtime == nil || runtime.Indexing == nil {
		return results, nil
	}

	releaseHigh, releaseLow := releaseReadyThresholds(runtime.Indexing)
	pendingReleaseCandidates, err := g.repo.CountPendingReleaseCandidateFamilies(ctx)
	if err != nil {
		return nil, fmt.Errorf("count pending release candidate families: %w", err)
	}
	if !runtime.Indexing.Release.Enabled {
		g.refreshBlocked = false
	} else if g.refreshBlocked {
		if pendingReleaseCandidates > releaseLow {
			results[supervisor.StageReleaseSummaryRefresh] = supervisor.StageGateDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("release_summary_refresh paused for release catch-up: ready_candidates=%d resume_threshold=%d", pendingReleaseCandidates, releaseLow),
			}
		} else {
			g.refreshBlocked = false
		}
	} else if pendingReleaseCandidates >= releaseHigh {
		g.refreshBlocked = true
		results[supervisor.StageReleaseSummaryRefresh] = supervisor.StageGateDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("release_summary_refresh paused for release catch-up: ready_candidates=%d high_water=%d", pendingReleaseCandidates, releaseHigh),
		}
	}

	return results, nil
}

func pipelineGuardApplies(stageName supervisor.StageName) bool {
	switch stageName {
	case supervisor.StageReleaseSummaryRefresh:
		return true
	default:
		return false
	}
}

func releaseReadyThresholds(indexing *app.IndexingRuntimeSettings) (highWater, lowWater int64) {
	if indexing == nil {
		return 1000, 500
	}
	batch := indexing.Release.BatchSize
	if batch <= 0 {
		batch = 2500
	}
	highWater = int64(batch * 5)
	if highWater < 1000 {
		highWater = 1000
	}
	lowWater = highWater / 2
	if lowWater < 250 {
		lowWater = 250
	}
	return highWater, lowWater
}
