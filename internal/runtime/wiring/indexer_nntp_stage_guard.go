package wiring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

const nntpStageGuardRefreshInterval = 5 * time.Second

type nntpStageBacklogReader interface {
	CountPendingYEncRecoveryBinaries(ctx context.Context) (int64, error)
}

type cachedNNTPTrafficGuard struct {
	settingsStore app.SettingsStore
	repo          nntpStageBacklogReader
	statsFn       func() app.NNTPRuntimeStats

	mu          sync.Mutex
	lastCheck   time.Time
	lastResults map[supervisor.StageName]supervisor.StageGateDecision
}

func newIndexerNNTPTrafficGuard(appCtx *app.Context, statsFn func() app.NNTPRuntimeStats) supervisor.StageGateFunc {
	if appCtx == nil || appCtx.SettingsStore == nil || appCtx.PGIndexStore == nil || statsFn == nil {
		return nil
	}
	repo, ok := appCtx.PGIndexStore.(nntpStageBacklogReader)
	if !ok {
		return nil
	}
	guard := &cachedNNTPTrafficGuard{
		settingsStore: appCtx.SettingsStore,
		repo:          repo,
		statsFn:       statsFn,
		lastResults:   make(map[supervisor.StageName]supervisor.StageGateDecision),
	}
	return guard.allowStage
}

func (g *cachedNNTPTrafficGuard) allowStage(ctx context.Context, stage supervisor.Stage, trigger string) (supervisor.StageGateDecision, error) {
	if trigger == "manual" || !nntpGuardApplies(stage.Name) {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}

	g.mu.Lock()
	if time.Since(g.lastCheck) < nntpStageGuardRefreshInterval {
		if result, ok := g.lastResults[stage.Name]; ok {
			g.mu.Unlock()
			return result, nil
		}
		g.mu.Unlock()
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
	g.mu.Unlock()

	runtime, err := g.settingsStore.GetRuntimeSettings(ctx)
	if err != nil {
		return supervisor.StageGateDecision{}, fmt.Errorf("load runtime settings for nntp stage guard: %w", err)
	}
	results, err := g.evaluate(ctx, runtime)
	if err != nil {
		return supervisor.StageGateDecision{}, err
	}

	g.mu.Lock()
	g.lastCheck = time.Now()
	g.lastResults = results
	result, ok := results[stage.Name]
	g.mu.Unlock()
	if !ok {
		return supervisor.StageGateDecision{Allowed: true}, nil
	}
	return result, nil
}

func (g *cachedNNTPTrafficGuard) evaluate(ctx context.Context, runtime *app.RuntimeSettings) (map[supervisor.StageName]supervisor.StageGateDecision, error) {
	results := make(map[supervisor.StageName]supervisor.StageGateDecision)
	if runtime == nil || runtime.Indexing == nil {
		return results, nil
	}

	stats := g.statsFn()
	if !nntpPoolHot(runtime.NNTPPool, stats) {
		return results, nil
	}

	yencBacklog, err := g.repo.CountPendingYEncRecoveryBinaries(ctx)
	if err != nil {
		return nil, fmt.Errorf("count pending yenc recovery backlog for nntp guard: %w", err)
	}
	yencHot := runtime.Indexing.RecoverYEnc.Enabled && yencBacklog >= yencHotThreshold(runtime.Indexing)

	scopeActivity := make(map[string]app.NNTPScopeRuntimeStats, len(stats.Scopes))
	for _, scope := range stats.Scopes {
		scopeActivity[scope.Scope] = scope
	}

	// Lowest priority: PAR2 inspection yields whenever the pool is hot and higher-priority
	// NNTP work is active or clearly available.
	if runtime.Indexing.InspectPAR2.Enabled && (yencHot || scopeHot(scopeActivity, "recover_yenc") || scopeHot(scopeActivity, "scrape")) {
		results[supervisor.StageInspectPAR2] = supervisor.StageGateDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("inspect_par2 paused for nntp catch-up: active=%d capacity=%d waiting=%d", stats.Active, stats.Capacity, stats.Waiting),
		}
	}

	// Backfill is lower priority than latest freshness and yEnc identity recovery.
	if runtime.Indexing.ScrapeBackfill.Enabled && (yencHot || runtime.Indexing.ScrapeLatest.Enabled || scopeHot(scopeActivity, "recover_yenc")) {
		results[supervisor.StageScrapeBackfill] = supervisor.StageGateDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("scrape_backfill paused for nntp catch-up: active=%d capacity=%d waiting=%d", stats.Active, stats.Capacity, stats.Waiting),
		}
	}

	// Latest scraping yields only when yEnc has a meaningful backlog and the pool is already hot.
	if runtime.Indexing.ScrapeLatest.Enabled && yencHot {
		results[supervisor.StageScrapeLatest] = supervisor.StageGateDecision{
			Allowed: false,
			Reason:  fmt.Sprintf("scrape_latest paused for recover_yenc catch-up: pending_yenc=%d active=%d capacity=%d", yencBacklog, stats.Active, stats.Capacity),
		}
	}

	return results, nil
}

func nntpGuardApplies(stageName supervisor.StageName) bool {
	switch stageName {
	case supervisor.StageScrapeLatest, supervisor.StageScrapeBackfill, supervisor.StageRecoverYEnc, supervisor.StageInspectPAR2:
		return true
	default:
		return false
	}
}

func nntpPoolHot(pool *app.NNTPPoolRuntimeSettings, stats app.NNTPRuntimeStats) bool {
	if stats.Capacity <= 0 {
		return false
	}
	targetPercent := 90
	if pool != nil && pool.IndexerStageTargetPercent > 0 {
		targetPercent = pool.IndexerStageTargetPercent
	}
	targetActive := stats.Capacity * targetPercent / 100
	if targetActive <= 0 {
		targetActive = 1
	}
	return stats.Waiting > 0 || stats.Active >= targetActive
}

func yencHotThreshold(indexing *app.IndexingRuntimeSettings) int64 {
	if indexing == nil {
		return 1000
	}
	batch := indexing.RecoverYEnc.BatchSize
	if batch <= 0 {
		batch = 1000
	}
	threshold := int64(batch * 4)
	if threshold < 1000 {
		threshold = 1000
	}
	return threshold
}

func scopeHot(scopes map[string]app.NNTPScopeRuntimeStats, scope string) bool {
	stats, ok := scopes[scope]
	if !ok {
		return false
	}
	return stats.Active > 0 || stats.Waiting > 0
}
