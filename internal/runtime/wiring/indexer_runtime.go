package wiring

import (
	"context"
	"encoding/json"
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
	"github.com/datallboy/gonzb/internal/indexing/yencrecover"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/store/pgindex"
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
	ReleaseMinExpectedFileCoveragePct               float64
	RequireExpectedFileCountForContextualObfuscated bool
	Match                                           match.Options
	Inspect                                         inspectpkg.Options
	EnrichPreDB                                     predb.Options
	EnrichTMDB                                      tmdb.Options
	ScrapeLatest                                    indexerStageConfig
	ScrapeBackfill                                  indexerStageConfig
	Assemble                                        indexerStageConfig
	AssembleLaneA                                   indexerStageConfig
	AssembleLaneB                                   indexerStageConfig
	RecoverYEnc                                     indexerStageConfig
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

	indexerConfig := appCtx.Config
	if servers := scopedIndexerServers(appCtx); len(servers) > 0 {
		cfg := *appCtx.Config
		cfg.Servers = servers
		indexerConfig = &cfg
	}
	runtimeCfg, err := deriveUsenetIndexerConfig(indexerConfig)
	if err != nil {
		return nil, err
	}

	var (
		scrapeLatestSvc         *scrape.Service
		scrapeBackfillSvc       *scrape.Service
		scrapeProvider          io.Closer
		nntpStats               func() app.NNTPRuntimeStats
		assembleFetcher         inspectpkg.ArticleFetcher
		assembleAFetcher        inspectpkg.ArticleFetcher
		assembleBFetcher        inspectpkg.ArticleFetcher
		inspectDiscoveryFetcher inspectpkg.ArticleFetcher
		inspectPAR2Fetcher      inspectpkg.ArticleFetcher
		inspectNFOFetcher       inspectpkg.ArticleFetcher
		inspectArchiveFetcher   inspectpkg.ArticleFetcher
		inspectPasswordFetcher  inspectpkg.ArticleFetcher
		inspectMediaFetcher     inspectpkg.ArticleFetcher
		recoverFetcher          interface {
			FetchBodyPrefix(ctx context.Context, msgID string, groups []string, maxBytes int64) ([]byte, error)
		}
	)

	if len(runtimeCfg.Newsgroups) > 0 {
		if runtimeCfg.ScrapeServer == nil {
			return nil, fmt.Errorf("usenet indexer scrape runtime requires at least one NNTP server")
		}

		managerConfig := *appCtx.Config
		managerConfig.Servers = []config.ServerConfig{*runtimeCfg.ScrapeServer}
		managerCtx := *appCtx
		managerCtx.Config = &managerConfig
		manager, err := nntp.NewManagerWithOptions(&managerCtx, nntp.ManagerOptions{
			CapacityPolicy: nntp.CapacityWaitQueue,
		})
		if err != nil {
			return nil, fmt.Errorf("scrape manager initialization failed: %w", err)
		}
		scrapeClient := manager.ClientForScope("scrape")
		assembleClient := manager.ClientForScope("assemble")
		assembleLaneAClient := manager.ClientForScope("assemble_lane_a")
		assembleLaneBClient := manager.ClientForScope("assemble_lane_b")
		recoverYEncClient := manager.ClientForScope("recover_yenc")

		scrapeAdapter := scrape.NewNNTPAdapter(scrapeClient)
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
		scrapeProvider = manager
		nntpStats = func() app.NNTPRuntimeStats {
			return manager.RuntimeStats("indexer")
		}
		assembleFetcher = assembleClient
		assembleAFetcher = assembleLaneAClient
		assembleBFetcher = assembleLaneBClient
		inspectDiscoveryFetcher = manager.ClientForScope("inspect_discovery")
		inspectPAR2Fetcher = manager.ClientForScope("inspect_par2")
		inspectNFOFetcher = manager.ClientForScope("inspect_nfo")
		inspectArchiveFetcher = manager.ClientForScope("inspect_archive")
		inspectPasswordFetcher = manager.ClientForScope("inspect_password")
		inspectMediaFetcher = manager.ClientForScope("inspect_media")
		recoverFetcher = recoverYEncClient
	}

	matcherSvc := match.NewService(runtimeCfg.Match)
	assembleSvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		assembleFetcher,
		appCtx.Logger,
		assemble.Options{
			BatchSize:   runtimeCfg.Assemble.BatchSize,
			ClaimOwner:  "assemble",
			ClaimLease:  5 * time.Minute,
			Concurrency: runtimeCfg.Assemble.Concurrency,
		},
	)
	assembleLaneASvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		assembleAFetcher,
		appCtx.Logger,
		assemble.Options{
			BatchSize:   runtimeCfg.AssembleLaneA.BatchSize,
			ClaimOwner:  "assemble-lane-a",
			ClaimLease:  5 * time.Minute,
			Concurrency: runtimeCfg.AssembleLaneA.Concurrency,
			Lane:        pgindex.AssemblyClaimLaneA,
		},
	)
	assembleLaneBSvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		assembleBFetcher,
		appCtx.Logger,
		assemble.Options{
			BatchSize:   runtimeCfg.AssembleLaneB.BatchSize,
			ClaimOwner:  "assemble-lane-b",
			ClaimLease:  5 * time.Minute,
			Concurrency: runtimeCfg.AssembleLaneB.Concurrency,
			Lane:        pgindex.AssemblyClaimLaneB,
		},
	)
	recoverYEncSvc := yencrecover.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		recoverFetcher,
		appCtx.Logger,
		yencrecover.Options{
			BatchSize:      runtimeCfg.RecoverYEnc.BatchSize,
			MaxHeaderBytes: 8192,
			FetchTimeout:   10 * time.Second,
			Concurrency:    runtimeCfg.RecoverYEnc.Concurrency,
		},
	)

	releaseSvc := release.NewService(
		appCtx.PGIndexStore,
		appCtx.Logger,
		release.Options{
			BatchSize:                         runtimeCfg.ReleaseStage.BatchSize,
			ReleaseMinConfidence:              runtimeCfg.ReleaseMinConfidence,
			ReleaseMinCompletion:              runtimeCfg.ReleaseMinCompletion,
			ReleaseMinExpectedFileCoveragePct: runtimeCfg.ReleaseMinExpectedFileCoveragePct,
			RequireExpectedFileCountForContextualObfuscated:    runtimeCfg.RequireExpectedFileCountForContextualObfuscated,
			RequireExpectedFileCountForContextualObfuscatedSet: true,
		},
	)
	workspaceManager := inspectpkg.NewWorkspaceManager(runtimeCfg.Inspect)
	commandRunner := inspectpkg.ExecCommandRunner{}
	inspectDiscoverySvc := discovery.NewService(appCtx.PGIndexStore, inspectDiscoveryFetcher, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectDiscovery.BatchSize))
	inspectPAR2Svc := par2.NewService(appCtx.PGIndexStore, workspaceManager, inspectPAR2Fetcher, appCtx.Logger, withInspectStage(runtimeCfg.Inspect, runtimeCfg.InspectPAR2, stageOwner))
	inspectNFOSvc := nfo.NewService(appCtx.PGIndexStore, workspaceManager, inspectNFOFetcher, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectNFO.BatchSize))
	inspectArchiveSvc := archive.NewService(appCtx.PGIndexStore, workspaceManager, inspectArchiveFetcher, commandRunner, appCtx.Logger, withInspectStage(runtimeCfg.Inspect, runtimeCfg.InspectArchive, stageOwner))
	inspectPasswordSvc := password.NewService(appCtx.PGIndexStore, workspaceManager, inspectPasswordFetcher, commandRunner, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectPassword.BatchSize))
	inspectMediaSvc := media.NewService(appCtx.PGIndexStore, workspaceManager, inspectMediaFetcher, commandRunner, appCtx.Logger, withInspectStage(runtimeCfg.Inspect, runtimeCfg.InspectMedia, stageOwner))
	enrichPreDBSvc := predb.NewService(appCtx.PGIndexStore, appCtx.Logger, runtimeCfg.EnrichPreDB)
	enrichTMDBSvc := tmdb.NewService(appCtx.PGIndexStore, appCtx.Logger, runtimeCfg.EnrichTMDB)
	maintenanceSvc := maintenance.NewService(appCtx.PGIndexStore, appCtx.Logger)

	supervisorSvc := supervisor.New(appCtx.Logger, []supervisor.Stage{
		{
			Name:        supervisor.StageScrapeLatest,
			Interval:    runtimeCfg.ScrapeLatest.Interval,
			Enabled:     scrapeLatestSvc != nil && runtimeCfg.ScrapeLatest.Enabled,
			BatchSize:   runtimeCfg.ScrapeLatest.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.ScrapeLatest.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(scrapeLatestSvc.RunLatestOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageScrapeBackfill,
			Interval:    runtimeCfg.ScrapeBackfill.Interval,
			Enabled:     scrapeBackfillSvc != nil && runtimeCfg.ScrapeBackfill.Enabled,
			BatchSize:   runtimeCfg.ScrapeBackfill.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.ScrapeBackfill.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(scrapeBackfillSvc.RunBackfillOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageAssemble,
			Interval:    runtimeCfg.Assemble.Interval,
			Enabled:     assembleSvc != nil && runtimeCfg.Assemble.Enabled,
			BatchSize:   runtimeCfg.Assemble.BatchSize,
			Concurrency: runtimeCfg.Assemble.Concurrency,
			Backoff:     runtimeCfg.Assemble.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(assembleSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageAssembleLaneA,
			Interval:    runtimeCfg.AssembleLaneA.Interval,
			Enabled:     assembleLaneASvc != nil && runtimeCfg.AssembleLaneA.Enabled,
			BatchSize:   runtimeCfg.AssembleLaneA.BatchSize,
			Concurrency: runtimeCfg.AssembleLaneA.Concurrency,
			Backoff:     runtimeCfg.AssembleLaneA.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(assembleLaneASvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageAssembleLaneB,
			Interval:    runtimeCfg.AssembleLaneB.Interval,
			Enabled:     assembleLaneBSvc != nil && runtimeCfg.AssembleLaneB.Enabled,
			BatchSize:   runtimeCfg.AssembleLaneB.BatchSize,
			Concurrency: runtimeCfg.AssembleLaneB.Concurrency,
			Backoff:     runtimeCfg.AssembleLaneB.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(assembleLaneBSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageRecoverYEnc,
			Interval:    runtimeCfg.RecoverYEnc.Interval,
			Enabled:     recoverYEncSvc != nil && recoverFetcher != nil && runtimeCfg.RecoverYEnc.Enabled,
			BatchSize:   runtimeCfg.RecoverYEnc.BatchSize,
			Concurrency: runtimeCfg.RecoverYEnc.Concurrency,
			Backoff:     runtimeCfg.RecoverYEnc.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(recoverYEncSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageRelease,
			Interval:    runtimeCfg.ReleaseStage.Interval,
			Enabled:     releaseSvc != nil && runtimeCfg.ReleaseStage.Enabled,
			BatchSize:   runtimeCfg.ReleaseStage.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.ReleaseStage.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(releaseSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageInspectDiscovery,
			Interval:    runtimeCfg.InspectDiscovery.Interval,
			Enabled:     runtimeCfg.InspectDiscovery.Enabled,
			BatchSize:   runtimeCfg.InspectDiscovery.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.InspectDiscovery.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(inspectDiscoverySvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageInspectPAR2,
			Interval:    runtimeCfg.InspectPAR2.Interval,
			Enabled:     runtimeCfg.InspectPAR2.Enabled,
			BatchSize:   runtimeCfg.InspectPAR2.BatchSize,
			Concurrency: runtimeCfg.InspectPAR2.Concurrency,
			Backoff:     runtimeCfg.InspectPAR2.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(inspectPAR2Svc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageInspectNFO,
			Interval:    runtimeCfg.InspectNFO.Interval,
			Enabled:     runtimeCfg.InspectNFO.Enabled,
			BatchSize:   runtimeCfg.InspectNFO.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.InspectNFO.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(inspectNFOSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageInspectArchive,
			Interval:    runtimeCfg.InspectArchive.Interval,
			Enabled:     runtimeCfg.InspectArchive.Enabled,
			BatchSize:   runtimeCfg.InspectArchive.BatchSize,
			Concurrency: runtimeCfg.InspectArchive.Concurrency,
			Backoff:     runtimeCfg.InspectArchive.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(inspectArchiveSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageInspectPassword,
			Interval:    runtimeCfg.InspectPassword.Interval,
			Enabled:     runtimeCfg.InspectPassword.Enabled,
			BatchSize:   runtimeCfg.InspectPassword.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.InspectPassword.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(inspectPasswordSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageInspectMedia,
			Interval:    runtimeCfg.InspectMedia.Interval,
			Enabled:     runtimeCfg.InspectMedia.Enabled,
			BatchSize:   runtimeCfg.InspectMedia.BatchSize,
			Concurrency: runtimeCfg.InspectMedia.Concurrency,
			Backoff:     runtimeCfg.InspectMedia.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(inspectMediaSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageEnrichPreDB,
			Interval:    runtimeCfg.EnrichPreDBStage.Interval,
			Enabled:     runtimeCfg.EnrichPreDBStage.Enabled,
			BatchSize:   runtimeCfg.EnrichPreDBStage.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.EnrichPreDBStage.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(enrichPreDBSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageEnrichTMDB,
			Interval:    runtimeCfg.EnrichTMDBStage.Interval,
			Enabled:     runtimeCfg.EnrichTMDBStage.Enabled,
			BatchSize:   runtimeCfg.EnrichTMDBStage.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.EnrichTMDBStage.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(enrichTMDBSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageMaintenance,
			Interval:    runtimeCfg.MaintenanceStage.Interval,
			Enabled:     runtimeCfg.MaintenanceStage.Enabled,
			BatchSize:   runtimeCfg.MaintenanceStage.BatchSize,
			Concurrency: runtimeCfg.MaintenanceStage.Concurrency,
			Backoff:     runtimeCfg.MaintenanceStage.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(maintenanceSvc.RunOnceWithMetrics(ctx))
			}),
		},
	}, supervisor.Options{
		Tracker: appCtx.PGIndexStore,
		Owner:   stageOwner,
	})

	service := indexing.NewService(supervisorSvc, indexing.Options{
		AssembleLaneA:           assembleLaneASvc.RunOnce,
		AssembleLaneB:           assembleLaneBSvc.RunOnce,
		RecoverYEnc:             recoverYEncSvc.RunOnce,
		ReleaseReform:           releaseSvc.RunReformOnce,
		EnrichPredbSceneName:    enrichPreDBSvc.RunSceneNameRecoveryOnce,
		EnrichPredbMetadataOnly: enrichPreDBSvc.RunMetadataFallbackOnce,
		EnrichPredbSyncFeed:     enrichPreDBSvc.RunSyncFeedOnce,
		EnrichPredbSyncBackfill: enrichPreDBSvc.RunSyncBackfillOnce,
		NNTPStats:               nntpStats,
	})

	return &usenetIndexerRuntime{
		service:        service,
		supervisor:     supervisorSvc,
		scrapeProvider: scrapeProvider,
	}, nil
}

func scopedIndexerServers(appCtx *app.Context) []config.ServerConfig {
	if appCtx == nil || appCtx.SettingsStore == nil {
		return nil
	}
	runtime, err := appCtx.SettingsStore.GetRuntimeSettings(context.Background(), appCtx.BootstrapConfig)
	if err != nil {
		appCtx.Logger.Warn("Failed to load indexer NNTP runtime settings: %v", err)
		return nil
	}
	return app.ToConfigServers(app.IndexerNNTPServers(runtime))
}

func marshalStageMetrics(metrics map[string]any, err error) (json.RawMessage, error) {
	if metrics == nil {
		metrics = map[string]any{}
	}
	payload, marshalErr := json.Marshal(metrics)
	if marshalErr != nil {
		if err != nil {
			return json.RawMessage(`{"metrics_error":"marshal_failed"}`), err
		}
		return nil, marshalErr
	}
	return payload, err
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
		Newsgroups:                                      append([]string(nil), indexingCfg.Newsgroups...),
		BackfillUntilDateByGroup:                        backfillCutoffs,
		ReleaseMinConfidence:                            indexingCfg.Release.MinConfidence,
		ReleaseMinCompletion:                            indexingCfg.Release.MinCompletionPct,
		ReleaseMinExpectedFileCoveragePct:               indexingCfg.Release.MinExpectedFileCoveragePct,
		RequireExpectedFileCountForContextualObfuscated: indexingCfg.Release.RequireExpectedFileCountForContextualObfuscated,
		Match: match.Options{
			HighConfidenceThreshold:     indexingCfg.Match.HighConfidenceThreshold,
			ProbableConfidenceThreshold: indexingCfg.Match.ProbableConfidenceThreshold,
			ArticleBucketSize:           indexingCfg.Match.ArticleBucketSize,
		},
		Inspect: inspectpkg.DefaultOptions(inspectpkg.Options{
			WorkDir:            indexingCfg.Inspect.WorkDir,
			WorkspaceBackend:   indexingCfg.Inspect.WorkspaceBackend,
			MemoryWorkDir:      indexingCfg.Inspect.MemoryWorkDir,
			MaxBytes:           indexingCfg.Inspect.MaxBytes,
			MinBinaryBytes:     indexingCfg.Inspect.MinBinaryBytes,
			MaxBinaryBytes:     indexingCfg.Inspect.MaxBinaryBytes,
			BlockedMagicHex:    append([]string(nil), indexingCfg.Inspect.BlockedMagicHex...),
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
		AssembleLaneA:  newIndexerStageConfig(indexingCfg.AssembleLaneA),
		AssembleLaneB:  newIndexerStageConfig(indexingCfg.AssembleLaneB),
		RecoverYEnc:    newIndexerStageConfig(indexingCfg.RecoverYEnc),
		ReleaseStage: newIndexerStageConfig(app.IndexingStageRuntimeSettings{
			Enabled:         indexingCfg.Release.Enabled,
			IntervalMinutes: indexingCfg.Release.IntervalMinutes,
			BatchSize:       indexingCfg.Release.BatchSize,
			BackoffSeconds:  indexingCfg.Release.BackoffSeconds,
		}),
		InspectDiscovery: newIndexerStageConfig(indexingCfg.InspectDiscovery),
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

func withInspectStage(in inspectpkg.Options, stage indexerStageConfig, owner string) inspectpkg.Options {
	out := withInspectBatch(in, stage.BatchSize)
	out.Concurrency = stage.Concurrency
	out.ClaimOwner = owner
	out.ClaimLease = 15 * time.Minute
	return out
}

func IndexingStageRuntimeSettingsFromPredb(in app.IndexingPreDBRuntimeSettings) app.IndexingStageRuntimeSettings {
	return app.IndexingStageRuntimeSettings{
		Enabled:         in.Enabled,
		IntervalMinutes: in.IntervalMinutes,
		BatchSize:       in.BatchSize,
		BackoffSeconds:  in.BackoffSeconds,
	}
}

func IndexingStageRuntimeSettingsFromTMDB(in app.IndexingTMDBRuntimeSettings) app.IndexingStageRuntimeSettings {
	return app.IndexingStageRuntimeSettings{
		Enabled:         in.Enabled,
		IntervalMinutes: in.IntervalMinutes,
		BatchSize:       in.BatchSize,
		BackoffSeconds:  in.BackoffSeconds,
	}
}
