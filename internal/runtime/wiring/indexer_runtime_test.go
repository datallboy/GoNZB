package wiring

import (
	"context"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
)

type fakeSettingsStore struct {
	runtime *app.RuntimeSettings
}

func (f fakeSettingsStore) LoadEffectiveSettings(context.Context, *config.Config) (*config.Config, error) {
	return nil, nil
}

func (f fakeSettingsStore) GetRuntimeSettings(context.Context, ...*config.Config) (*app.RuntimeSettings, error) {
	return f.runtime, nil
}

func (f fakeSettingsStore) UpdateSettings(context.Context, *app.RuntimeSettings) error {
	return nil
}

func (f fakeSettingsStore) WatchSettingsChanges(context.Context) (<-chan struct{}, error) {
	return nil, nil
}

func (f fakeSettingsStore) Ping(context.Context) error {
	return nil
}

func (f fakeSettingsStore) SchemaVersion(context.Context) (int, error) {
	return 0, nil
}

func (f fakeSettingsStore) ExpectedSchemaVersion() int {
	return 0
}

func (f fakeSettingsStore) ValidateSchema(context.Context) error {
	return nil
}

func TestDeriveUsenetIndexerConfigUsesExpandedRuntimeSettings(t *testing.T) {
	enabled := true
	interval := 1.5
	batch := 64
	concurrency := 2
	backoff := 9
	matchHigh := 0.9
	matchProbable := 0.7
	articleBucket := int64(12000)
	predbTimeout := 22
	tmdbTimeout := 33

	cfg := &config.Config{
		Servers: []config.ServerConfig{{
			ID:            "primary",
			Host:          "news.example.com",
			Port:          563,
			Username:      "user",
			Password:      "pass",
			TLS:           true,
			MaxConnection: 10,
			Priority:      1,
		}},
		Store: config.StoreConfig{
			PGDSN: "postgres://postgres:postgres@localhost:5432/gonzb?sslmode=disable",
		},
		Modules: config.ModulesConfig{
			UsenetIndexer: config.ModuleToggle{Enabled: true},
		},
		Indexing: config.IndexingConfig{
			Newsgroups: []string{"alt.binaries.test"},
		},
	}
	cfg.Indexing.ScrapeLatest = config.IndexingStageConfig{
		Enabled:         &enabled,
		IntervalMinutes: &interval,
		BatchSize:       &batch,
		BackoffSeconds:  &backoff,
	}
	cfg.Indexing.Assemble = config.IndexingStageConfig{
		Concurrency: &concurrency,
	}
	cfg.Indexing.Match = config.IndexingMatchConfig{
		HighConfidenceThreshold:     &matchHigh,
		ProbableConfidenceThreshold: &matchProbable,
		ArticleBucketSize:           &articleBucket,
	}
	cfg.Indexing.Release = config.IndexingReleaseConfig{
		Enabled:                    &enabled,
		IntervalMinutes:            &interval,
		BatchSize:                  &batch,
		BackoffSeconds:             &backoff,
		MinConfidence:              &matchHigh,
		MinCompletionPct:           func() *float64 { v := 25.0; return &v }(),
		MinExpectedFileCoveragePct: func() *float64 { v := 92.0; return &v }(),
		RequireExpectedFileCountForContextualObfuscated: func() *bool { v := true; return &v }(),
	}
	cfg.Indexing.InspectMedia = config.IndexingStageConfig{
		Enabled:     &enabled,
		BatchSize:   &batch,
		Concurrency: &concurrency,
	}
	cfg.Indexing.InspectPAR2 = config.IndexingStageConfig{
		Enabled:     &enabled,
		BatchSize:   &batch,
		Concurrency: &concurrency,
	}
	cfg.Indexing.EnrichPreDB = config.IndexingPreDBConfig{
		Enabled:            &enabled,
		IntervalMinutes:    &interval,
		BatchSize:          &batch,
		BackoffSeconds:     &backoff,
		Provider:           "club",
		BaseURL:            "https://predb.example/api",
		FeedURL:            "https://predb.example/rss",
		HTTPTimeoutSeconds: &predbTimeout,
	}
	cfg.Indexing.EnrichTMDB = config.IndexingTMDBConfig{
		Enabled:            &enabled,
		IntervalMinutes:    &interval,
		BatchSize:          &batch,
		BackoffSeconds:     &backoff,
		HTTPTimeoutSeconds: &tmdbTimeout,
		TMDBAPIKey:         "tmdb-key",
		TMDBAccessToken:    "tmdb-token",
	}
	cfg.Indexing.Inspect = config.IndexingInspectConfig{
		WorkDir:          "/tmp/inspect",
		WorkspaceBackend: "memory",
		MemoryWorkDir:    "/dev/shm/gonzb-inspect-test",
		FFProbePath:      "ffprobe",
		SevenZipPath:     "7z",
		UnrarPath:        "unrar",
		PAR2Path:         "par2",
		MaxBytes:         1024,
		MaxArchiveDepth:  2,
		ToolTimeoutSecs:  15,
	}

	got, err := deriveUsenetIndexerConfig(cfg)
	if err != nil {
		t.Fatalf("derive config: %v", err)
	}

	if got.ScrapeLatest.Interval != 90*time.Second || got.ScrapeLatest.BatchSize != batch {
		t.Fatalf("unexpected scrape_latest stage config: %+v", got.ScrapeLatest)
	}
	if got.ScrapeLatest.Backoff != 9*time.Second || got.Assemble.Concurrency != concurrency {
		t.Fatalf("unexpected scrape_latest backoff or assemble concurrency: scrape=%+v assemble=%+v", got.ScrapeLatest, got.Assemble)
	}
	if got.Match.ArticleBucketSize != articleBucket || got.Match.HighConfidenceThreshold != matchHigh {
		t.Fatalf("unexpected match config: %+v", got.Match)
	}
	if got.ReleaseMinConfidence != matchHigh || got.ReleaseMinCompletion != 25 || got.ReleaseMinExpectedFileCoveragePct != 92 || !got.RequireExpectedFileCountForContextualObfuscated {
		t.Fatalf("unexpected release thresholds: min_confidence=%v min_completion=%v min_expected_file_coverage_pct=%v require_expected=%v", got.ReleaseMinConfidence, got.ReleaseMinCompletion, got.ReleaseMinExpectedFileCoveragePct, got.RequireExpectedFileCountForContextualObfuscated)
	}
	if !got.ReleaseSummaryRefreshStage.Enabled || got.ReleaseSummaryRefreshStage.BatchSize != 10000 || got.ReleaseSummaryRefreshStage.Interval != 2*time.Minute {
		t.Fatalf("unexpected release summary refresh stage config: %+v", got.ReleaseSummaryRefreshStage)
	}
	if got.InspectMedia.BatchSize != batch {
		t.Fatalf("expected inspect_media batch size %d, got %+v", batch, got.InspectMedia)
	}
	if got.InspectMedia.Concurrency != concurrency {
		t.Fatalf("expected inspect_media concurrency %d, got %+v", concurrency, got.InspectMedia)
	}
	if got.InspectPAR2.BatchSize != batch || got.InspectPAR2.Concurrency != concurrency {
		t.Fatalf("expected inspect_par2 batch/concurrency from config, got %+v", got.InspectPAR2)
	}
	if got.Inspect.WorkspaceBackend != "memory" || got.Inspect.MemoryWorkDir != "/dev/shm/gonzb-inspect-test" {
		t.Fatalf("expected inspect workspace backend settings, got %+v", got.Inspect)
	}
	if got.EnrichPreDB.Limit != batch || got.EnrichPreDB.HTTPTimeout != 22*time.Second {
		t.Fatalf("unexpected predb options: %+v", got.EnrichPreDB)
	}
	if got.EnrichTMDB.Limit != batch || got.EnrichTMDB.HTTPTimeout != 33*time.Second {
		t.Fatalf("unexpected tmdb options: %+v", got.EnrichTMDB)
	}
	if got.EnrichPreDBStage.Interval != 90*time.Second || !got.EnrichTMDBStage.Enabled {
		t.Fatalf("unexpected enrich stage config: predb=%+v tmdb=%+v", got.EnrichPreDBStage, got.EnrichTMDBStage)
	}
}

