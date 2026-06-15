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
	"github.com/datallboy/gonzb/internal/indexing/releasearchive"
	"github.com/datallboy/gonzb/internal/indexing/releasegenerate"
	"github.com/datallboy/gonzb/internal/indexing/releasepurge"
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
	AssembleLaneA                                   indexerStageConfig
	AssembleLaneB                                   indexerStageConfig
	RecoverYEnc                                     indexerStageConfig
	ReleaseSummaryRefreshStage                      indexerStageConfig
	ReleaseStage                                    indexerStageConfig
	ReleaseGenerateNZBStage                         indexerStageConfig
	ReleaseArchiveNZBStage                          indexerStageConfig
	ReleasePurgeArchivedSourcesStage                indexerStageConfig
	ReleaseReadyPolicy                              pgindex.ReleaseReadyPolicy
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
		assembleLaneAClient := indexerNNTPClient(manager, "assemble_lane_a")
		assembleLaneBClient := indexerNNTPClient(manager, "assemble_lane_b")
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
		assembleAFetcher = assembleLaneAClient
		assembleBFetcher = assembleLaneBClient
		inspectDiscoveryFetcher = indexerNNTPClient(manager, "inspect_discovery")
		inspectPAR2Fetcher = indexerNNTPClient(manager, "inspect_par2")
		inspectNFOFetcher = indexerNNTPClient(manager, "inspect_nfo")
		inspectArchiveFetcher = indexerNNTPClient(manager, "inspect_archive")
		inspectPasswordFetcher = indexerNNTPClient(manager, "inspect_password")
		inspectMediaFetcher = indexerNNTPClient(manager, "inspect_media")
		recoverFetcher = recoverYEncClient
	}

	matcherSvc := match.NewService(runtimeCfg.Match)
	assembleLaneASvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		assembleAFetcher,
		appCtx.Logger,
		assemble.Options{
			BatchSize:               runtimeCfg.AssembleLaneA.BatchSize,
			ClaimOwner:              "assemble-lane-a",
			ClaimLease:              5 * time.Minute,
			Concurrency:             runtimeCfg.AssembleLaneA.Concurrency,
			BinaryUpsertDBChunkSize: runtimeCfg.AssembleLaneA.BinaryUpsertDBChunkSize,
			Lane:                    pgindex.AssemblyClaimLaneA,
		},
	)
	assembleLaneBSvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		assembleBFetcher,
		appCtx.Logger,
		assemble.Options{
			BatchSize:               runtimeCfg.AssembleLaneB.BatchSize,
			ClaimOwner:              "assemble-lane-b",
			ClaimLease:              5 * time.Minute,
			Concurrency:             runtimeCfg.AssembleLaneB.Concurrency,
			BinaryUpsertDBChunkSize: runtimeCfg.AssembleLaneB.BinaryUpsertDBChunkSize,
			Lane:                    pgindex.AssemblyClaimLaneB,
		},
	)
	recoverYEncSvc := yencrecover.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		recoverFetcher,
		appCtx.Logger,
		yencrecover.Options{
			BatchSize:               runtimeCfg.RecoverYEnc.BatchSize,
			MaxHeaderBytes:          8192,
			FetchTimeout:            10 * time.Second,
			Concurrency:             runtimeCfg.RecoverYEnc.Concurrency,
			MaxEffectiveConcurrency: runtimeCfg.RecoverYEnc.MaxEffectiveConcurrency,
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
	releasePurgeSvc := releasepurge.NewService(
		appCtx.PGIndexStore,
		releasepurge.Options{BatchSize: runtimeCfg.ReleasePurgeArchivedSourcesStage.BatchSize, Policy: runtimeCfg.ReleaseReadyPolicy},
	)
	workspaceManager := inspectpkg.NewWorkspaceManager(runtimeCfg.Inspect)
	commandRunner := inspectpkg.ExecCommandRunner{}
	inspectDiscoverySvc := discovery.NewService(appCtx.PGIndexStore, inspectDiscoveryFetcher, appCtx.Logger, withInspectBatch(runtimeCfg.Inspect, runtimeCfg.InspectDiscovery.BatchSize))
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
		{
			Name:        supervisor.StageReleasePurgeArchivedSources,
			Interval:    runtimeCfg.ReleasePurgeArchivedSourcesStage.Interval,
			Enabled:     releasePurgeSvc != nil && runtimeCfg.ReleasePurgeArchivedSourcesStage.Enabled,
			BatchSize:   runtimeCfg.ReleasePurgeArchivedSourcesStage.BatchSize,
			Concurrency: 1,
			Backoff:     runtimeCfg.ReleasePurgeArchivedSourcesStage.Backoff,
			Runner: supervisor.ResultRunnerFunc(func(ctx context.Context) (json.RawMessage, error) {
				return marshalStageMetrics(releasePurgeSvc.RunOnceWithMetrics(ctx))
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
		StageGate: chainStageGates(
			newIndexerPrerequisiteGate(appCtx),
			newIndexerIntegrityGuard(appCtx.PGIndexStore),
			newIndexerScrapeBacklogGuard(appCtx),
			newIndexerPipelineBacklogGuard(appCtx),
			newIndexerNNTPTrafficGuard(appCtx, nntpStats),
			newIndexerStageResourceGuard(appCtx.PGIndexStore, runtimeCfg.StorageGuard, runtimeCfg.MemoryGuard),
		),
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
	if runtimeCfg.ScrapeServer == nil {
		return nil, false, fmt.Errorf("usenet indexer scrape runtime requires at least one NNTP server")
	}
	managerConfig := *appCtx.Config
	managerConfig.Servers = []config.ServerConfig{*runtimeCfg.ScrapeServer}
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
		AssembleLaneA:              newIndexerStageConfig(indexingCfg.AssembleLaneA),
		AssembleLaneB:              newIndexerStageConfig(indexingCfg.AssembleLaneB),
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
		StorageGuard: pgindex.DatabaseStorageGuardConfig{
			Enabled:        indexingCfg.StorageGuard.Enabled,
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
		Enabled:                 in.Enabled,
		Interval:                time.Duration(in.IntervalMinutes * float64(time.Minute)),
		BatchSize:               in.BatchSize,
		MaxBatches:              in.MaxBatches,
		Concurrency:             in.Concurrency,
		MaxEffectiveConcurrency: in.MaxEffectiveConcurrency,
		Backoff:                 time.Duration(in.BackoffSeconds) * time.Second,
		BinaryUpsertDBChunkSize: in.BinaryUpsertDBChunkSize,
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
