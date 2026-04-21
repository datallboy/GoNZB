package wiring

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing"
	"github.com/datallboy/gonzb/internal/indexing/assemble"
	"github.com/datallboy/gonzb/internal/indexing/enrich/predb"
	"github.com/datallboy/gonzb/internal/indexing/enrich/tmdb"
	inspectpkg "github.com/datallboy/gonzb/internal/indexing/inspect"
	"github.com/datallboy/gonzb/internal/indexing/inspect/archive"
	"github.com/datallboy/gonzb/internal/indexing/inspect/discovery"
	"github.com/datallboy/gonzb/internal/indexing/inspect/media"
	"github.com/datallboy/gonzb/internal/indexing/inspect/nfo"
	"github.com/datallboy/gonzb/internal/indexing/inspect/par2"
	"github.com/datallboy/gonzb/internal/indexing/inspect/password"
	"github.com/datallboy/gonzb/internal/indexing/maintenance"
	"github.com/datallboy/gonzb/internal/indexing/match"
	"github.com/datallboy/gonzb/internal/indexing/release"
	"github.com/datallboy/gonzb/internal/indexing/scrape"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/segmentio/ksuid"
)

type usenetIndexerRuntime struct {
	service        app.UsenetIndexerService
	supervisor     *supervisor.Supervisor
	scrapeProvider io.Closer
}

type usenetIndexerConfig struct {
	Newsgroups                                      []string
	BackfillUntilDateByGroup                        map[string]time.Time
	ScrapeServer                                    *config.ServerConfig
	ReleaseMinConfidence                            float64
	ReleaseMinCompletion                            float64
	RequireExpectedFileCountForContextualObfuscated bool
	Match                                           match.Options
	Inspect                                         inspectpkg.Options
	EnrichPreDB                                     predb.Options
	EnrichTMDB                                      tmdb.Options
	ScrapeLatest                                    indexerStageConfig
	ScrapeBackfill                                  indexerStageConfig
	Assemble                                        indexerStageConfig
	ReleaseStage                                    indexerStageConfig
	InspectDiscovery                                indexerStageConfig
	InspectPAR2                                     indexerStageConfig
	InspectNFO                                      indexerStageConfig
	InspectArchive                                  indexerStageConfig
	InspectPassword                                 indexerStageConfig
	InspectMedia                                    indexerStageConfig
	EnrichPreDBStage                                indexerStageConfig
	EnrichTMDBStage                                 indexerStageConfig
	MaintenanceStage                                indexerStageConfig
}

type indexerStageConfig struct {
	Enabled     bool
	Interval    time.Duration
	BatchSize   int
	Concurrency int
	Backoff     time.Duration
}