func TestScopedIndexerServersPrefersIndexerScopedRuntimeServers(t *testing.T) {
	appCtx := &app.Context{
		BootstrapConfig: &config.Config{},
		SettingsStore: fakeSettingsStore{
			runtime: &app.RuntimeSettings{
				Servers: []app.ServerRuntimeSettings{{
					ID:       "shared",
					Host:     "shared.example.com",
					Port:     563,
					Username: "shared-user",
					Password: "shared-pass",
					TLS:      true,
				}},
				IndexerServers: []app.ServerRuntimeSettings{{
					ID:       "indexer",
					Host:     "indexer.example.com",
					Port:     563,
					Username: "indexer-user",
					Password: "indexer-pass",
					TLS:      true,
				}},
			},
		},
	}

	servers := scopedIndexerServers(appCtx)
	if len(servers) != 1 {
		t.Fatalf("expected one indexer-scoped server, got %+v", servers)
	}
	if servers[0].ID != "indexer" || servers[0].Host != "indexer.example.com" || servers[0].Username != "indexer-user" {
		t.Fatalf("expected indexer-scoped server selection, got %+v", servers[0])
	}
}

func TestScopedDownloaderServersPrefersDownloaderScopedRuntimeServers(t *testing.T) {
	appCtx := &app.Context{
		BootstrapConfig: &config.Config{},
		SettingsStore: fakeSettingsStore{
			runtime: &app.RuntimeSettings{
				Servers: []app.ServerRuntimeSettings{{
					ID:       "shared",
					Host:     "shared.example.com",
					Port:     563,
					Username: "shared-user",
					Password: "shared-pass",
					TLS:      true,
				}},
				DownloaderServers: []app.ServerRuntimeSettings{{
					ID:       "downloader",
					Host:     "downloader.example.com",
					Port:     563,
					Username: "downloader-user",
					Password: "downloader-pass",
					TLS:      true,
				}},
			},
		},
	}

	servers := scopedDownloaderServers(appCtx)
	if len(servers) != 1 {
		t.Fatalf("expected one downloader-scoped server, got %+v", servers)
	}
	if servers[0].ID != "downloader" || servers[0].Host != "downloader.example.com" || servers[0].Username != "downloader-user" {
		t.Fatalf("expected downloader-scoped server selection, got %+v", servers[0])
	}
}
