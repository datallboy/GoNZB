package wiring

import (
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/infra/config"
)

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
			Newsgroups:              []string{"alt.binaries.test"},
			ScrapeBatchSize:         5000,
			ScheduleIntervalMinutes: 10,
			ReleaseMinConfidence:    0.55,
			ReleaseMinCompletionPct: 5,
		},
	}
	cfg.Indexing.ScrapeLatest = config.IndexingStageConfig{
		Enabled:         &enabled,
		IntervalMinutes: &interval,
		BatchSize:       &batch,
		Concurrency:     &concurrency,
		BackoffSeconds:  &backoff,
	}
	cfg.Indexing.Match = config.IndexingMatchConfig{
		HighConfidenceThreshold:     &matchHigh,
		ProbableConfidenceThreshold: &matchProbable,
		ArticleBucketSize:           &articleBucket,
	}
	cfg.Indexing.Release = config.IndexingReleaseConfig{
		Enabled:          &enabled,
		IntervalMinutes:  &interval,
		BatchSize:        &batch,
		Concurrency:      &concurrency,
		BackoffSeconds:   &backoff,
		MinConfidence:    &matchHigh,
		MinCompletionPct: func() *float64 { v := 25.0; return &v }(),
	}
	cfg.Indexing.InspectMedia = config.IndexingStageConfig{
		Enabled:   &enabled,
		BatchSize: &batch,
	}
	cfg.Indexing.EnrichPreDB = config.IndexingPreDBConfig{
		Enabled:            &enabled,
		IntervalMinutes:    &interval,
		BatchSize:          &batch,
		Concurrency:        &concurrency,
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
		Concurrency:        &concurrency,
		BackoffSeconds:     &backoff,
		HTTPTimeoutSeconds: &tmdbTimeout,
		TMDBAPIKey:         "tmdb-key",
		TMDBAccessToken:    "tmdb-token",
	}

	got, err := deriveUsenetIndexerConfig(cfg)
	if err != nil {
		t.Fatalf("derive config: %v", err)
	}

	if got.ScrapeLatest.Interval != 90*time.Second || got.ScrapeLatest.BatchSize != batch {
		t.Fatalf("unexpected scrape_latest stage config: %+v", got.ScrapeLatest)
	}
	if got.ScrapeLatest.Concurrency != concurrency || got.ScrapeLatest.Backoff != 9*time.Second {
		t.Fatalf("unexpected scrape_latest concurrency/backoff: %+v", got.ScrapeLatest)
	}
	if got.Match.ArticleBucketSize != articleBucket || got.Match.HighConfidenceThreshold != matchHigh {
		t.Fatalf("unexpected match config: %+v", got.Match)
	}
	if got.ReleaseMinConfidence != matchHigh || got.ReleaseMinCompletion != 25 {
		t.Fatalf("unexpected release thresholds: min_confidence=%v min_completion=%v", got.ReleaseMinConfidence, got.ReleaseMinCompletion)
	}
	if got.InspectMedia.BatchSize != batch {
		t.Fatalf("expected inspect_media batch size %d, got %+v", batch, got.InspectMedia)
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
