package wiring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

const (
	scrapeBacklogGuardRefreshInterval = 15 * time.Second
	scrapeBacklogGuardMinHighWater    = int64(50000)
	scrapeBacklogGuardMinLowWater     = int64(10000)
)

type unassembledBacklogReader interface {
	EstimateUnassembledArticleHeaders(ctx context.Context) (int64, error)
	CountUnassembledArticleHeaders(ctx context.Context) (int64, error)
}

type cachedScrapeBacklogGuard struct {
	settingsStore app.SettingsStore
	repo          unassembledBacklogReader

	mu         sync.Mutex
	lastCheck  time.Time
	lastResult supervisor.StageGateDecision
	blocked    bool
}

func newIndexerScrapeBacklogGuard(appCtx *app.Context) supervisor.StageGateFunc {
	if appCtx == nil || appCtx.SettingsStore == nil || appCtx.PGIndexStore == nil {
		return nil
	}
	repo, ok := appCtx.PGIndexStore.(unassembledBacklogReader)
	if !ok {
		return nil
	}
	guard := &cachedScrapeBacklogGuard{
		settingsStore: appCtx.SettingsStore,
		repo:          repo,
	}
	return guard.allowStage
}

func (g *cachedScrapeBacklogGuard) allowStage(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
	if stage.Name != supervisor.StageScrapeLatest && stage.Name != supervisor.StageScrapeBackfill {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
	if trigger == "manual" {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	g.mu.Lock()
	if time.Since(g.lastCheck) < scrapeBacklogGuardRefreshInterval {
		result := g.lastResult
		g.mu.Unlock()
		return result, nil
	}
	g.mu.Unlock()

	runtime, err := g.settingsStore.GetRuntimeSettings(ctx)
	if err != nil {
		return supervisor.StageGateDecision{}, fmt.Errorf("load runtime settings for scrape backlog guard: %w", err)
	}
	decision, err := g.evaluate(ctx, runtime)
	if err != nil {
		return supervisor.StageGateDecision{}, err
	}

	g.mu.Lock()
	g.lastCheck = time.Now()
	g.lastResult = decision
	g.mu.Unlock()
	return decision, nil
}

func (g *cachedScrapeBacklogGuard) evaluate(ctx context.Context, runtime *app.RuntimeSettings) (supervisor.StageGateDecision, error) {
	if runtime == nil || runtime.Indexing == nil {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
	if !assembleEnabled(runtime.Indexing) {
		g.blocked = false
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	highWater, lowWater := scrapeBacklogThresholds(runtime.Indexing)
	estimated, err := g.repo.EstimateUnassembledArticleHeaders(ctx)
	if err != nil {
		return supervisor.StageGateDecision{}, fmt.Errorf("estimate unassembled article header backlog: %w", err)
	}
	backlog := estimated
	if backlog == 0 {
		exact, err := g.repo.CountUnassembledArticleHeaders(ctx)
		if err != nil {
			return supervisor.StageGateDecision{}, fmt.Errorf("count unassembled article header backlog: %w", err)
		}
		backlog = exact
	}

	g.mu.Lock()
	blocked := g.blocked
	g.mu.Unlock()

	if blocked {
		if backlog > lowWater {
			return supervisor.StageGateDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("scrape paused for assemble catch-up: unassembled_headers=%d resume_threshold=%d", backlog, lowWater),
			}, nil
		}
		g.mu.Lock()
		g.blocked = false
		g.mu.Unlock()
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	if backlog >= highWater {
		g.mu.Lock()
		g.blocked = true
		g.mu.Unlock()
		return supervisor.StageGateDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("scrape paused for assemble catch-up: unassembled_headers=%d high_water=%d", backlog, highWater),
		}, nil
	}

	return supervisor.StageGateDecision{Allowed: true}, nil
}

func assembleEnabled(indexing *app.IndexingRuntimeSettings) bool {
	if indexing == nil {
		return false
	}
	return indexing.Assemble.Enabled || indexing.AssembleLaneA.Enabled || indexing.AssembleLaneB.Enabled
}

func scrapeBacklogThresholds(indexing *app.IndexingRuntimeSettings) (highWater int64, lowWater int64) {
	if indexing == nil {
		return scrapeBacklogGuardMinHighWater, scrapeBacklogGuardMinLowWater
	}
	capacity := 0
	for _, stage := range []app.IndexingStageRuntimeSettings{
		indexing.Assemble,
		indexing.AssembleLaneA,
		indexing.AssembleLaneB,
	} {
		if !stage.Enabled {
			continue
		}
		batchSize := stage.BatchSize
		if batchSize <= 0 {
			batchSize = 5000
		}
		capacity += batchSize
	}
	if capacity <= 0 {
		capacity = 5000
	}
	highWater = int64(capacity * 20)
	if highWater < scrapeBacklogGuardMinHighWater {
		highWater = scrapeBacklogGuardMinHighWater
	}
	lowWater = highWater / 2
	if lowWater < scrapeBacklogGuardMinLowWater {
		lowWater = scrapeBacklogGuardMinLowWater
	}
	return highWater, lowWater
}
