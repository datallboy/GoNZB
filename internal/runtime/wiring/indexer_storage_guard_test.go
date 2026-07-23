package wiring

import (
	"context"
	"testing"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type fakeStorageGuardSettingsStore struct {
	runtime *app.RuntimeSettings
}

func (f fakeStorageGuardSettingsStore) GetRuntimeSettings(context.Context, ...*config.Config) (*app.RuntimeSettings, error) {
	return f.runtime, nil
}

func TestShouldAlwaysAllowOnLowDBSpaceOnlyAllowsArchiveAndPurgeStages(t *testing.T) {
	allowed := []supervisor.StageName{
		supervisor.StageReleaseArchiveNZB,
		supervisor.StageReleasePurgeArchivedSources,
		supervisor.StageMaintenanceReleaseSourcePurge,
	}
	for _, stage := range allowed {
		if !shouldAlwaysAllowOnLowDBSpace(stage) {
			t.Fatalf("expected %s to be allowed during low DB space", stage)
		}
	}

	blocked := []supervisor.StageName{
		supervisor.StageReleaseGenerateNZB,
		supervisor.StageMaintenance,
		supervisor.StageAssemble,
		supervisor.StageRecoverYEnc,
	}
	for _, stage := range blocked {
		if shouldAlwaysAllowOnLowDBSpace(stage) {
			t.Fatalf("expected %s to be blocked by low DB space guard", stage)
		}
	}
}

func TestCachedStorageGuardUsesRuntimeStorageSettings(t *testing.T) {
	runtime := app.DefaultRuntimeSettings()
	runtime.Indexing.StorageGuard.Enabled = true
	runtime.Indexing.StorageGuard.DataDirectory = "/runtime/pgdata"
	runtime.Indexing.StorageGuard.MinFreeBytes = 1234
	runtime.Indexing.StorageGuard.MinFreePercent = 7

	guard := &cachedStorageGuard{
		config: pgindex.DatabaseStorageGuardConfig{
			Enabled:        false,
			DataDirectory:  "/startup/pgdata",
			MinFreeBytes:   1,
			MinFreePercent: 1,
		},
		settingsStore: fakeStorageGuardSettingsStore{runtime: runtime},
	}

	cfg, err := guard.currentConfig(context.Background())
	if err != nil {
		t.Fatalf("currentConfig returned error: %v", err)
	}
	if !cfg.Enabled || cfg.DataDirectory != "/runtime/pgdata" || cfg.MinFreeBytes != 1234 || cfg.MinFreePercent != 7 {
		t.Fatalf("expected runtime storage guard settings, got %+v", cfg)
	}
}
