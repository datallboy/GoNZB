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
	"github.com/datallboy/gonzb/internal/indexing/ingestmaterialize"
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
	"github.com/datallboy/gonzb/internal/indexing/releasearchive"
	"github.com/datallboy/gonzb/internal/indexing/releasegenerate"
	"github.com/datallboy/gonzb/internal/indexing/scrape"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
	"github.com/datallboy/gonzb/internal/indexing/yencrecover"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/resolver"
	"github.com/datallboy/gonzb/internal/store/pgindex"
	"github.com/segmentio/ksuid"
)

type usenetIndexerRuntime struct {
	service        app.UsenetIndexerService
	supervisor     *supervisor.Supervisor
	scrapeProvider io.Closer
	nntpStats      func() app.NNTPRuntimeStats
}

type usenetIndexerConfig struct {
	Newsgroups                                      []string
	BackfillUntilDateByGroup                        map[string]time.Time
	ScrapeServer                                    *config.ServerConfig
	ScrapeServers                                   []config.ServerConfig
	ReleaseMinConfidence                            float64
	ReleaseMinCompletion                            float64
	ReleaseMinExpectedFileCoveragePct               float64
	ReleaseAutoReformBatchSize                      int
	RequireExpectedFileCountForContextualObfuscated bool
	ReopenArchivedNZBOnReleaseChange                bool
	Match                                           match.Options
	Inspect                                         inspectpkg.Options
	EnrichPreDB                                     predb.Options
	EnrichTMDB                                      tmdb.Options
	ScrapeLatest                                    indexerStageConfig
	ScrapeBackfill                                  indexerStageConfig
	PosterMaterialize                               indexerStageConfig
	CrosspostPopularityRefresh                      indexerStageConfig
	Assemble                                        indexerStageConfig
	RecoverYEnc                                     indexerStageConfig
	ReleaseSummaryRefreshStage                      indexerStageConfig
	ReleaseStage                                    indexerStageConfig
	ReleaseGenerateNZBStage                         indexerStageConfig
	ReleaseArchiveNZBStage                          indexerStageConfig
	ReleasePurgeArchivedSourcesStage                indexerStageConfig
	InspectDiscoveryReadyRefresh                    indexerStageConfig
	InspectPAR2ReadyRefresh                         indexerStageConfig
	InspectArchiveReadyRefresh                      indexerStageConfig
	InspectMediaReadyRefresh                        indexerStageConfig
	ReleaseReadyPolicy                              pgindex.ReleaseReadyPolicy
	RetentionPolicy                                 pgindex.RawStageRetentionPolicy
	StorageGuard                                    pgindex.DatabaseStorageGuardConfig
	MemoryGuard                                     IndexerMemoryGuardConfig
	InspectDiscovery                                indexerStageConfig
	InspectPAR2                                     indexerStageConfig
	InspectNFO                                      indexerStageConfig
	InspectArchive                                  indexerStageConfig
	InspectPassword                                 indexerStageConfig
	InspectMedia                                    indexerStageConfig
	EnrichPreDBStage                                indexerStageConfig
	EnrichTMDBStage                                 indexerStageConfig
	MaintenanceStage                                indexerStageConfig
	MaintenanceTasks                                map[string]app.IndexingMaintenanceTaskRuntimeSettings
}

