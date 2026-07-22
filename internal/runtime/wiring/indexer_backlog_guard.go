package wiring

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

const (
	scrapeBacklogGuardRefreshInterval = 15 * time.Second
	scrapeBacklogGuardMinHighWater    = int64(50000)
	scrapeBacklogGuardMinLowWater     = int64(10000)
)

type unassembledBacklogReader interface {
	CountUnassembledArticleHeaders(ctx context.Context) (int64, error)
	CountBlockingYEncRecoveryBacklog(ctx context.Context) (int64, error)
	ConfigureYEncRecoveryAdmission(ctx context.Context, cfg pgindex.YEncRecoveryAdmissionConfig) error
	RefreshYEncRecoveryAdmissionSnapshot(ctx context.Context) (*pgindex.YEncRecoveryAdmissionSnapshot, error)
}

type cachedScrapeBacklogGuard struct {
	settingsStore app.SettingsStore
	repo          unassembledBacklogReader

	mu              sync.Mutex
	cond            *sync.Cond
	evaluating      bool
	lastCheck       time.Time
	lastResults     map[supervisor.StageName]supervisor.StageGateDecision
	assembleBlocked bool
	yencBlocked     bool
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
	if stage.Name != supervisor.StageScrapeLatest && stage.Name != supervisor.StageScrapeBackfill && stage.Name != supervisor.StageScrapeTimeframe {
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
		supervisor.StageScrapeLatest:    {Allowed: true},
		supervisor.StageScrapeBackfill:  {Allowed: true},
		supervisor.StageScrapeTimeframe: {Allowed: true},
	}
	if runtime == nil || runtime.Indexing == nil {
		return allowed, nil
	}
	if !assembleEnabled(runtime.Indexing) && !recoverYEncEnabled(runtime.Indexing) {
		g.mu.Lock()
		g.assembleBlocked = false
		g.yencBlocked = false
		g.mu.Unlock()
		return allowed, nil
	}

	if assembleEnabled(runtime.Indexing) {
		highWater, lowWater := scrapeBacklogThresholds(runtime.Indexing)
		backlog, err := g.repo.CountUnassembledArticleHeaders(ctx)
		if err != nil {
			return nil, fmt.Errorf("count unassembled article header backlog: %w", err)
		}

		g.mu.Lock()
		blocked := g.assembleBlocked
		g.mu.Unlock()

		if blocked || backlog >= highWater {
			if blocked && backlog <= lowWater {
				g.mu.Lock()
				g.assembleBlocked = false
				g.mu.Unlock()
			} else {
				label := "resume_threshold"
				threshold := lowWater
				if !blocked && backlog >= highWater {
					label = "high_water"
					threshold = highWater
				}
				g.mu.Lock()
				g.assembleBlocked = true
				g.mu.Unlock()
				return g.scrapeBlockedDecisions(backlog, threshold, label), nil
			}
		}
	} else {
		g.mu.Lock()
		g.assembleBlocked = false
		g.mu.Unlock()
	}

	if recoverYEncEnabled(runtime.Indexing) {
		if err := g.repo.ConfigureYEncRecoveryAdmission(ctx, yencAdmissionConfigFromRuntime(runtime.Indexing.RecoveryAdmission)); err != nil {
			return nil, fmt.Errorf("configure yenc recovery admission: %w", err)
		}
		snapshot, err := g.repo.RefreshYEncRecoveryAdmissionSnapshot(ctx)
		if err != nil {
			return nil, fmt.Errorf("refresh yenc recovery admission snapshot: %w", err)
		}
		if snapshot != nil && snapshot.OpenTotal >= snapshot.HardCap {
			g.mu.Lock()
			g.yencBlocked = true
			g.mu.Unlock()
			return yencHardCapBlockedDecisions(snapshot), nil
		}
		if snapshot != nil && snapshot.OpenTotal >= snapshot.SoftCap {
			g.mu.Lock()
			g.yencBlocked = false
			g.mu.Unlock()
			allowed[supervisor.StageScrapeBackfill] = supervisor.StageGateDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("scrape_backfill paused for recover_yenc capacity: open_yenc=%d soft_cap=%d hard_cap=%d", snapshot.OpenTotal, snapshot.SoftCap, snapshot.HardCap),
			}
			allowed[supervisor.StageScrapeTimeframe] = supervisor.StageGateDecision{
				Allowed: false,
				Reason:  fmt.Sprintf("scrape_timeframe paused for recover_yenc capacity: open_yenc=%d soft_cap=%d hard_cap=%d", snapshot.OpenTotal, snapshot.SoftCap, snapshot.HardCap),
			}
		}
	} else {
		g.mu.Lock()
		g.yencBlocked = false
		g.mu.Unlock()
	}

	return allowed, nil
}

