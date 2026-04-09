package app

import (
	"testing"

	"github.com/datallboy/gonzb/internal/infra/config"
)

func TestIndexingRuntimeFromConfigUsesExpandedSettings(t *testing.T) {
	enabled := true
	disabled := false
	interval := 2.5
	batch := 42
	concurrency := 3
	backoff := 17
	high := 0.91
	probable := 0.63
	bucket := int64(9000)
	httpTimeout := 21

	runtime := IndexingRuntimeFromConfig(config.IndexingConfig{
		Newsgroups:              []string{"alt.binaries.test"},
		ScrapeBatchSize:         5000,
		ScheduleIntervalMinutes: 10,
		ReleaseMinConfidence:    0.55,
		ReleaseMinCompletionPct: 12,
		Release: config.IndexingReleaseConfig{
			MinConfidence:    &high,
			MinCompletionPct: func() *float64 { v := 34.0; return &v }(),
		},
		Inspect: config.IndexingInspectConfig{
			WorkDir:         "/tmp/inspect",
			MaxBytes:        1024,
			MaxArchiveDepth: 5,
			ToolTimeoutSecs: 45,
			FFProbePath:     "/usr/bin/ffprobe",
			SevenZipPath:    "/usr/bin/7z",
			UnrarPath:       "/usr/bin/unrar",
			PAR2Path:        "/usr/bin/par2",
		},
		ScrapeLatest: config.IndexingStageConfig{
			Enabled:         &disabled,
			IntervalMinutes: &interval,
			BatchSize:       &batch,
			Concurrency:     &concurrency,
			BackoffSeconds:  &backoff,
		},
		Match: config.IndexingMatchConfig{
			HighConfidenceThreshold:     &high,
			ProbableConfidenceThreshold: &probable,
			ArticleBucketSize:           &bucket,
		},
		InspectPAR2: config.IndexingStageConfig{
			Enabled: &enabled,
		},
		EnrichPreDB: config.IndexingPreDBConfig{
			Enabled:            &enabled,
			IntervalMinutes:    &interval,
			BatchSize:          &batch,
			Concurrency:        &concurrency,
			BackoffSeconds:     &backoff,
			Provider:           "club",
			BaseURL:            "https://predb.example/api",
			FeedURL:            "https://predb.example/rss",
			DumpURL:            "https://predb.example/dump",
			HTTPTimeoutSeconds: &httpTimeout,
		},
		EnrichTMDB: config.IndexingTMDBConfig{
			Enabled:         &enabled,
			IntervalMinutes: &interval,
			BatchSize:       &batch,
			TMDBAPIKey:      "tmdb-key",
			TMDBAccessToken: "tmdb-token",
			TVDBAPIKey:      "tvdb-key",
			TVDBPIN:         "1234",
		},
	})

	if runtime.ScrapeLatest.Enabled {
		t.Fatalf("expected scrape_latest to be disabled")
	}
	if runtime.Release.MinConfidence != high || runtime.Release.MinCompletionPct != 34 {
		t.Fatalf("unexpected release config: %+v", runtime.Release)
	}
	if runtime.ScrapeLatest.IntervalMinutes != interval || runtime.ScrapeLatest.BatchSize != batch {
		t.Fatalf("unexpected scrape_latest config: %+v", runtime.ScrapeLatest)
	}
	if runtime.Match.HighConfidenceThreshold != high || runtime.Match.ArticleBucketSize != bucket {
		t.Fatalf("unexpected match config: %+v", runtime.Match)
	}
	if runtime.Inspect.WorkDir != "/tmp/inspect" || runtime.InspectWorkDir != "/tmp/inspect" {
		t.Fatalf("expected inspect work dir to be mirrored, got %+v", runtime.Inspect)
	}
	if runtime.EnrichPreDB.Provider != "club" || runtime.EnrichPreDB.HTTPTimeoutSeconds != httpTimeout {
		t.Fatalf("unexpected predb config: %+v", runtime.EnrichPreDB)
	}
	if runtime.TMDBAPIKey != "tmdb-key" || runtime.EnrichTMDB.TVDBPIN != "1234" {
		t.Fatalf("expected enrichment secrets to be mirrored, got %+v", runtime.EnrichTMDB)
	}
}

func TestApplyPatchPreservesExistingArrIntegrations(t *testing.T) {
	current := &RuntimeSettings{
		ArrIntegrations: []ArrIntegrationRuntimeSettings{{
			ID:      "sonarr",
			Kind:    "sonarr",
			Enabled: true,
		}},
	}

	next := ApplyPatch(current, &RuntimeSettingsPatch{
		Download: &DownloadRuntimeSettings{OutDir: "/downloads"},
	})

	if len(next.ArrIntegrations) != 1 || next.ArrIntegrations[0].ID != "sonarr" {
		t.Fatalf("expected arr integrations to be preserved, got %+v", next.ArrIntegrations)
	}
}

func TestRedactedCopyRemovesNestedIndexerSecrets(t *testing.T) {
	redacted := RedactedCopy(&RuntimeSettings{
		Indexing: &IndexingRuntimeSettings{
			TMDBAPIKey:      "top-level-tmdb",
			TMDBAccessToken: "top-level-token",
			TVDBAPIKey:      "top-level-tvdb",
			TVDBPIN:         "top-level-pin",
			EnrichTMDB: IndexingTMDBRuntimeSettings{
				TMDBAPIKey:      "nested-tmdb",
				TMDBAccessToken: "nested-token",
				TVDBAPIKey:      "nested-tvdb",
				TVDBPIN:         "nested-pin",
			},
		},
	})

	if redacted.Indexing.TMDBAPIKey != "" || redacted.Indexing.TMDBAccessToken != "" {
		t.Fatalf("expected top-level TMDB secrets to be redacted, got %+v", redacted.Indexing)
	}
	if redacted.Indexing.EnrichTMDB.TVDBAPIKey != "" || redacted.Indexing.EnrichTMDB.TVDBPIN != "" {
		t.Fatalf("expected nested TVDB secrets to be redacted, got %+v", redacted.Indexing.EnrichTMDB)
	}
}
