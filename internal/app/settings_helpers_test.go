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
	backoff := 17
	high := 0.91
	probable := 0.63
	bucket := int64(9000)
	httpTimeout := 21

	runtime := IndexingRuntimeFromConfig(config.IndexingConfig{
		Newsgroups: []string{"alt.binaries.test"},
		ScrapeBackfill: config.IndexingStageConfig{
			BatchSize:   func() *int { v := 5000; return &v }(),
			Concurrency: func() *int { v := 12; return &v }(),
		},
		Assemble: config.IndexingStageConfig{
			BatchSize: func() *int { v := 5000; return &v }(),
		},
		Release: config.IndexingReleaseConfig{
			IntervalMinutes:            func() *float64 { v := 10.0; return &v }(),
			MinConfidence:              &high,
			MinCompletionPct:           func() *float64 { v := 34.0; return &v }(),
			MinExpectedFileCoveragePct: func() *float64 { v := 88.0; return &v }(),
			RequireExpectedFileCountForContextualObfuscated: func() *bool { v := false; return &v }(),
			PublicRequirePayloadComplete:                    func() *bool { v := true; return &v }(),
			PublicRequireExpectedFileCountComplete:          func() *bool { v := true; return &v }(),
			RetainUntilExpectedFileCountComplete:            func() *bool { v := true; return &v }(),
			ReopenArchivedNZBOnReleaseChange:                func() *bool { v := true; return &v }(),
		},
		Inspect: config.IndexingInspectConfig{
			WorkDir:                  "/tmp/inspect",
			WorkspaceBackend:         "memory",
			MemoryWorkDir:            "/dev/shm/custom-inspect",
			MaxBytes:                 1024,
			MaxArchiveDepth:          5,
			ToolTimeoutSecs:          45,
			RequireExpectedFileCount: true,
			FFProbePath:              "/usr/bin/ffprobe",
			SevenZipPath:             "/usr/bin/7z",
			UnrarPath:                "/usr/bin/unrar",
			PAR2Path:                 "/usr/bin/par2",
		},
		ScrapeLatest: config.IndexingStageConfig{
			Enabled:         &disabled,
			IntervalMinutes: &interval,
			BatchSize:       &batch,
			Concurrency:     func() *int { v := 8; return &v }(),
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
	if runtime.Release.MinConfidence != high || runtime.Release.MinCompletionPct != 34 || runtime.Release.MinExpectedFileCoveragePct != 88 || runtime.Release.RequireExpectedFileCountForContextualObfuscated {
		t.Fatalf("unexpected release config: %+v", runtime.Release)
	}
	if !runtime.Release.PublicRequirePayloadComplete || !runtime.Release.PublicRequireExpectedFileCountComplete || !runtime.Release.RetainUntilExpectedFileCountComplete || !runtime.Release.ReopenArchivedNZBOnReleaseChange {
		t.Fatalf("expected release readiness toggles to be mirrored, got %+v", runtime.Release)
	}
	if runtime.ScrapeLatest.IntervalMinutes != interval || runtime.ScrapeLatest.BatchSize != batch {
		t.Fatalf("unexpected scrape_latest config: %+v", runtime.ScrapeLatest)
	}
	if runtime.ScrapeLatest.Concurrency != 8 || runtime.ScrapeBackfill.Concurrency != 12 {
		t.Fatalf("expected scrape concurrency to be mirrored, got latest=%+v backfill=%+v", runtime.ScrapeLatest, runtime.ScrapeBackfill)
	}
	if runtime.Match.HighConfidenceThreshold != high || runtime.Match.ArticleBucketSize != bucket {
		t.Fatalf("unexpected match config: %+v", runtime.Match)
	}
	if runtime.Inspect.WorkDir != "/tmp/inspect" {
		t.Fatalf("expected inspect work dir to be mirrored, got %+v", runtime.Inspect)
	}
	if runtime.Inspect.WorkspaceBackend != "memory" || runtime.Inspect.MemoryWorkDir != "/dev/shm/custom-inspect" {
		t.Fatalf("expected inspect workspace settings to be mirrored, got %+v", runtime.Inspect)
	}
	if !runtime.Inspect.RequireExpectedFileCount {
		t.Fatalf("expected inspect expected-file gate to be mirrored, got %+v", runtime.Inspect)
	}
	if runtime.EnrichPreDB.Provider != "club" || runtime.EnrichPreDB.HTTPTimeoutSeconds != httpTimeout {
		t.Fatalf("unexpected predb config: %+v", runtime.EnrichPreDB)
	}
	if runtime.EnrichTMDB.TMDBAPIKey != "tmdb-key" || runtime.EnrichTMDB.TVDBPIN != "1234" {
		t.Fatalf("expected enrichment secrets to be mirrored, got %+v", runtime.EnrichTMDB)
	}
}

func TestDefaultRuntimeSettingsAreOperationallyDisabled(t *testing.T) {
	runtime := DefaultRuntimeSettings()

	if len(runtime.Servers) != 0 || len(runtime.Indexers) != 0 {
		t.Fatalf("expected empty servers and external indexers, got %+v", runtime)
	}
	if runtime.Aggregator == nil || runtime.Aggregator.Sources.LocalBlob.Enabled || runtime.Aggregator.Sources.UsenetIndexer.Enabled {
		t.Fatalf("expected disabled aggregator sources, got %+v", runtime.Aggregator)
	}
	if runtime.Indexing == nil || runtime.Indexing.ScrapeLatest.Enabled || runtime.Indexing.Release.Enabled || runtime.Indexing.EnrichTMDB.Enabled {
		t.Fatalf("expected disabled indexer stages, got %+v", runtime.Indexing)
	}
	if runtime.Indexing.Inspect.WorkspaceBackend != "auto" || runtime.Indexing.Inspect.MemoryWorkDir != "/dev/shm/gonzb-inspect" {
		t.Fatalf("expected auto inspect workspace defaults, got %+v", runtime.Indexing.Inspect)
	}
	if runtime.Indexing.InspectPAR2.Concurrency != 4 {
		t.Fatalf("expected inspect_par2 concurrency default, got %+v", runtime.Indexing.InspectPAR2)
	}
}

func TestWithRuntimeDefaultsBackfillsAssembleLaneStageDefaults(t *testing.T) {
	runtime := WithRuntimeDefaults(&RuntimeSettings{
		Indexing: &IndexingRuntimeSettings{
			Assemble: IndexingStageRuntimeSettings{Enabled: true, IntervalMinutes: 5, BatchSize: 4000, Concurrency: 2},
		},
	})

	if runtime.Indexing == nil {
		t.Fatalf("expected indexing settings")
	}
	if runtime.Indexing.AssembleLaneA.IntervalMinutes <= 0 || runtime.Indexing.AssembleLaneA.BatchSize <= 0 {
		t.Fatalf("expected lane A defaults to be backfilled, got %+v", runtime.Indexing.AssembleLaneA)
	}
	if runtime.Indexing.AssembleLaneB.IntervalMinutes <= 0 || runtime.Indexing.AssembleLaneB.BatchSize <= 0 {
		t.Fatalf("expected lane B defaults to be backfilled, got %+v", runtime.Indexing.AssembleLaneB)
	}
	if runtime.Indexing.Assemble.BinaryUpsertDBChunkSize != 250 {
		t.Fatalf("expected assemble chunk-size default to be backfilled, got %+v", runtime.Indexing.Assemble)
	}
	if runtime.Indexing.AssembleLaneA.BinaryUpsertDBChunkSize != 250 || runtime.Indexing.AssembleLaneB.BinaryUpsertDBChunkSize != 250 {
		t.Fatalf("expected lane chunk-size defaults to be backfilled, got laneA=%+v laneB=%+v", runtime.Indexing.AssembleLaneA, runtime.Indexing.AssembleLaneB)
	}
	if !runtime.Indexing.StorageGuard.Enabled || runtime.Indexing.StorageGuard.MinFreeBytes <= 0 || runtime.Indexing.StorageGuard.MinFreePercent <= 0 {
		t.Fatalf("expected storage guard defaults to be backfilled, got %+v", runtime.Indexing.StorageGuard)
	}
}

func TestToStageConfigOmitsUnsetBinaryUpsertChunkSize(t *testing.T) {
	cfg := toStageConfig(IndexingStageRuntimeSettings{
		Enabled:         true,
		IntervalMinutes: 10,
		BatchSize:       100,
		BackoffSeconds:  0,
	})

	if cfg.BinaryUpsertDBChunkSize != nil {
		t.Fatalf("expected unset binary upsert chunk size to remain nil, got %v", *cfg.BinaryUpsertDBChunkSize)
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

func TestCloneIndexingPreservesExplicitlyEmptyScrapeGroups(t *testing.T) {
	indexing := &IndexingRuntimeSettings{
		Newsgroups:               []string{"alt.binaries.test"},
		BackfillUntilDateByGroup: map[string]string{"alt.binaries.test": "2026-06-01"},
		ExplicitGroups:           []IndexingScrapeGroupRuntimeSettings{},
		WildcardRules:            []IndexingWildcardRuleRuntimeSettings{},
		ProviderGroupInventory:   []IndexingProviderGroupInventoryRuntimeSettings{},
		MaterializedGroups:       []IndexingMaterializedGroupRuntimeSettings{},
	}

	cloned := cloneIndexing(indexing)
	if cloned == nil {
		t.Fatal("expected cloned indexing settings")
	}
	if cloned.ExplicitGroups == nil {
		t.Fatal("expected explicit groups slice to remain explicitly empty, not nil")
	}
	if len(cloned.ExplicitGroups) != 0 {
		t.Fatalf("expected no explicit groups after clone, got %+v", cloned.ExplicitGroups)
	}
	if len(cloned.Newsgroups) != 0 {
		t.Fatalf("expected derived newsgroups to stay empty, got %+v", cloned.Newsgroups)
	}
	if len(cloned.BackfillUntilDateByGroup) != 0 {
		t.Fatalf("expected derived backfill map to stay empty, got %+v", cloned.BackfillUntilDateByGroup)
	}
}

func TestApplyPatchPreservesScrapeConcurrency(t *testing.T) {
	current := DefaultRuntimeSettings()
	current.Indexing.ScrapeLatest.Concurrency = 8
	current.Indexing.ScrapeBackfill.Concurrency = 12

	next := ApplyPatch(current, &RuntimeSettingsPatch{
		Indexing: &IndexingRuntimeSettings{
			ScrapeLatest: IndexingStageRuntimeSettings{
				Enabled:         true,
				IntervalMinutes: 5,
				BatchSize:       5000,
				MaxBatches:      8,
				Concurrency:     8,
			},
			ScrapeBackfill: IndexingStageRuntimeSettings{
				Enabled:         true,
				IntervalMinutes: 5,
				BatchSize:       10000,
				MaxBatches:      16,
				Concurrency:     12,
			},
		},
	})

	if next.Indexing.ScrapeLatest.Concurrency != 8 {
		t.Fatalf("expected scrape_latest concurrency to persist, got %+v", next.Indexing.ScrapeLatest)
	}
	if next.Indexing.ScrapeBackfill.Concurrency != 12 {
		t.Fatalf("expected scrape_backfill concurrency to persist, got %+v", next.Indexing.ScrapeBackfill)
	}
}

func TestApplyToConfigPreservesScrapeConcurrency(t *testing.T) {
	base := &config.Config{}
	base.Indexing = config.IndexingConfig{}

	runtime := DefaultRuntimeSettings()
	runtime.Indexing.ScrapeLatest = IndexingStageRuntimeSettings{
		Enabled:         true,
		IntervalMinutes: 5,
		BatchSize:       5000,
		MaxBatches:      8,
		Concurrency:     8,
	}
	runtime.Indexing.ScrapeBackfill = IndexingStageRuntimeSettings{
		Enabled:         true,
		IntervalMinutes: 5,
		BatchSize:       10000,
		MaxBatches:      16,
		Concurrency:     12,
	}

	effective := ApplyToConfig(base, runtime)
	if effective.Indexing.ScrapeLatest.Concurrency == nil || *effective.Indexing.ScrapeLatest.Concurrency != 8 {
		t.Fatalf("expected scrape_latest concurrency 8 in effective config, got %+v", effective.Indexing.ScrapeLatest)
	}
	if effective.Indexing.ScrapeBackfill.Concurrency == nil || *effective.Indexing.ScrapeBackfill.Concurrency != 12 {
		t.Fatalf("expected scrape_backfill concurrency 12 in effective config, got %+v", effective.Indexing.ScrapeBackfill)
	}
}

func TestApplyToConfigPreservesInspectExpectedFileGate(t *testing.T) {
	base := &config.Config{}
	base.Indexing = config.IndexingConfig{}

	runtime := DefaultRuntimeSettings()
	runtime.Indexing.Inspect.RequireExpectedFileCount = true

	effective := ApplyToConfig(base, runtime)
	if !effective.Indexing.Inspect.RequireExpectedFileCount {
		t.Fatalf("expected inspect.require_expected_file_count in effective config, got %+v", effective.Indexing.Inspect)
	}
}

func TestApplyToConfigPreservesReleaseReadinessPolicies(t *testing.T) {
	base := &config.Config{}
	base.Indexing = config.IndexingConfig{}

	runtime := DefaultRuntimeSettings()
	runtime.Indexing.Release.PublicRequirePayloadComplete = true
	runtime.Indexing.Release.PublicRequireExpectedFileCountComplete = true
	runtime.Indexing.Release.RetainUntilExpectedFileCountComplete = true
	runtime.Indexing.Release.ReopenArchivedNZBOnReleaseChange = true

	effective := ApplyToConfig(base, runtime)
	if effective.Indexing.Release.PublicRequirePayloadComplete == nil || !*effective.Indexing.Release.PublicRequirePayloadComplete {
		t.Fatalf("expected public payload-complete gate to persist, got %+v", effective.Indexing.Release)
	}
	if effective.Indexing.Release.RetainUntilExpectedFileCountComplete == nil || !*effective.Indexing.Release.RetainUntilExpectedFileCountComplete {
		t.Fatalf("expected retain-until-expected gate to persist, got %+v", effective.Indexing.Release)
	}
	if effective.Indexing.Release.ReopenArchivedNZBOnReleaseChange == nil || !*effective.Indexing.Release.ReopenArchivedNZBOnReleaseChange {
		t.Fatalf("expected reopen-archived toggle to persist, got %+v", effective.Indexing.Release)
	}
}

func TestRedactedCopyRemovesNestedIndexerSecrets(t *testing.T) {
	redacted := RedactedCopy(&RuntimeSettings{
		Indexing: &IndexingRuntimeSettings{
			EnrichTMDB: IndexingTMDBRuntimeSettings{
				TMDBAPIKey:      "nested-tmdb",
				TMDBAccessToken: "nested-token",
				TVDBAPIKey:      "nested-tvdb",
				TVDBPIN:         "nested-pin",
			},
		},
	})

	if redacted.Indexing.EnrichTMDB.TVDBAPIKey != "" || redacted.Indexing.EnrichTMDB.TVDBPIN != "" {
		t.Fatalf("expected nested TVDB secrets to be redacted, got %+v", redacted.Indexing.EnrichTMDB)
	}
}

func TestFromConfigMirrorsServerConnectionTuning(t *testing.T) {
	cfg := &config.Config{
		Servers: []config.ServerConfig{{
			ID:                     "primary",
			Host:                   "news.example.com",
			Port:                   563,
			MaxConnection:          20,
			Priority:               1,
			DialTimeoutSeconds:     11,
			TCPKeepAliveSeconds:    31,
			PoolIdleTimeoutSeconds: 46,
			PoolMaxAgeSeconds:      601,
			EnablePoolLogging:      true,
		}},
	}

	runtime := FromConfig(cfg)
	if len(runtime.Servers) != 1 {
		t.Fatalf("expected one server, got %d", len(runtime.Servers))
	}
	got := runtime.Servers[0]
	if got.DialTimeoutSeconds != 11 || got.TCPKeepAliveSeconds != 31 || got.PoolIdleTimeoutSeconds != 46 || got.PoolMaxAgeSeconds != 601 || !got.EnablePoolLogging {
		t.Fatalf("expected server tuning fields to round-trip, got %+v", got)
	}
}
