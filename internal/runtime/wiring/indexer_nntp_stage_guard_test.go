package wiring

import (
	"context"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

type fakeNNTPTrafficBacklogReader struct {
	yenc int64
}

func (f fakeNNTPTrafficBacklogReader) CountPendingYEncRecoveryBinaries(context.Context) (int64, error) {
	return f.yenc, nil
}

func TestNNTPTrafficGuardBlocksInspectPAR2WhenPoolHotAndRecoverActive(t *testing.T) {
	guard := &cachedNNTPTrafficGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			NNTPPool: &app.NNTPPoolRuntimeSettings{IndexerStageTargetPercent: 90},
			Indexing: &app.IndexingRuntimeSettings{
				RecoverYEnc: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
				InspectPAR2: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 100},
			},
		}},
		repo: fakeNNTPTrafficBacklogReader{yenc: 5000},
		statsFn: func() app.NNTPRuntimeStats {
			return app.NNTPRuntimeStats{
				Capacity: 100,
				Active:   92,
				Scopes: []app.NNTPScopeRuntimeStats{{
					Scope:  "recover_yenc",
					Active: 8,
				}},
			}
		},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageInspectPAR2}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected inspect_par2 to be blocked, got %+v", decision)
	}
}

func TestNNTPTrafficGuardBlocksScrapeBackfillWhenPoolHotAndLatestEnabled(t *testing.T) {
	guard := &cachedNNTPTrafficGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			NNTPPool: &app.NNTPPoolRuntimeSettings{IndexerStageTargetPercent: 90},
			Indexing: &app.IndexingRuntimeSettings{
				ScrapeLatest:   app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
				ScrapeBackfill: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
			},
		}},
		repo: fakeNNTPTrafficBacklogReader{},
		statsFn: func() app.NNTPRuntimeStats {
			return app.NNTPRuntimeStats{
				Capacity: 40,
				Active:   39,
			}
		},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeBackfill}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected scrape_backfill to be blocked, got %+v", decision)
	}
}

func TestNNTPTrafficGuardBlocksScrapeLatestWhenRecoverYEncBacklogIsHot(t *testing.T) {
	guard := &cachedNNTPTrafficGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			NNTPPool: &app.NNTPPoolRuntimeSettings{IndexerStageTargetPercent: 90},
			Indexing: &app.IndexingRuntimeSettings{
				ScrapeLatest: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
				RecoverYEnc:  app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
			},
		}},
		repo: fakeNNTPTrafficBacklogReader{yenc: 6000},
		statsFn: func() app.NNTPRuntimeStats {
			return app.NNTPRuntimeStats{
				Capacity: 50,
				Active:   47,
				Waiting:  3,
			}
		},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeLatest}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected scrape_latest to be blocked, got %+v", decision)
	}
}

func TestNNTPTrafficGuardAllowsStagesWhenPoolNotHot(t *testing.T) {
	guard := &cachedNNTPTrafficGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			NNTPPool: &app.NNTPPoolRuntimeSettings{IndexerStageTargetPercent: 90},
			Indexing: &app.IndexingRuntimeSettings{
				ScrapeLatest: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
				RecoverYEnc:  app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
			},
		}},
		repo: fakeNNTPTrafficBacklogReader{yenc: 6000},
		statsFn: func() app.NNTPRuntimeStats {
			return app.NNTPRuntimeStats{
				Capacity: 50,
				Active:   20,
			}
		},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeLatest}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected scrape_latest to be allowed, got %+v", decision)
	}
}