func buildUsenetIndexerRuntime(appCtx *app.Context, stageOwner string) (*usenetIndexerRuntime, error) {
	if appCtx == nil {
		return nil, fmt.Errorf("app context is required")
	}
	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		return &usenetIndexerRuntime{}, nil
	}
	if appCtx.PGIndexStore == nil {
		return nil, fmt.Errorf("usenet indexer is enabled but PGIndexStore is not initialized")
	}

	runtimeCfg, err := deriveUsenetIndexerConfig(appCtx.Config)
	if err != nil {
		return nil, err
	}

	var (
		scrapeLatestSvc   *scrape.Service
		scrapeBackfillSvc *scrape.Service
		scrapeProvider    io.Closer
		inspectFetcher    inspectpkg.ArticleFetcher
	)

	if len(runtimeCfg.Newsgroups) > 0 {
		if runtimeCfg.ScrapeServer == nil {
			return nil, fmt.Errorf("usenet indexer scrape runtime requires at least one NNTP server")
		}

		// Current default: the first configured NNTP server is used as the
		// scrape transport until per-module transport selection is introduced.
		provider := nntp.NewNNTPProvider(*runtimeCfg.ScrapeServer)
		if err := provider.TestConnection(); err != nil {
			return nil, fmt.Errorf("scrape provider initialization failed: %w", err)
		}

		scrapeAdapter := scrape.NewNNTPAdapter(provider)
		scrapeLatestSvc = scrape.NewService(
			appCtx.PGIndexStore,
			scrapeAdapter,
			appCtx.Logger,
			scrape.Options{
				Newsgroups:               runtimeCfg.Newsgroups,
				BatchSize:                int64(runtimeCfg.ScrapeLatest.BatchSize),
				BackfillUntilDateByGroup: runtimeCfg.BackfillUntilDateByGroup,
			},
		)
		scrapeBackfillSvc = scrape.NewService(
			appCtx.PGIndexStore,
			scrapeAdapter,
			appCtx.Logger,
			scrape.Options{
				Newsgroups:               runtimeCfg.Newsgroups,
				BatchSize:                int64(runtimeCfg.ScrapeBackfill.BatchSize),
				BackfillUntilDateByGroup: runtimeCfg.BackfillUntilDateByGroup,
			},
		)
		scrapeProvider = provider
		inspectFetcher = provider
	}

	matcherSvc := match.NewService(runtimeCfg.Match)
	assembleSvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		inspectFetcher,
		appCtx.Logger,
		assemble.Options{
			BatchSize: runtimeCfg.Assemble.BatchSize,
		},
	)

	releaseSvc := release.NewService(
		appCtx.PGIndexStore,
		appCtx.Logger,
		release.Options{
			BatchSize:            runtimeCfg.ReleaseStage.BatchSize,
			ReleaseMinConfidence: runtimeCfg.ReleaseMinConfidence,
			ReleaseMinCompletion: runtimeCfg.ReleaseMinCompletion,
			RequireExpectedFileCountForContextualObfuscated:    runtimeCfg.RequireExpectedFileCountForContextualObfuscated,
			RequireExpectedFileCountForContextualObfuscatedSet: true,
		},
	)
	workspaceManager := inspectpkg.NewWorkspaceManager(runtimeCfg.Inspect)
	commandRunner := inspectpkg.ExecCommandRunner{}
	inspectDiscoverySvc := discovery.NewService(appCtx.PGIndexStore, inspectFetcher, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectDiscovery.BatchSize))
	inspectPAR2Svc := par2.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectPAR2.BatchSize))
	inspectNFOSvc := nfo.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectNFO.BatchSize))
	inspectArchiveSvc := archive.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, commandRunner, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectArchive.BatchSize))
	inspectPasswordSvc := password.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, commandRunner, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectPassword.BatchSize))
	inspectMediaSvc := media.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, commandRunner, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectMedia.BatchSize))
	enrichPreDBSvc := predb.NewService(appCtx.PGIndexStore, appCtx.Logger, runtimeCfg.EnrichPreDB)
	enrichTMDBSvc := tmdb.NewService(appCtx.PGIndexStore, appCtx.Logger, runtimeCfg.EnrichTMDB)
	maintenanceSvc := maintenance.NewService(appCtx.PGIndexStore, appCtx.Logger)

	supervisorSvc := supervisor.New(appCtx.Logger, []supervisor.Stage{
		{
			Name:        supervisor.StageScrapeLatest,
			Interval:    runtimeCfg.ScrapeLatest.Interval,
			Enabled:     scrapeLatestSvc != nil && runtimeCfg.ScrapeLatest.Enabled,
			BatchSize:   runtimeCfg.ScrapeLatest.BatchSize,
			Concurrency: runtimeCfg.ScrapeLatest.Concurrency,
			Backoff:     runtimeCfg.ScrapeLatest.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return scrapeLatestSvc.RunLatestOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageScrapeBackfill,
			Interval:    runtimeCfg.ScrapeBackfill.Interval,
			Enabled:     scrapeBackfillSvc != nil && runtimeCfg.ScrapeBackfill.Enabled,
			BatchSize:   runtimeCfg.ScrapeBackfill.BatchSize,
			Concurrency: runtimeCfg.ScrapeBackfill.Concurrency,
			Backoff:     runtimeCfg.ScrapeBackfill.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return scrapeBackfillSvc.RunBackfillOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageAssemble,
			Interval:    runtimeCfg.Assemble.Interval,
			Enabled:     assembleSvc != nil && runtimeCfg.Assemble.Enabled,
			BatchSize:   runtimeCfg.Assemble.BatchSize,
			Concurrency: runtimeCfg.Assemble.Concurrency,
			Backoff:     runtimeCfg.Assemble.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return assembleSvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageRelease,
			Interval:    runtimeCfg.ReleaseStage.Interval,
			Enabled:     releaseSvc != nil && runtimeCfg.ReleaseStage.Enabled,
			BatchSize:   runtimeCfg.ReleaseStage.BatchSize,
			Concurrency: runtimeCfg.ReleaseStage.Concurrency,
			Backoff:     runtimeCfg.ReleaseStage.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return releaseSvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageInspectDiscovery,
			Interval:    runtimeCfg.InspectDiscovery.Interval,
			Enabled:     runtimeCfg.InspectDiscovery.Enabled,
			BatchSize:   runtimeCfg.InspectDiscovery.BatchSize,
			Concurrency: runtimeCfg.InspectDiscovery.Concurrency,
			Backoff:     runtimeCfg.InspectDiscovery.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectDiscoverySvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageInspectPAR2,
			Interval:    runtimeCfg.InspectPAR2.Interval,
			Enabled:     runtimeCfg.InspectPAR2.Enabled,
			BatchSize:   runtimeCfg.InspectPAR2.BatchSize,
			Concurrency: runtimeCfg.InspectPAR2.Concurrency,
			Backoff:     runtimeCfg.InspectPAR2.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectPAR2Svc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageInspectNFO,
			Interval:    runtimeCfg.InspectNFO.Interval,
			Enabled:     runtimeCfg.InspectNFO.Enabled,
			BatchSize:   runtimeCfg.InspectNFO.BatchSize,
			Concurrency: runtimeCfg.InspectNFO.Concurrency,
			Backoff:     runtimeCfg.InspectNFO.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectNFOSvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageInspectArchive,
			Interval:    runtimeCfg.InspectArchive.Interval,
			Enabled:     runtimeCfg.InspectArchive.Enabled,
			BatchSize:   runtimeCfg.InspectArchive.BatchSize,
			Concurrency: runtimeCfg.InspectArchive.Concurrency,
			Backoff:     runtimeCfg.InspectArchive.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectArchiveSvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageInspectPassword,
			Interval:    runtimeCfg.InspectPassword.Interval,
			Enabled:     runtimeCfg.InspectPassword.Enabled,
			BatchSize:   runtimeCfg.InspectPassword.BatchSize,
			Concurrency: runtimeCfg.InspectPassword.Concurrency,
			Backoff:     runtimeCfg.InspectPassword.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectPasswordSvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageInspectMedia,
			Interval:    runtimeCfg.InspectMedia.Interval,
			Enabled:     runtimeCfg.InspectMedia.Enabled,
			BatchSize:   runtimeCfg.InspectMedia.BatchSize,
			Concurrency: runtimeCfg.InspectMedia.Concurrency,
			Backoff:     runtimeCfg.InspectMedia.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectMediaSvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageEnrichPreDB,
			Interval:    runtimeCfg.EnrichPreDBStage.Interval,
			Enabled:     runtimeCfg.EnrichPreDBStage.Enabled,
			BatchSize:   runtimeCfg.EnrichPreDBStage.BatchSize,
			Concurrency: runtimeCfg.EnrichPreDBStage.Concurrency,
			Backoff:     runtimeCfg.EnrichPreDBStage.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return enrichPreDBSvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageEnrichTMDB,
			Interval:    runtimeCfg.EnrichTMDBStage.Interval,
			Enabled:     runtimeCfg.EnrichTMDBStage.Enabled,
			BatchSize:   runtimeCfg.EnrichTMDBStage.BatchSize,
			Concurrency: runtimeCfg.EnrichTMDBStage.Concurrency,
			Backoff:     runtimeCfg.EnrichTMDBStage.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return enrichTMDBSvc.RunOnce(ctx)
			}),
		},
		{
			Name:        supervisor.StageMaintenance,
			Interval:    runtimeCfg.MaintenanceStage.Interval,
			Enabled:     runtimeCfg.MaintenanceStage.Enabled,
			BatchSize:   runtimeCfg.MaintenanceStage.BatchSize,
			Concurrency: runtimeCfg.MaintenanceStage.Concurrency,
			Backoff:     runtimeCfg.MaintenanceStage.Backoff,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return maintenanceSvc.RunOnce(ctx)
			}),
		},
	}, supervisor.Options{
		Tracker: appCtx.PGIndexStore,
		Owner:   stageOwner,
	})

	service := indexing.NewService(supervisorSvc, indexing.Options{
		ReleaseReform:           releaseSvc.RunReformOnce,
		EnrichPredbSceneName:    enrichPreDBSvc.RunSceneNameRecoveryOnce,
		EnrichPredbMetadataOnly: enrichPreDBSvc.RunMetadataFallbackOnce,
		EnrichPredbSyncFeed:     enrichPreDBSvc.RunSyncFeedOnce,
		EnrichPredbSyncBackfill: enrichPreDBSvc.RunSyncBackfillOnce,
	})

	return &usenetIndexerRuntime{
		service:        service,
		supervisor:     supervisorSvc,
		scrapeProvider: scrapeProvider,
	}, nil
}

