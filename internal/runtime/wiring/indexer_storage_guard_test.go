package wiring

import (
	"context"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	settingsstore "github.com/datallboy/gonzb/internal/store/settings"
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

func TestLiveStorageGuardBlocksStageUsingRuntimeSettings(t *testing.T) {
	dsn := os.Getenv("GONZB_LIVE_STORAGE_GUARD_DSN")
	if dsn == "" {
		t.Skip("set GONZB_LIVE_STORAGE_GUARD_DSN to run live storage guard test")
	}
	settingsPath := os.Getenv("GONZB_LIVE_STORAGE_GUARD_SETTINGS_DB")
	if settingsPath == "" {
		t.Skip("set GONZB_LIVE_STORAGE_GUARD_SETTINGS_DB to run live storage guard test")
	}
	dataDir := os.Getenv("GONZB_LIVE_STORAGE_GUARD_DATA_DIR")
	if dataDir == "" {
		t.Skip("set GONZB_LIVE_STORAGE_GUARD_DATA_DIR to run live storage guard test")
	}

	store, err := pgindex.NewMaintenanceStore(dsn)
	if err != nil {
		t.Fatalf("open live pgindex store: %v", err)
	}
	defer store.Close()

	settings, err := settingsstore.NewStore(settingsPath)
	if err != nil {
		t.Fatalf("open live settings store: %v", err)
	}
	defer settings.Close()

	runtime, err := settings.GetRuntimeSettings(context.Background(), &config.Config{})
	if err != nil {
		t.Fatalf("load live runtime settings: %v", err)
	}
	if runtime == nil || runtime.Indexing == nil || runtime.Indexing.StorageGuard.DataDirectory != dataDir {
		t.Fatalf("live runtime storage guard data_directory is not configured for %q", dataDir)
	}

	var ran atomic.Bool
	stageName := supervisor.StageName("storage_guard_live_probe")
	stage := supervisor.Stage{
		Name:        stageName,
		Enabled:     true,
		Interval:    time.Hour,
		BatchSize:   1,
		Concurrency: 1,
		Runner: supervisor.RunnerFunc(func(context.Context) error {
			ran.Store(true)
			return nil
		}),
	}

	before := liveStageRunCount(t, store, string(stageName))
	gate := newIndexerStageResourceGuard(
		store,
		pgindex.DatabaseStorageGuardConfig{Enabled: false},
		IndexerMemoryGuardConfig{},
		settings,
		&config.Config{},
	)
	svc := supervisor.New(nil, []supervisor.Stage{stage}, supervisor.Options{
		Tracker:   store,
		Owner:     "storage-guard-live-test",
		StageGate: gate,
	})
	if err := svc.RunStageOnce(context.Background(), stageName); err != nil {
		t.Fatalf("run live probe stage: %v", err)
	}
	after := liveStageRunCount(t, store, string(stageName))
	if ran.Load() {
		t.Fatalf("expected storage guard to block probe runner")
	}
	if after != before {
		t.Fatalf("expected storage guard to block stage claim writes; before=%d after=%d", before, after)
	}
}

func liveStageRunCount(t *testing.T, store *pgindex.Store, stageName string) int64 {
	t.Helper()
	var count int64
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT COUNT(*)
		FROM indexer_stage_runs
		WHERE stage_name = $1`, stageName).Scan(&count); err != nil {
		t.Fatalf("count live stage runs: %v", err)
	}
	return count
}
