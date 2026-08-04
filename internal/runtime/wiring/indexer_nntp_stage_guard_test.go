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

type fakeProfileNNTPTrafficBacklogReader struct {
	yenc            int64
	maxPriorityRank *int
}

func (f fakeProfileNNTPTrafficBacklogReader) CountPendingYEncRecoveryBinaries(context.Context) (int64, error) {
	return f.yenc, nil
}

func (f fakeProfileNNTPTrafficBacklogReader) CountPendingYEncRecoveryBinariesByMaxPriority(_ context.Context, maxPriorityRank int) (int64, error) {
	if f.maxPriorityRank != nil {
		*f.maxPriorityRank = maxPriorityRank
	}
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

func TestNNTPTrafficGuardKeepsScrapeLatestAheadOfRecoveryBacklog(t *testing.T) {
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
	if !decision.Allowed {
		t.Fatalf("expected scrape_latest to remain allowed, got %+v", decision)
	}
}

func TestNNTPTrafficGuardBlocksRecoveryWhileScrapeIsUsingHotPool(t *testing.T) {
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
				Scopes: []app.NNTPScopeRuntimeStats{{
					Scope:  "scrape",
					Active: 40,
				}},
			}
		},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageRecoverYEnc}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected recover_yenc to yield to scrape traffic, got %+v", decision)
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

func TestNNTPTrafficGuardUsesProfileEligibleRecoveryBacklog(t *testing.T) {
	for _, tt := range []struct {
		name        string
		profile     string
		wantMaxRank int
	}{
		{name: "balanced", profile: app.IndexingRecoveryProfileBalanced, wantMaxRank: 0},
		{name: "exhaustive", profile: app.IndexingRecoveryProfileExhaustive, wantMaxRank: 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gotMaxRank := -1
			guard := &cachedNNTPTrafficGuard{
				settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
					NNTPPool: &app.NNTPPoolRuntimeSettings{IndexerStageTargetPercent: 90},
					Indexing: &app.IndexingRuntimeSettings{
						RecoveryProfile: tt.profile,
						ScrapeLatest:    app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
						RecoverYEnc:     app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
					},
				}},
				repo: fakeProfileNNTPTrafficBacklogReader{
					yenc:            6000,
					maxPriorityRank: &gotMaxRank,
				},
				statsFn: func() app.NNTPRuntimeStats {
					return app.NNTPRuntimeStats{Capacity: 50, Active: 47, Waiting: 3}
				},
				lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
			}

			if _, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeLatest}, "scheduled"); err != nil {
				t.Fatalf("allowStage returned error: %v", err)
			}
			if gotMaxRank != tt.wantMaxRank {
				t.Fatalf("profile %q counted through priority %d, want %d", tt.profile, gotMaxRank, tt.wantMaxRank)
			}
		})
	}
}

func TestNNTPTrafficGuardHeaderOnlyIgnoresRecoveryBacklog(t *testing.T) {
	gotMaxRank := -1
	guard := &cachedNNTPTrafficGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			NNTPPool: &app.NNTPPoolRuntimeSettings{IndexerStageTargetPercent: 90},
			Indexing: &app.IndexingRuntimeSettings{
				RecoveryProfile: app.IndexingRecoveryProfileHeaderOnly,
				ScrapeLatest:    app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
				RecoverYEnc:     app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
			},
		}},
		repo: fakeProfileNNTPTrafficBacklogReader{
			yenc:            6000,
			maxPriorityRank: &gotMaxRank,
		},
		statsFn: func() app.NNTPRuntimeStats {
			return app.NNTPRuntimeStats{Capacity: 50, Active: 47, Waiting: 3}
		},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeLatest}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if gotMaxRank != -1 {
		t.Fatalf("header-only profile queried recovery backlog through priority %d", gotMaxRank)
	}
	if !decision.Allowed {
		t.Fatalf("header-only profile was blocked by dormant recovery work: %+v", decision)
	}
}