func newIndexerStageOwner() string {
	return ksuid.New().String()
}

func deriveUsenetIndexerConfig(cfg *config.Config) (usenetIndexerConfig, error) {
	if cfg == nil {
		return usenetIndexerConfig{}, fmt.Errorf("app config is required")
	}

	indexingCfg := app.IndexingRuntimeFromConfig(cfg.Indexing)
	backfillCutoffs := map[string]time.Time{}
	for group, rawDate := range cfg.Indexing.BackfillUntilDateByGroup {
		parsed, err := time.Parse("2006-01-02", rawDate)
		if err != nil {
			return usenetIndexerConfig{}, fmt.Errorf("parse indexing.backfill_until_date_by_group[%s]: %w", group, err)
		}
		backfillCutoffs[group] = parsed.UTC()
	}

	out := usenetIndexerConfig{
		Newsgroups:               append([]string(nil), indexingCfg.Newsgroups...),
		BackfillUntilDateByGroup: backfillCutoffs,
		ReleaseMinConfidence:     indexingCfg.Release.MinConfidence,
		ReleaseMinCompletion:     indexingCfg.Release.MinCompletionPct,
		RequireExpectedFileCountForContextualObfuscated: indexingCfg.Release.RequireExpectedFileCountForContextualObfuscated,
		Match: match.Options{
			HighConfidenceThreshold:     indexingCfg.Match.HighConfidenceThreshold,
			ProbableConfidenceThreshold: indexingCfg.Match.ProbableConfidenceThreshold,
			ArticleBucketSize:           indexingCfg.Match.ArticleBucketSize,
		},
		Inspect: inspectpkg.DefaultOptions(inspectpkg.Options{
			WorkDir:            indexingCfg.Inspect.WorkDir,
			MaxBytes:           indexingCfg.Inspect.MaxBytes,
			MaxArchiveDepth:    indexingCfg.Inspect.MaxArchiveDepth,
			ToolTimeout:        time.Duration(indexingCfg.Inspect.ToolTimeoutSecs) * time.Second,
			FFProbePath:        indexingCfg.Inspect.FFProbePath,
			SevenZipPath:       indexingCfg.Inspect.SevenZipPath,
			UnrarPath:          indexingCfg.Inspect.UnrarPath,
			PAR2Path:           indexingCfg.Inspect.PAR2Path,
			CandidateBatchSize: 100,
		}),
		EnrichTMDB: tmdb.DefaultOptions(tmdb.Options{
			Limit:           indexingCfg.EnrichTMDB.BatchSize,
			HTTPTimeout:     time.Duration(indexingCfg.EnrichTMDB.HTTPTimeoutSeconds) * time.Second,
			TMDBAPIKey:      indexingCfg.EnrichTMDB.TMDBAPIKey,
			TMDBAccessToken: indexingCfg.EnrichTMDB.TMDBAccessToken,
			TMDBBaseURL:     indexingCfg.EnrichTMDB.TMDBBaseURL,
			TVDBAPIKey:      indexingCfg.EnrichTMDB.TVDBAPIKey,
			TVDBPIN:         indexingCfg.EnrichTMDB.TVDBPIN,
			TVDBBaseURL:     indexingCfg.EnrichTMDB.TVDBBaseURL,
		}),
		EnrichPreDB: predb.DefaultOptions(predb.Options{
			Limit:            indexingCfg.EnrichPreDB.BatchSize,
			Provider:         indexingCfg.EnrichPreDB.Provider,
			BaseURL:          indexingCfg.EnrichPreDB.BaseURL,
			FeedURL:          indexingCfg.EnrichPreDB.FeedURL,
			DumpURL:          indexingCfg.EnrichPreDB.DumpURL,
			HTTPTimeout:      time.Duration(indexingCfg.EnrichPreDB.HTTPTimeoutSeconds) * time.Second,
			BackfillPageSize: indexingCfg.EnrichPreDB.BackfillPageSize,
			MaxBackfillPages: indexingCfg.EnrichPreDB.MaxBackfillPages,
		}),
		ScrapeLatest:   newIndexerStageConfig(indexingCfg.ScrapeLatest),
		ScrapeBackfill: newIndexerStageConfig(indexingCfg.ScrapeBackfill),
		Assemble:       newIndexerStageConfig(indexingCfg.Assemble),
		ReleaseStage: newIndexerStageConfig(app.IndexingStageRuntimeSettings{
			Enabled:         indexingCfg.Release.Enabled,
			IntervalMinutes: indexingCfg.Release.IntervalMinutes,
			BatchSize:       indexingCfg.Release.BatchSize,
			Concurrency:     indexingCfg.Release.Concurrency,
			BackoffSeconds:  indexingCfg.Release.BackoffSeconds,
		}),
		InspectDiscovery: newIndexerStageConfig(indexingCfg.InspectArchive),
		InspectPAR2:      newIndexerStageConfig(indexingCfg.InspectPAR2),
		InspectNFO:       newIndexerStageConfig(indexingCfg.InspectNFO),
		InspectArchive:   newIndexerStageConfig(indexingCfg.InspectArchive),
		InspectPassword:  newIndexerStageConfig(indexingCfg.InspectPassword),
		InspectMedia:     newIndexerStageConfig(indexingCfg.InspectMedia),
		EnrichPreDBStage: newIndexerStageConfig(IndexingStageRuntimeSettingsFromPredb(indexingCfg.EnrichPreDB)),
		EnrichTMDBStage:  newIndexerStageConfig(IndexingStageRuntimeSettingsFromTMDB(indexingCfg.EnrichTMDB)),
		MaintenanceStage: indexerStageConfig{
			Enabled:     true,
			Interval:    6 * time.Hour,
			BatchSize:   0,
			Concurrency: 1,
			Backoff:     0,
		},
	}

	if len(cfg.Servers) > 0 {
		server := cfg.Servers[0]
		out.ScrapeServer = &server
	}

	return out, nil
}

