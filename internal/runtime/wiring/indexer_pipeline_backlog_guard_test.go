package wiring

import (
	"context"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/infra/config"
)

type fakePipelineSettingsStore struct {
	runtime *app.RuntimeSettings
}

func (f fakePipelineSettingsStore) LoadEffectiveSettings(context.Context, *config.Config) (*config.Config, error) {
	return nil, nil
}

func (f fakePipelineSettingsStore) GetRuntimeSettings(context.Context, ...*config.Config) (*app.RuntimeSettings, error) {
	return f.runtime, nil
}

func (f fakePipelineSettingsStore) UpdateSettings(context.Context, *app.RuntimeSettings) error {
	return nil
}

func (f fakePipelineSettingsStore) WatchSettingsChanges(context.Context) (<-chan struct{}, error) {
	return nil, nil
}

func (f fakePipelineSettingsStore) Ping(context.Context) error { return nil }
func (f fakePipelineSettingsStore) SchemaVersion(context.Context) (int, error) {
	return 0, nil
}
func (f fakePipelineSettingsStore) ExpectedSchemaVersion() int           { return 0 }
func (f fakePipelineSettingsStore) ValidateSchema(context.Context) error { return nil }

type fakePipelineBacklogReader struct {
	assembleEstimate int64
	assemble         int64
	yenc             int64
	refresh          int
	ready            int64
}

func (f fakePipelineBacklogReader) EstimateUnassembledArticleHeaders(context.Context) (int64, error) {
	if f.assembleEstimate > 0 {
		return f.assembleEstimate, nil
	}
	return f.assemble, nil
}
func (f fakePipelineBacklogReader) CountPendingYEncRecoveryBinaries(context.Context) (int64, error) {
	return f.yenc, nil
}
func (f fakePipelineBacklogReader) CountQueuedReleaseFamilySummaries(context.Context) (int, error) {
	return f.refresh, nil
}
func (f fakePipelineBacklogReader) CountPendingReleaseCandidateFamilies(context.Context) (int64, error) {
	return f.ready, nil
}

func TestPipelineBacklogGuardBlocksRefreshWhenReleaseReadyBacklogIsHigh(t *testing.T) {
	guard := &cachedPipelineBacklogGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				ReleaseSummaryRefresh: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
				Release:               app.IndexingReleaseRuntimeSettings{Enabled: true, BatchSize: 200},
			},
		}},
		repo:        fakePipelineBacklogReader{ready: 2000},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageReleaseSummaryRefresh}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected release summary refresh to be blocked, got %+v", decision)
	}
}

func TestPipelineBacklogGuardBlocksHeavyInspectWhenCoreBacklogIsHot(t *testing.T) {
	guard := &cachedPipelineBacklogGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				AssembleLaneA: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
				RecoverYEnc:   app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
				Release:       app.IndexingReleaseRuntimeSettings{Enabled: true, BatchSize: 200},
			},
		}},
		repo:        fakePipelineBacklogReader{assembleEstimate: 200000},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageInspectArchive}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected heavy inspect stage to be blocked, got %+v", decision)
	}
}

func TestPipelineBacklogGuardAllowsDiscoveryAndPAR2WhenCoreBacklogIsHot(t *testing.T) {
	guard := &cachedPipelineBacklogGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				AssembleLaneA: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
				RecoverYEnc:   app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
				Release:       app.IndexingReleaseRuntimeSettings{Enabled: true, BatchSize: 200},
			},
		}},
		repo:        fakePipelineBacklogReader{assembleEstimate: 200000},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	for _, stageName := range []supervisor.StageName{
		supervisor.StageInspectDiscovery,
		supervisor.StageInspectPAR2,
	} {
		decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: stageName}, "scheduled")
		if err != nil {
			t.Fatalf("allowStage(%s) returned error: %v", stageName, err)
		}
		if !decision.Allowed {
			t.Fatalf("expected %s to stay allowed, got %+v", stageName, decision)
		}
	}
}

func TestPipelineBacklogGuardAllowsReleaseToKeepRunning(t *testing.T) {
	guard := &cachedPipelineBacklogGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				ReleaseSummaryRefresh: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
				Release:               app.IndexingReleaseRuntimeSettings{Enabled: true, BatchSize: 200},
			},
		}},
		repo:        fakePipelineBacklogReader{ready: 2000},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageRelease}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected release to stay allowed, got %+v", decision)
	}
}

func TestPipelineBacklogGuardDefaultsToAllowedWhenNoBlockDecisionExists(t *testing.T) {
	guard := &cachedPipelineBacklogGuard{
		settingsStore: fakePipelineSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				ReleaseSummaryRefresh: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 1000},
				Release:               app.IndexingReleaseRuntimeSettings{Enabled: true, BatchSize: 200},
			},
		}},
		repo:        fakePipelineBacklogReader{ready: 0},
		lastResults: make(map[supervisor.StageName]supervisor.StageGateDecision),
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageReleaseSummaryRefresh}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected release summary refresh to default allowed, got %+v", decision)
	}
}
