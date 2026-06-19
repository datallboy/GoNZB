package wiring

import (
	"context"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/infra/config"
)

type fakeBacklogSettingsStore struct {
	runtime *app.RuntimeSettings
}

func (f fakeBacklogSettingsStore) LoadEffectiveSettings(context.Context, *config.Config) (*config.Config, error) {
	return nil, nil
}

func (f fakeBacklogSettingsStore) GetRuntimeSettings(context.Context, ...*config.Config) (*app.RuntimeSettings, error) {
	return f.runtime, nil
}

func (f fakeBacklogSettingsStore) UpdateSettings(context.Context, *app.RuntimeSettings) error {
	return nil
}

func (f fakeBacklogSettingsStore) WatchSettingsChanges(context.Context) (<-chan struct{}, error) {
	return nil, nil
}

func (f fakeBacklogSettingsStore) Ping(context.Context) error { return nil }

func (f fakeBacklogSettingsStore) SchemaVersion(context.Context) (int, error) { return 0, nil }

func (f fakeBacklogSettingsStore) ExpectedSchemaVersion() int { return 0 }

func (f fakeBacklogSettingsStore) ValidateSchema(context.Context) error { return nil }

type fakeUnassembledBacklogReader struct {
	estimate int64
	count    int64
}

func (f fakeUnassembledBacklogReader) EstimateUnassembledArticleHeaders(context.Context) (int64, error) {
	return f.estimate, nil
}

func (f fakeUnassembledBacklogReader) CountUnassembledArticleHeaders(context.Context) (int64, error) {
	return f.count, nil
}

func TestScrapeBacklogGuardBlocksScheduledBackfillWhenAssembleEnabled(t *testing.T) {
	guard := &cachedScrapeBacklogGuard{
		settingsStore: fakeBacklogSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				Assemble: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
			},
		}},
		repo: fakeUnassembledBacklogReader{estimate: 200000},
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeBackfill}, "scheduled")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected scrape to be blocked, got %+v", decision)
	}
}

func TestScrapeBacklogGuardAllowsLatestTrickleDuringAssembleCatchup(t *testing.T) {
	guard := &cachedScrapeBacklogGuard{
		settingsStore: fakeBacklogSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				Assemble: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
			},
		}},
		repo: fakeUnassembledBacklogReader{estimate: 200000},
	}

	latest, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeLatest}, "scheduled")
	if err != nil {
		t.Fatalf("allow latest returned error: %v", err)
	}
	if !latest.Allowed {
		t.Fatalf("expected scrape_latest trickle to be allowed, got %+v", latest)
	}

	backfill, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeBackfill}, "scheduled")
	if err != nil {
		t.Fatalf("allow backfill returned error: %v", err)
	}
	if backfill.Allowed {
		t.Fatalf("expected scrape_backfill to stay blocked, got %+v", backfill)
	}
}

func TestScrapeBacklogGuardAllowsManualScrapeOverride(t *testing.T) {
	guard := &cachedScrapeBacklogGuard{
		settingsStore: fakeBacklogSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				Assemble: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
			},
		}},
		repo: fakeUnassembledBacklogReader{estimate: 200000},
	}

	decision, err := guard.allowStage(context.Background(), supervisor.Stage{Name: supervisor.StageScrapeBackfill}, "manual")
	if err != nil {
		t.Fatalf("allowStage returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected manual scrape to bypass backlog guard, got %+v", decision)
	}
}

func TestScrapeBacklogGuardUsesHysteresisBeforeReenabling(t *testing.T) {
	guard := &cachedScrapeBacklogGuard{
		settingsStore: fakeBacklogSettingsStore{runtime: &app.RuntimeSettings{
			Indexing: &app.IndexingRuntimeSettings{
				Assemble: app.IndexingStageRuntimeSettings{Enabled: true, BatchSize: 5000},
			},
		}},
		repo: fakeUnassembledBacklogReader{estimate: 200000},
	}

	stage := supervisor.Stage{Name: supervisor.StageScrapeBackfill}
	decision, err := guard.allowStage(context.Background(), stage, "scheduled")
	if err != nil {
		t.Fatalf("first allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected initial scrape block")
	}

	guard.lastCheck = guard.lastCheck.Add(-scrapeBacklogGuardRefreshInterval - 1)
	guard.repo = fakeUnassembledBacklogReader{estimate: 60000}

	decision, err = guard.allowStage(context.Background(), stage, "scheduled")
	if err != nil {
		t.Fatalf("second allowStage returned error: %v", err)
	}
	if decision.Allowed {
		t.Fatalf("expected hysteresis to keep scrape blocked above resume threshold, got %+v", decision)
	}

	guard.lastCheck = guard.lastCheck.Add(-scrapeBacklogGuardRefreshInterval - 1)
	guard.repo = fakeUnassembledBacklogReader{estimate: 5000}

	decision, err = guard.allowStage(context.Background(), stage, "scheduled")
	if err != nil {
		t.Fatalf("third allowStage returned error: %v", err)
	}
	if !decision.Allowed {
		t.Fatalf("expected scrape to resume below low-water mark, got %+v", decision)
	}
}