func newIndexerStageConfig(in app.IndexingStageRuntimeSettings) indexerStageConfig {
	return indexerStageConfig{
		Enabled:     in.Enabled,
		Interval:    time.Duration(in.IntervalMinutes * float64(time.Minute)),
		BatchSize:   in.BatchSize,
		Concurrency: in.Concurrency,
		Backoff:     time.Duration(in.BackoffSeconds) * time.Second,
	}
}

func withInspectBatch(in inspectpkg.Options, batchSize int) inspectpkg.Options {
	out := in
	if batchSize > 0 {
		out.CandidateBatchSize = batchSize
	}
	return out
}

func IndexingStageRuntimeSettingsFromPredb(in app.IndexingPreDBRuntimeSettings) app.IndexingStageRuntimeSettings {
	return app.IndexingStageRuntimeSettings{
		Enabled:         in.Enabled,
		IntervalMinutes: in.IntervalMinutes,
		BatchSize:       in.BatchSize,
		Concurrency:     in.Concurrency,
		BackoffSeconds:  in.BackoffSeconds,
	}
}

func IndexingStageRuntimeSettingsFromTMDB(in app.IndexingTMDBRuntimeSettings) app.IndexingStageRuntimeSettings {
	return app.IndexingStageRuntimeSettings{
		Enabled:         in.Enabled,
		IntervalMinutes: in.IntervalMinutes,
		BatchSize:       in.BatchSize,
		Concurrency:     in.Concurrency,
		BackoffSeconds:  in.BackoffSeconds,
	}
}