func yencAdmissionConfigFromRuntime(in app.IndexingRecoveryAdmissionRuntimeSettings) pgindex.YEncRecoveryAdmissionConfig {
	return pgindex.YEncRecoveryAdmissionConfig{
		SoftQueueHours:              in.SoftQueueHours,
		HardQueueMultiplier:         in.HardQueueMultiplier,
		AbsoluteHardQueueCap:        int64(in.AbsoluteHardQueueCap),
		BootstrapProbesPerHour:      float64(in.BootstrapProbesPerHour),
		EWMAWindowMinutes:           in.EWMAWindowMinutes,
		Priority0OverflowCap:        int64(in.Priority0OverflowCap),
		Priority0ReservoirBatches:   in.Priority0ReservoirBatches,
		NearTimeCohortBucketMinutes: in.NearTimeCohortBucketMinutes,
		LatestReservePercent:        in.LatestReservePercent,
	}
}

func (g *cachedScrapeBacklogGuard) scrapeBlockedDecisions(backlog, threshold int64, thresholdLabel string) map[supervisor.StageName]supervisor.StageGateDecision {
	reason := fmt.Sprintf("scrape paused for assemble catch-up: unassembled_headers=%d %s=%d", backlog, thresholdLabel, threshold)
	return map[supervisor.StageName]supervisor.StageGateDecision{
		supervisor.StageScrapeLatest:    {Allowed: false, Reason: reason},
		supervisor.StageScrapeBackfill:  {Allowed: false, Reason: reason},
		supervisor.StageScrapeTimeframe: {Allowed: false, Reason: reason},
	}
}

func yencHardCapBlockedDecisions(snapshot *pgindex.YEncRecoveryAdmissionSnapshot) map[supervisor.StageName]supervisor.StageGateDecision {
	var openTotal, softCap, hardCap int64
	if snapshot != nil {
		openTotal = snapshot.OpenTotal
		softCap = snapshot.SoftCap
		hardCap = snapshot.HardCap
	}
	reason := fmt.Sprintf("scrape paused for recover_yenc hard cap: open_yenc=%d soft_cap=%d hard_cap=%d", openTotal, softCap, hardCap)
	return map[supervisor.StageName]supervisor.StageGateDecision{
		supervisor.StageScrapeLatest:    {Allowed: false, Reason: reason},
		supervisor.StageScrapeBackfill:  {Allowed: false, Reason: reason},
		supervisor.StageScrapeTimeframe: {Allowed: false, Reason: reason},
	}
}

func assembleEnabled(indexing *app.IndexingRuntimeSettings) bool {
	if indexing == nil {
		return false
	}
	return indexing.Assemble.Enabled
}

func recoverYEncEnabled(indexing *app.IndexingRuntimeSettings) bool {
	if indexing == nil {
		return false
	}
	return indexing.RecoverYEnc.Enabled
}

func scrapeBacklogThresholds(indexing *app.IndexingRuntimeSettings) (highWater int64, lowWater int64) {
	if indexing == nil {
		return scrapeBacklogGuardMinHighWater, scrapeBacklogGuardMinLowWater
	}
	if indexing.ScrapeTiers.AssembleBacklogHighWater > 0 {
		highWater = int64(indexing.ScrapeTiers.AssembleBacklogHighWater)
	} else {
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
		highWater = int64(capacity * 10)
		if indexing.ScrapeTiers.MaxArticlesPerGroupWindow > 0 {
			highWater = int64(indexing.ScrapeTiers.MaxArticlesPerGroupWindow * 2)
		}
		if highWater < scrapeBacklogGuardMinHighWater {
			highWater = scrapeBacklogGuardMinHighWater
		}
	}

	if indexing.ScrapeTiers.AssembleBacklogLowWater > 0 {
		lowWater = int64(indexing.ScrapeTiers.AssembleBacklogLowWater)
	} else {
		lowWater = highWater / 2
		if lowWater < scrapeBacklogGuardMinLowWater {
			lowWater = scrapeBacklogGuardMinLowWater
		}
	}
	if highWater < scrapeBacklogGuardMinHighWater {
		highWater = scrapeBacklogGuardMinHighWater
	}
	if lowWater < scrapeBacklogGuardMinLowWater {
		lowWater = scrapeBacklogGuardMinLowWater
	}
	if lowWater >= highWater {
		lowWater = highWater / 2
		if lowWater < scrapeBacklogGuardMinLowWater {
			lowWater = scrapeBacklogGuardMinLowWater
		}
	}
	return highWater, lowWater
}