type indexerStageConfig struct {
	Enabled                 bool
	Interval                time.Duration
	BatchSize               int
	MaxBatches              int
	Concurrency             int
	MaxEffectiveConcurrency int
	Backoff                 time.Duration
	BinaryUpsertDBChunkSize int
	LaneATargetPct          int
	LaneBMinPct             int
	LaneATimeWindowMinutes  int
	TargetWindowEnabled     bool
	TargetWindowStart       string
	TargetWindowEnd         string
	TargetWindowPct         int
	NewestPct               int
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
	if appCtx.DisableReleasePurgeArchivedSources {
		runtimeCfg.ReleasePurgeArchivedSourcesStage.Enabled = false
	}

	var (
		scrapeLatestSvc         *scrape.Service
		scrapeBackfillSvc       *scrape.Service
		scrapeProvider          io.Closer
		nntpStats               func() app.NNTPRuntimeStats
		assembleFetcher         inspectpkg.ArticleFetcher
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

	scrapeLatestSvc = scrape.NewService(
		appCtx.PGIndexStore,
		nil,
		appCtx.Logger,
		scrape.Options{
			Newsgroups:               runtimeCfg.Newsgroups,
			BatchSize:                int64(runtimeCfg.ScrapeLatest.BatchSize),
			Concurrency:              runtimeCfg.ScrapeLatest.Concurrency,
			MaxBatches:               runtimeCfg.ScrapeLatest.MaxBatches,
			BackfillUntilDateByGroup: runtimeCfg.BackfillUntilDateByGroup,
		},
	)
	scrapeBackfillSvc = scrape.NewService(
		appCtx.PGIndexStore,
		nil,
		appCtx.Logger,
		scrape.Options{
			Newsgroups:               runtimeCfg.Newsgroups,
			BatchSize:                int64(runtimeCfg.ScrapeBackfill.BatchSize),
			Concurrency:              runtimeCfg.ScrapeBackfill.Concurrency,
			MaxBatches:               runtimeCfg.ScrapeBackfill.MaxBatches,
			BackfillUntilDateByGroup: runtimeCfg.BackfillUntilDateByGroup,
		},
	)

	if runtimeCfg.ScrapeServer != nil {
		manager, ownedManager, err := indexerNNTPManager(appCtx, runtimeCfg)
		if err != nil {
			return nil, err
		}
		scrapeClient := indexerNNTPClient(manager, "scrape")
		assembleClient := indexerNNTPClient(manager, "assemble")
		recoverYEncClient := indexerNNTPClient(manager, "recover_yenc")

		if len(runtimeCfg.Newsgroups) > 0 {
			scrapeAdapter := scrape.NewNNTPAdapter(scrapeClient)
			scrapeLatestSvc = scrape.NewService(
				appCtx.PGIndexStore,
				scrapeAdapter,
				appCtx.Logger,
				scrape.Options{
					Newsgroups:               runtimeCfg.Newsgroups,
					BatchSize:                int64(runtimeCfg.ScrapeLatest.BatchSize),
					Concurrency:              runtimeCfg.ScrapeLatest.Concurrency,
					MaxBatches:               runtimeCfg.ScrapeLatest.MaxBatches,
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
					Concurrency:              runtimeCfg.ScrapeBackfill.Concurrency,
					MaxBatches:               runtimeCfg.ScrapeBackfill.MaxBatches,
					BackfillUntilDateByGroup: runtimeCfg.BackfillUntilDateByGroup,
				},
			)
		}
		if ownedManager {
			scrapeProvider = manager
		}
		nntpStats = func() app.NNTPRuntimeStats {
			return manager.RuntimeStats("indexer")
		}
		assembleFetcher = assembleClient
		inspectDiscoveryFetcher = indexerNNTPClient(manager, "inspect_discovery")
		inspectPAR2Fetcher = indexerNNTPClient(manager, "inspect_par2")
		inspectNFOFetcher = indexerNNTPClient(manager, "inspect_nfo")
		inspectArchiveFetcher = indexerNNTPClient(manager, "inspect_archive")
		inspectPasswordFetcher = indexerNNTPClient(manager, "inspect_password")
		inspectMediaFetcher = indexerNNTPClient(manager, "inspect_media")
		recoverFetcher = recoverYEncClient
	}

	matcherSvc := match.NewService(runtimeCfg.Match)
	assembleSvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		assembleFetcher,
		appCtx.Logger,
		assemble.Options{
			BatchSize:               runtimeCfg.Assemble.BatchSize,
			ClaimOwner:              "assemble",
			ClaimLease:              5 * time.Minute,
			Concurrency:             runtimeCfg.Assemble.Concurrency,
			BinaryUpsertDBChunkSize: runtimeCfg.Assemble.BinaryUpsertDBChunkSize,
			LaneATargetPct:          runtimeCfg.Assemble.LaneATargetPct,
			LaneBMinPct:             runtimeCfg.Assemble.LaneBMinPct,
			LaneATimeWindowMinutes:  runtimeCfg.Assemble.LaneATimeWindowMinutes,
		},
	)
	recoverYEncSvc := yencrecover.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		recoverFetcher,
		appCtx.Logger,
		yencrecover.Options{
			BatchSize:           runtimeCfg.RecoverYEnc.BatchSize,
			MaxHeaderBytes:      8192,
			FetchTimeout:        10 * time.Second,
			Concurrency:         runtimeCfg.RecoverYEnc.Concurrency,
			TargetWindowEnabled: runtimeCfg.RecoverYEnc.TargetWindowEnabled,
			TargetWindowStart:   runtimeCfg.RecoverYEnc.TargetWindowStart,
			TargetWindowEnd:     runtimeCfg.RecoverYEnc.TargetWindowEnd,
			TargetWindowPercent: runtimeCfg.RecoverYEnc.TargetWindowPct,
			NewestPercent:       runtimeCfg.RecoverYEnc.NewestPct,
		},
	)

	releaseSvc := release.NewService(
		appCtx.PGIndexStore,
		appCtx.Logger,
		release.Options{
			BatchSize:                                          runtimeCfg.ReleaseStage.BatchSize,
			SummaryRefreshBatchSize:                            runtimeCfg.ReleaseSummaryRefreshStage.BatchSize,
			SummaryRefreshMaxBatches:                           runtimeCfg.ReleaseSummaryRefreshStage.MaxBatches,
			ReleaseMinConfidence:                               runtimeCfg.ReleaseMinConfidence,
			ReleaseMinCompletion:                               runtimeCfg.ReleaseMinCompletion,
			ReleaseMinExpectedFileCoveragePct:                  runtimeCfg.ReleaseMinExpectedFileCoveragePct,
			AutoReformBatchSize:                                runtimeCfg.ReleaseAutoReformBatchSize,
			RequireExpectedFileCountForContextualObfuscated:    runtimeCfg.RequireExpectedFileCountForContextualObfuscated,
			RequireExpectedFileCountForContextualObfuscatedSet: true,
			ReopenArchivedNZBOnReleaseChange:                   runtimeCfg.ReopenArchivedNZBOnReleaseChange,
		},
	)
	archiveResolver := resolver.NewUsenetIndexResolver(appCtx.PGIndexStore, appCtx.IndexerArchiveStore)
	releaseArchiveSvc := releasearchive.NewService(
		appCtx.PGIndexStore,
		archiveResolver,
		appCtx.IndexerArchiveStore,
		appCtx.Logger,
		releasearchive.Options{BatchSize: runtimeCfg.ReleaseArchiveNZBStage.BatchSize},
	)
	releaseGenerateSvc := releasegenerate.NewService(
		appCtx.PGIndexStore,
		archiveResolver,
		appCtx.IndexerArchiveStore,
		releasegenerate.Options{
			BatchSize: runtimeCfg.ReleaseGenerateNZBStage.BatchSize,
			Policy:    runtimeCfg.ReleaseReadyPolicy,
		},
	)
	workspaceManager := inspectpkg.NewWorkspaceManager(runtimeCfg.Inspect)
	commandRunner := inspectpkg.ExecCommandRunner{}
	inspectDiscoverySvc := discovery.NewService(appCtx.PGIndexStore, inspectDiscoveryFetcher, appCtx.Logger, withInspectStage(runtimeCfg.Inspect, runtimeCfg.InspectDiscovery, stageOwner))
	inspectPAR2Svc := par2.NewService(appCtx.PGIndexStore, workspaceManager, inspectPAR2Fetcher, appCtx.Logger, withInspectStage(runtimeCfg.Inspect, runtimeCfg.InspectPAR2, stageOwner))
	inspectNFOSvc := nfo.NewService(appCtx.PGIndexStore, workspaceManager, inspectNFOFetcher, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectNFO.BatchSize))
	inspectArchiveSvc := archive.NewService(appCtx.PGIndexStore, workspaceManager, inspectArchiveFetcher, commandRunner, appCtx.IndexerArchiveStore, appCtx.Logger, withInspectStage(runtimeCfg.Inspect, runtimeCfg.InspectArchive, stageOwner))
	inspectPasswordSvc := password.NewService(appCtx.PGIndexStore, workspaceManager, inspectPasswordFetcher, commandRunner, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectPassword.BatchSize))
	inspectMediaSvc := media.NewService(appCtx.PGIndexStore, workspaceManager, inspectMediaFetcher, commandRunner, appCtx.IndexerArchiveStore, appCtx.Logger, withInspectStage(runtimeCfg.Inspect, runtimeCfg.InspectMedia, stageOwner))
	enrichPreDBSvc := predb.NewService(appCtx.PGIndexStore, appCtx.Logger, runtimeCfg.EnrichPreDB)
	enrichTMDBSvc := tmdb.NewService(appCtx.PGIndexStore, appCtx.Logger, runtimeCfg.EnrichTMDB)
	maintenanceSvc := maintenance.NewService(appCtx.PGIndexStore, appCtx.Logger, func(ctx context.Context) (int, error) {
		return inspectpkg.CleanupStaleWorkspaceRoots(ctx, runtimeCfg.Inspect)
	})
	posterMaterializeSvc := ingestmaterialize.NewService(
		appCtx.PGIndexStore,
		ingestmaterialize.Options{BatchSize: runtimeCfg.PosterMaterialize.BatchSize},
	)
	crosspostPopularitySvc := ingestmaterialize.NewService(
		appCtx.PGIndexStore,
		ingestmaterialize.Options{BatchSize: runtimeCfg.CrosspostPopularityRefresh.BatchSize},
	)
	readyQueueRefresher, _ := appCtx.PGIndexStore.(inspectionReadyQueueRefresher)

	supervisorSvc := supervisor.New(appCtx.Logger, []supervisor.Stage{
		{
			Name:        supervisor.StageScrapeLatest,
			Interval:    runtimeCfg.ScrapeLatest.Interval,
			Enabled:     runtimeCfg.ScrapeLatest.Enabled,
			BatchSize:   runtimeCfg.ScrapeLatest.BatchSize,
			Concurrency: runtimeCfg.ScrapeLatest.Concurrency,
			Backoff:     runtimeCfg.ScrapeLatest.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(scrapeLatestSvc.RunLatestOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageScrapeBackfill,
			Interval:    runtimeCfg.ScrapeBackfill.Interval,
			Enabled:     runtimeCfg.ScrapeBackfill.Enabled,
			BatchSize:   runtimeCfg.ScrapeBackfill.BatchSize,
			Concurrency: runtimeCfg.ScrapeBackfill.Concurrency,
			Backoff:     runtimeCfg.ScrapeBackfill.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(scrapeBackfillSvc.RunBackfillOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StagePosterMaterialize,
			Interval:    runtimeCfg.PosterMaterialize.Interval,
			Enabled:     runtimeCfg.PosterMaterialize.Enabled,
			BatchSize:   runtimeCfg.PosterMaterialize.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.PosterMaterialize.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(posterMaterializeSvc.RunPostersOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageCrosspostPopularityRefresh,
			Interval:    runtimeCfg.CrosspostPopularityRefresh.Interval,
			Enabled:     runtimeCfg.CrosspostPopularityRefresh.Enabled,
			BatchSize:   runtimeCfg.CrosspostPopularityRefresh.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.CrosspostPopularityRefresh.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(crosspostPopularitySvc.RunCrosspostPopularityOnceWithMetrics(ctx))
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
			Name:        supervisor.StageReleaseSummaryRefresh,
			Interval:    runtimeCfg.ReleaseSummaryRefreshStage.Interval,
			Enabled:     releaseSvc != nil && runtimeCfg.ReleaseSummaryRefreshStage.Enabled,
			BatchSize:   runtimeCfg.ReleaseSummaryRefreshStage.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.ReleaseSummaryRefreshStage.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(releaseSvc.RunSummaryRefreshOnceWithMetrics(ctx))
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
			Name:        supervisor.StageReleaseGenerateNZB,
			Interval:    runtimeCfg.ReleaseGenerateNZBStage.Interval,
			Enabled:     releaseGenerateSvc != nil && runtimeCfg.ReleaseGenerateNZBStage.Enabled,
			BatchSize:   runtimeCfg.ReleaseGenerateNZBStage.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.ReleaseGenerateNZBStage.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(releaseGenerateSvc.RunOnceWithMetrics(ctx))
			}),
		},
		{
			Name:        supervisor.StageReleaseArchiveNZB,
			Interval:    runtimeCfg.ReleaseArchiveNZBStage.Interval,
			Enabled:     releaseArchiveSvc != nil && appCtx.IndexerArchiveStore != nil && runtimeCfg.ReleaseArchiveNZBStage.Enabled,
			BatchSize:   runtimeCfg.ReleaseArchiveNZBStage.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.ReleaseArchiveNZBStage.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(releaseArchiveSvc.RunOnceWithMetrics(ctx))
			}),
		},
		inspectReadyRefreshStage(runtimeCfg.InspectDiscoveryReadyRefresh, supervisor.StageInspectDiscoveryReadyRefresh, "inspect_discovery", readyQueueRefresher),
		inspectReadyRefreshStage(runtimeCfg.InspectPAR2ReadyRefresh, supervisor.StageInspectPAR2ReadyRefresh, "inspect_par2", readyQueueRefresher),
		inspectReadyRefreshStage(runtimeCfg.InspectArchiveReadyRefresh, supervisor.StageInspectArchiveReadyRefresh, "inspect_archive", readyQueueRefresher),
		inspectReadyRefreshStage(runtimeCfg.InspectMediaReadyRefresh, supervisor.StageInspectMediaReadyRefresh, "inspect_media", readyQueueRefresher),
		{
			Name:        supervisor.StageInspectDiscovery,
			Interval:    runtimeCfg.InspectDiscovery.Interval,
			Enabled:     runtimeCfg.InspectDiscovery.Enabled,
			BatchSize:   runtimeCfg.InspectDiscovery.BatchSize,
			Concurrency: runtimeCfg.InspectDiscovery.Concurrency,
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
		maintenanceTaskStage(runtimeCfg, "dashboard_stats_refresh", supervisor.StageName("maintenance.dashboard_stats_refresh"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			stats, err := appCtx.PGIndexStore.RefreshIndexerDashboardStats(ctx)
			return dashboardStatsMaintenanceMetrics(stats), err
		}),
		maintenanceTaskStage(runtimeCfg, "release_source_purge", supervisor.StageMaintenanceReleaseSourcePurge, func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunReleaseSourcePurge(ctx, cfg.BatchSize, runtimeCfg.ReleaseReadyPolicy)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "poster_queue_done_cleanup", supervisor.StageName("maintenance.poster_queue_done_cleanup"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "poster_queue_done_cleanup", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "inspect_ready_queue_cleanup", supervisor.StageName("maintenance.inspect_ready_queue_cleanup"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "inspect_ready_queue_cleanup", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "assembly_queue_stale_cleanup", supervisor.StageName("maintenance.assembly_queue_stale_cleanup"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "assembly_queue_stale_cleanup", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "readiness_cleanup", supervisor.StageName("maintenance.readiness_cleanup"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			out, err := appCtx.PGIndexStore.RunIndexerMaintenance(ctx)
			if out == nil {
				return map[string]any{}, err
			}
			return map[string]any{
				"purged_readiness_summaries": out.PurgedReadinessSummaries,
				"purged_orphan_releases":     out.PurgedOrphanReleases,
			}, err
		}),
		maintenanceTaskStage(runtimeCfg, "runtime_history_cleanup", supervisor.StageName("maintenance.runtime_history_cleanup"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "runtime_history_cleanup", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "grouping_evidence_cleanup", supervisor.StageName("maintenance.grouping_evidence_cleanup"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "grouping_evidence_cleanup", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "crosspost_group_raw_purge", supervisor.StageName("maintenance.crosspost_group_raw_purge"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "crosspost_group_raw_purge", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "yenc_done_work_item_cleanup", supervisor.StageName("maintenance.yenc_done_work_item_cleanup"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "yenc_done_work_item_cleanup", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "group_profile_refresh", supervisor.StageName("maintenance.group_profile_refresh"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			updated, err := appCtx.PGIndexStore.RefreshIndexerGroupProfiles(ctx)
			return map[string]any{"groups_scored": updated}, err
		}),
		maintenanceTaskStage(runtimeCfg, "raw_stage_retention", supervisor.StageName("maintenance.raw_stage_retention"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunRawStageRetentionTask(ctx, cfg.BatchSize, runtimeCfg.RetentionPolicy)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "stale_nonrelease_source_purge", supervisor.StageName("maintenance.stale_nonrelease_source_purge"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "stale_nonrelease_source_purge", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "emergency_source_window_reset", supervisor.StageName("maintenance.emergency_source_window_reset"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "emergency_source_window_reset", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
		maintenanceTaskStage(runtimeCfg, "header_payload_purge", supervisor.StageName("maintenance.header_payload_purge"), func(ctx context.Context, cfg app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error) {
			result, err := appCtx.PGIndexStore.RunSimpleMaintenanceTask(ctx, "header_payload_purge", cfg.BatchSize)
			return maintenanceTaskMetrics(result), err
		}),
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
		StageGate: chainStageGates(
			newIndexerPrerequisiteGate(appCtx),
			newIndexerIntegrityGuard(appCtx.PGIndexStore),
			newIndexerScrapeBacklogGuard(appCtx),
			newIndexerPipelineBacklogGuard(appCtx),
			newIndexerNNTPTrafficGuard(appCtx, nntpStats),
			newIndexerStageResourceGuard(appCtx.PGIndexStore, runtimeCfg.StorageGuard, runtimeCfg.MemoryGuard, appCtx.SettingsStore, appCtx.BootstrapConfig),
		),
	})

	service := indexing.NewService(supervisorSvc, indexing.Options{
		Assemble:                assembleSvc.RunOnce,
		RecoverYEnc:             recoverYEncSvc.RunOnce,
		ReleaseReform:           releaseSvc.RunReformOnce,
		ReleaseReformReleases:   releaseSvc.RunReformReleasesOnce,
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
		nntpStats:      nntpStats,
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

func indexerNNTPManager(appCtx *app.Context, runtimeCfg usenetIndexerConfig) (*nntp.Manager, bool, error) {
	if appCtx != nil && appCtx.NNTP != nil {
		if sharedManager, ok := appCtx.NNTP.(*nntp.Manager); ok {
			return sharedManager, false, nil
		}
	}
	if len(runtimeCfg.ScrapeServers) == 0 && runtimeCfg.ScrapeServer == nil {
		return nil, false, fmt.Errorf("usenet indexer scrape runtime requires at least one NNTP server")
	}
	managerConfig := *appCtx.Config
	managerConfig.Servers = append([]config.ServerConfig(nil), runtimeCfg.ScrapeServers...)
	if len(managerConfig.Servers) == 0 {
		managerConfig.Servers = []config.ServerConfig{*runtimeCfg.ScrapeServer}
	}
	managerCtx := *appCtx
	managerCtx.Config = &managerConfig
	manager, err := nntp.NewManagerWithOptions(&managerCtx, managerOptionsFromRuntime(indexerRuntimeSettings(appCtx), nntp.CapacityWaitQueue))
	if err != nil {
		return nil, false, fmt.Errorf("scrape manager initialization failed: %w", err)
	}
	return manager, true, nil
}

func indexerNNTPClient(manager *nntp.Manager, scope string) *nntp.ManagerClient {
	return manager.ClientForScopeWithPolicy(scope, nntp.CapacityWaitQueue)
}

func indexerRuntimeSettings(appCtx *app.Context) *app.RuntimeSettings {
	if appCtx == nil || appCtx.SettingsStore == nil {
		return nil
	}
	runtime, err := appCtx.SettingsStore.GetRuntimeSettings(context.Background(), appCtx.BootstrapConfig)
	if err != nil {
		appCtx.Logger.Warn("Failed to load indexer NNTP runtime settings: %v", err)
		return nil
	}
	return runtime
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

type inspectionReadyQueueRefresher interface {
	RefreshInspectionReadyQueue(ctx context.Context, stageName string, limit int) (*pgindex.BinaryInspectionReadyQueueRefreshResult, error)
}

func inspectReadyRefreshStage(cfg indexerStageConfig, name supervisor.StageName, inspectStageName string, repo inspectionReadyQueueRefresher) supervisor.Stage {
	return supervisor.Stage{
		Name:        name,
		Interval:    cfg.Interval,
		Enabled:     cfg.Enabled,
		BatchSize:   cfg.BatchSize,
		Concurrency: 1,
		Backoff:     cfg.Backoff,
		Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
			if repo == nil {
				return marshalStageMetrics(map[string]any{}, fmt.Errorf("inspection ready queue repository is not configured"))
			}
			refreshed, err := repo.RefreshInspectionReadyQueue(ctx, inspectStageName, cfg.BatchSize)
			metrics := map[string]any{
				"inspect_stage":  inspectStageName,
				"ready_upserted": int64(0),
				"retired":        int64(0),
				"requeued":       int64(0),
			}
			if refreshed != nil {
				metrics["ready_upserted"] = refreshed.ReadyUpserted
				metrics["retired"] = refreshed.Retired
				metrics["requeued"] = refreshed.Requeued
			}
			return marshalStageMetrics(metrics, err)
		}),
	}
}

func maintenanceTaskStage(runtimeCfg usenetIndexerConfig, taskKey string, name supervisor.StageName, run func(context.Context, app.IndexingMaintenanceTaskRuntimeSettings) (map[string]any, error)) supervisor.Stage {
	cfg := runtimeCfg.MaintenanceTasks[taskKey]
	if cfg.IntervalHours <= 0 {
		cfg.IntervalHours = 24
	}
	if cfg.BatchSize <= 0 {
		cfg.BatchSize = 100
	}
	return supervisor.Stage{
		Name:        name,
		Interval:    time.Duration(cfg.IntervalHours) * time.Hour,
		Enabled:     cfg.Enabled && cfg.ScheduleEnabled,
		BatchSize:   cfg.BatchSize,
		Concurrency: 1,
		Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
			return marshalStageMetrics(run(ctx, cfg))
		}),
	}
}

func maintenanceTaskMetrics(result *pgindex.MaintenanceTaskResult) map[string]any {
	if result == nil {
		return map[string]any{}
	}
	return map[string]any{
		"task_key":              result.TaskKey,
		"deleted_rows_by_table": result.DeletedRowsByTable,
		"before_storage":        result.BeforeStorage,
		"after_storage":         result.AfterStorage,
		"blockers":              result.Blockers,
		"warnings":              result.Warnings,
	}
}

func dashboardStatsMaintenanceMetrics(stats *pgindex.IndexerDashboardStats) map[string]any {
	if stats == nil {
		return map[string]any{}
	}
	metrics := map[string]any{"stat_count": stats.Count}
	var available, exact, failed int
	for _, item := range stats.Items {
		if item.Available {
			available++
		}
		if item.Exact {
			exact++
		}
		if item.LastError != "" {
			failed++
		}
	}
	metrics["available_count"] = available
	metrics["exact_count"] = exact
	metrics["failed_count"] = failed
	metrics["unavailable_count"] = stats.Count - available
	return metrics
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
	if indexingCfg.SourceWindow.Enabled && indexingCfg.SourceWindow.BackfillWindowDays > 0 {
		sourceWindowCutoff := time.Now().UTC().AddDate(0, 0, -indexingCfg.SourceWindow.BackfillWindowDays)
		for _, group := range indexingCfg.Newsgroups {
			if _, exists := backfillCutoffs[group]; !exists {
				backfillCutoffs[group] = sourceWindowCutoff
			}
		}
	}

	out := usenetIndexerConfig{
		Newsgroups:                                      append([]string(nil), indexingCfg.Newsgroups...),
		BackfillUntilDateByGroup:                        backfillCutoffs,
		ReleaseMinConfidence:                            indexingCfg.Release.MinConfidence,
		ReleaseMinCompletion:                            indexingCfg.Release.MinCompletionPct,
		ReleaseMinExpectedFileCoveragePct:               indexingCfg.Release.MinExpectedFileCoveragePct,
		ReleaseAutoReformBatchSize:                      indexingCfg.Release.AutoReformBatchSize,
		RequireExpectedFileCountForContextualObfuscated: indexingCfg.Release.RequireExpectedFileCountForContextualObfuscated,
		ReopenArchivedNZBOnReleaseChange:                indexingCfg.Release.ReopenArchivedNZBOnReleaseChange,
		Match: match.Options{
			HighConfidenceThreshold:     indexingCfg.Match.HighConfidenceThreshold,
			ProbableConfidenceThreshold: indexingCfg.Match.ProbableConfidenceThreshold,
			ArticleBucketSize:           indexingCfg.Match.ArticleBucketSize,
		},
		Inspect: inspectpkg.DefaultOptions(inspectpkg.Options{
			WorkDir:                  indexingCfg.Inspect.WorkDir,
			WorkspaceBackend:         indexingCfg.Inspect.WorkspaceBackend,
			MemoryWorkDir:            indexingCfg.Inspect.MemoryWorkDir,
			MaxBytes:                 indexingCfg.Inspect.MaxBytes,
			MinBinaryBytes:           indexingCfg.Inspect.MinBinaryBytes,
			MaxBinaryBytes:           indexingCfg.Inspect.MaxBinaryBytes,
			RequireExpectedFileCount: indexingCfg.Inspect.RequireExpectedFileCount,
			BlockedMagicHex:          append([]string(nil), indexingCfg.Inspect.BlockedMagicHex...),
			MaxArchiveDepth:          indexingCfg.Inspect.MaxArchiveDepth,
			ToolTimeout:              time.Duration(indexingCfg.Inspect.ToolTimeoutSecs) * time.Second,
			FFmpegPath:               indexingCfg.Inspect.FFmpegPath,
			FFProbePath:              indexingCfg.Inspect.FFProbePath,
			SevenZipPath:             indexingCfg.Inspect.SevenZipPath,
			UnrarPath:                indexingCfg.Inspect.UnrarPath,
			PAR2Path:                 indexingCfg.Inspect.PAR2Path,
			CandidateBatchSize:       100,
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
		ScrapeLatest:               newIndexerStageConfig(indexingCfg.ScrapeLatest),
		ScrapeBackfill:             newIndexerStageConfig(indexingCfg.ScrapeBackfill),
		PosterMaterialize:          newIndexerStageConfig(indexingCfg.PosterMaterialize),
		CrosspostPopularityRefresh: newIndexerStageConfig(indexingCfg.CrosspostPopularityRefresh),
		Assemble:                   newIndexerStageConfig(indexingCfg.Assemble),
		RecoverYEnc:                newIndexerStageConfig(indexingCfg.RecoverYEnc),
		ReleaseSummaryRefreshStage: newIndexerStageConfig(indexingCfg.ReleaseSummaryRefresh),
		ReleaseStage: newIndexerStageConfig(app.IndexingStageRuntimeSettings{
			Enabled:         indexingCfg.Release.Enabled,
			IntervalMinutes: indexingCfg.Release.IntervalMinutes,
			BatchSize:       indexingCfg.Release.BatchSize,
			BackoffSeconds:  indexingCfg.Release.BackoffSeconds,
		}),
		ReleaseGenerateNZBStage:          newIndexerStageConfig(indexingCfg.ReleaseGenerateNZB),
		ReleaseArchiveNZBStage:           newIndexerStageConfig(indexingCfg.ReleaseArchiveNZB),
		ReleasePurgeArchivedSourcesStage: newIndexerStageConfig(indexingCfg.ReleasePurgeArchivedSources),
		InspectDiscoveryReadyRefresh:     newIndexerStageConfig(indexingCfg.InspectDiscoveryReadyRefresh),
		InspectPAR2ReadyRefresh:          newIndexerStageConfig(indexingCfg.InspectPAR2ReadyRefresh),
		InspectArchiveReadyRefresh:       newIndexerStageConfig(indexingCfg.InspectArchiveReadyRefresh),
		InspectMediaReadyRefresh:         newIndexerStageConfig(indexingCfg.InspectMediaReadyRefresh),
		ReleaseReadyPolicy: pgindex.NormalizeReleaseReadyPolicy(pgindex.ReleaseReadyPolicy{
			MinMatchConfidence:                   indexingCfg.Release.PublicMinMatchConfidence,
			MinCompletionPct:                     indexingCfg.Release.PublicMinCompletionPct,
			MinIdentityStatus:                    indexingCfg.Release.PublicMinIdentityStatus,
			RequireInspection:                    indexingCfg.Release.PublicRequireInspection,
			RequireEnrichment:                    indexingCfg.Release.PublicRequireEnrichment,
			RequirePayloadComplete:               indexingCfg.Release.PublicRequirePayloadComplete,
			RequireExpectedFileCountComplete:     indexingCfg.Release.PublicRequireExpectedFileCountComplete,
			RequirePAR2:                          indexingCfg.Release.PublicRequirePAR2,
			RequireNFO:                           indexingCfg.Release.PublicRequireNFO,
			RequireSFV:                           indexingCfg.Release.PublicRequireSFV,
			RetainUntilExpectedFileCountComplete: indexingCfg.Release.RetainUntilExpectedFileCountComplete,
			RetainRequirePAR2:                    indexingCfg.Release.RetainRequirePAR2,
			RetainRequireNFO:                     indexingCfg.Release.RetainRequireNFO,
			RetainRequireSFV:                     indexingCfg.Release.RetainRequireSFV,
		}),
		RetentionPolicy: pgindex.RawStageRetentionPolicy{
			HotHours:         indexingCfg.Retention.RawStageHotHours,
			WarmHours:        indexingCfg.Retention.RawStageWarmHours,
			ColdHours:        indexingCfg.Retention.RawStageColdHours,
			FailedProbeHours: indexingCfg.Retention.FailedProbeHours,
			DoneYEncHours:    indexingCfg.Retention.RawStageWarmHours,
		},
		StorageGuard: pgindex.DatabaseStorageGuardConfig{
			Enabled:        indexingCfg.StorageGuard.Enabled,
			DataDirectory:  indexingCfg.StorageGuard.DataDirectory,
			MinFreeBytes:   indexingCfg.StorageGuard.MinFreeBytes,
			MinFreePercent: indexingCfg.StorageGuard.MinFreePercent,
		},
		MemoryGuard: IndexerMemoryGuardConfig{
			Enabled:             indexingCfg.MemoryGuard.Enabled,
			MinAvailableBytes:   indexingCfg.MemoryGuard.MinAvailableBytes,
			MinAvailablePercent: indexingCfg.MemoryGuard.MinAvailablePercent,
			MinSwapFreeBytes:    indexingCfg.MemoryGuard.MinSwapFreeBytes,
		},
		InspectDiscovery: newIndexerStageConfig(indexingCfg.InspectDiscovery),
		InspectPAR2:      newIndexerStageConfig(indexingCfg.InspectPAR2),
		InspectNFO:       newIndexerStageConfig(indexingCfg.InspectNFO),
		InspectArchive:   newIndexerStageConfig(indexingCfg.InspectArchive),
		InspectPassword:  newIndexerStageConfig(indexingCfg.InspectPassword),
		InspectMedia:     newIndexerStageConfig(indexingCfg.InspectMedia),
		EnrichPreDBStage: newIndexerStageConfig(IndexingStageRuntimeSettingsFromPredb(indexingCfg.EnrichPreDB)),
		EnrichTMDBStage:  newIndexerStageConfig(IndexingStageRuntimeSettingsFromTMDB(indexingCfg.EnrichTMDB)),
		MaintenanceTasks: indexingCfg.MaintenanceTasks,
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
		out.ScrapeServers = append([]config.ServerConfig(nil), cfg.Servers...)
	}

	return out, nil
}

func newIndexerStageConfig(in app.IndexingStageRuntimeSettings) indexerStageConfig {
	return indexerStageConfig{
		Enabled:                 in.Enabled,
		Interval:                time.Duration(in.IntervalMinutes * float64(time.Minute)),
		BatchSize:               in.BatchSize,
		MaxBatches:              in.MaxBatches,
		Concurrency:             in.Concurrency,
		MaxEffectiveConcurrency: in.MaxEffectiveConcurrency,
		Backoff:                 time.Duration(in.BackoffSeconds) * time.Second,
		BinaryUpsertDBChunkSize: in.BinaryUpsertDBChunkSize,
		LaneATargetPct:          in.LaneATargetPct,
		LaneBMinPct:             in.LaneBMinPct,
		LaneATimeWindowMinutes:  in.LaneATimeWindowMinutes,
		TargetWindowEnabled:     in.TargetWindowEnabled,
		TargetWindowStart:       in.TargetWindowStart,
		TargetWindowEnd:         in.TargetWindowEnd,
		TargetWindowPct:         in.TargetWindowPct,
		NewestPct:               in.NewestPct,
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
