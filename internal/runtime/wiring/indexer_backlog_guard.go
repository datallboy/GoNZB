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
	scrapeLatestTrickleInterval       = 5 * time.Minute
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

	mu                sync.Mutex
	cond              *sync.Cond
	evaluating        bool
	lastCheck         time.Time
	lastResults       map[supervisor.StageName]supervisor.StageGateDecision
	blocked           bool
	lastLatestTrickle time.Time
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
		lastResults:   make(map[supervisor.StageName]supervisor.StageGateDecision),
	}
	guard.cond = sync.NewCond(&guard.mu)
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
	if g.cond == nil {
		g.cond = sync.NewCond(&g.mu)
	}
	for g.evaluating {
		g.cond.Wait()
	}
	if time.Since(g.lastCheck) < scrapeBacklogGuardRefreshInterval {
		result, ok := g.lastResults[stage.Name]
		g.mu.Unlock()
		if ok {
			return result, nil
		}
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
		return supervisor.StageGateDecision{}, fmt.Errorf("load runtime settings for scrape backlog guard: %w", err)
	}
	decisions, err := g.evaluate(ctx, runtime)
	if err != nil {
		g.mu.Lock()
		g.evaluating = false
		g.cond.Broadcast()
		g.mu.Unlock()
		return supervisor.StageGateDecision{}, err
	}

	g.mu.Lock()
	g.lastCheck = time.Now()
	g.lastResults = decisions
	decision, ok := decisions[stage.Name]
	g.evaluating = false
	g.cond.Broadcast()
	g.mu.Unlock()
	if !ok {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
	return decision, nil
}

func (g *cachedScrapeBacklogGuard) evaluate(ctx context.Context, runtime *app.RuntimeSettings) (map[supervisor.StageName]supervisor.StageGateDecision, error) {
	allowed := map[supervisor.StageName]supervisor.StageGateDecision{
		supervisor.StageScrapeLatest:   {Allowed: true},
		supervisor.StageScrapeBackfill: {Allowed: true},
	}
	if runtime == nil || runtime.Indexing == nil {
		return allowed, nil
	}
	if !assembleEnabled(runtime.Indexing) {
		g.blocked = false
		return allowed, nil
	}

	highWater, lowWater := scrapeBacklogThresholds(runtime.Indexing)
	estimated, err := g.repo.EstimateUnassembledArticleHeaders(ctx)
	if err != nil {
		return nil, fmt.Errorf("estimate unassembled article header backlog: %w", err)
	}
	backlog := estimated
	if backlog == 0 {
		exact, err := g.repo.CountUnassembledArticleHeaders(ctx)
		if err != nil {
			return nil, fmt.Errorf("count unassembled article header backlog: %w", err)
		}
		backlog = exact
	}

	g.mu.Lock()
	blocked := g.blocked
	g.mu.Unlock()

	if blocked || backlog > lowWater {
		if backlog > lowWater {
			label := "resume_threshold"
			threshold := lowWater
			if !blocked && backlog >= highWater {
				label = "high_water"
				threshold = highWater
			}
			g.mu.Lock()
			g.blocked = true
			g.mu.Unlock()
			return g.scrapeBlockedDecisions(backlog, threshold, label), nil
		}
		g.mu.Lock()
		g.blocked = false
		g.mu.Unlock()
		return allowed, nil
	}

	return allowed, nil
}

func (g *cachedScrapeBacklogGuard) scrapeBlockedDecisions(backlog, threshold int64, thresholdLabel string) map[supervisor.StageName]supervisor.StageGateDecision {
	reason := fmt.Sprintf("scrape paused for assemble catch-up: unassembled_headers=%d %s=%d", backlog, thresholdLabel, threshold)
	decisions := map[supervisor.StageName]supervisor.StageGateDecision{
		supervisor.StageScrapeLatest:   {Allowed: false, Reason: reason},
		supervisor.StageScrapeBackfill: {Allowed: false, Reason: reason},
	}
	now := time.Now()

	g.mu.Lock()
	lastTrickle := g.lastLatestTrickle
	if lastTrickle.IsZero() || now.Sub(lastTrickle) >= scrapeLatestTrickleInterval {
		g.lastLatestTrickle = now
		decisions[supervisor.StageScrapeLatest] = supervisor.StageGateDecision{Allowed: true}
	}
	g.mu.Unlock()

	return decisions
}

func assembleEnabled(indexing *app.IndexingRuntimeSettings) bool {
	if indexing == nil {
		return false
	}
	return indexing.Assemble.Enabled
}

func scrapeBacklogThresholds(indexing *app.IndexingRuntimeSettings) (highWater int64, lowWater int64) {
	if indexing == nil {
		return scrapeBacklogGuardMinHighWater, scrapeBacklogGuardMinLowWater
	}
	capacity := 0
	for _, stage := range []app.IndexingStageRuntimeSettings{indexing.Assemble} {
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
