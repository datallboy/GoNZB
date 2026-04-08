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
	"github.com/datallboy/gonzb/internal/indexing/inspect/media"
	"github.com/datallboy/gonzb/internal/indexing/inspect/nfo"
	"github.com/datallboy/gonzb/internal/indexing/inspect/par2"
	"github.com/datallboy/gonzb/internal/indexing/inspect/password"
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
	Newsgroups            []string
	ScrapeBatchSize       int64
	StageInterval         time.Duration
	ReleaseMinConfidence  float64
	ReleaseMinCompletion  float64
	ScrapeServer          *config.ServerConfig
	Inspect               inspectpkg.Options
	EnrichTMDB            tmdb.Options
	EnableInspectPAR2     bool
	EnableInspectNFO      bool
	EnableInspectArchive  bool
	EnableInspectPassword bool
	EnableInspectMedia    bool
	EnableEnrichPreDB     bool
	EnableEnrichTMDB      bool
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
		scrapeSvc      *scrape.Service
		scrapeProvider io.Closer
		inspectFetcher inspectpkg.ArticleFetcher
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
		scrapeSvc = scrape.NewService(
			appCtx.PGIndexStore,
			scrapeAdapter,
			appCtx.Logger,
			scrape.Options{
				Newsgroups: runtimeCfg.Newsgroups,
				BatchSize:  runtimeCfg.ScrapeBatchSize,
			},
		)
		scrapeProvider = provider
		inspectFetcher = provider
	}

	matcherSvc := match.NewService()
	assembleSvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		appCtx.Logger,
		assemble.Options{
			BatchSize: int(runtimeCfg.ScrapeBatchSize),
		},
	)

	releaseSvc := release.NewService(
		appCtx.PGIndexStore,
		appCtx.Logger,
		release.Options{
			BatchSize:            1000,
			ReleaseMinConfidence: runtimeCfg.ReleaseMinConfidence,
			ReleaseMinCompletion: runtimeCfg.ReleaseMinCompletion,
		},
	)
	workspaceManager := inspectpkg.NewWorkspaceManager(runtimeCfg.Inspect)
	commandRunner := inspectpkg.ExecCommandRunner{}
	inspectPAR2Svc := par2.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, appCtx.Logger, runtimeCfg.Inspect)
	inspectNFOSvc := nfo.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, appCtx.Logger, runtimeCfg.Inspect)
	inspectArchiveSvc := archive.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, commandRunner, appCtx.Logger, runtimeCfg.Inspect)
	inspectPasswordSvc := password.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, commandRunner, appCtx.Logger, runtimeCfg.Inspect)
	inspectMediaSvc := media.NewService(appCtx.PGIndexStore, workspaceManager, inspectFetcher, commandRunner, appCtx.Logger, runtimeCfg.Inspect)
	enrichPreDBSvc := predb.NewService(appCtx.PGIndexStore, appCtx.Logger, int(runtimeCfg.Inspect.CandidateBatchSize))
	enrichTMDBSvc := tmdb.NewService(appCtx.PGIndexStore, appCtx.Logger, runtimeCfg.EnrichTMDB)

	supervisorSvc := supervisor.New(appCtx.Logger, []supervisor.Stage{
		{
			Name:      supervisor.StageScrapeLatest,
			Interval:  runtimeCfg.StageInterval,
			Enabled:   scrapeSvc != nil,
			BatchSize: int(runtimeCfg.ScrapeBatchSize),
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return scrapeSvc.RunLatestOnce(ctx)
			}),
		},
		{
			Name:      supervisor.StageScrapeBackfill,
			Interval:  runtimeCfg.StageInterval,
			Enabled:   scrapeSvc != nil,
			BatchSize: int(runtimeCfg.ScrapeBatchSize),
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return scrapeSvc.RunBackfillOnce(ctx)
			}),
		},
		{
			Name:      supervisor.StageAssemble,
			Interval:  runtimeCfg.StageInterval,
			Enabled:   assembleSvc != nil,
			BatchSize: int(runtimeCfg.ScrapeBatchSize),
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return assembleSvc.RunOnce(ctx)
			}),
		},
		{
			Name:      supervisor.StageRelease,
			Interval:  runtimeCfg.StageInterval,
			Enabled:   releaseSvc != nil,
			BatchSize: 1000,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return releaseSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectPAR2,
			Interval: runtimeCfg.StageInterval,
			Enabled:  runtimeCfg.EnableInspectPAR2,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectPAR2Svc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectNFO,
			Interval: runtimeCfg.StageInterval,
			Enabled:  runtimeCfg.EnableInspectNFO,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectNFOSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectArchive,
			Interval: runtimeCfg.StageInterval,
			Enabled:  runtimeCfg.EnableInspectArchive,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectArchiveSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectPassword,
			Interval: runtimeCfg.StageInterval,
			Enabled:  runtimeCfg.EnableInspectPassword,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectPasswordSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectMedia,
			Interval: runtimeCfg.StageInterval,
			Enabled:  runtimeCfg.EnableInspectMedia,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectMediaSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageEnrichPreDB,
			Interval: runtimeCfg.StageInterval,
			Enabled:  runtimeCfg.EnableEnrichPreDB,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return enrichPreDBSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageEnrichTMDB,
			Interval: runtimeCfg.StageInterval,
			Enabled:  runtimeCfg.EnableEnrichTMDB,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return enrichTMDBSvc.RunOnce(ctx)
			}),
		},
	}, supervisor.Options{
		Tracker: appCtx.PGIndexStore,
		Owner:   stageOwner,
	})

	service := indexing.NewService(supervisorSvc, indexing.Options{
		ReleaseReform: releaseSvc.RunReformOnce,
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

	out := usenetIndexerConfig{
		Newsgroups:           append([]string(nil), cfg.Indexing.Newsgroups...),
		ScrapeBatchSize:      cfg.Indexing.ScrapeBatchSize,
		StageInterval:        time.Duration(cfg.Indexing.ScheduleIntervalMinutes * float64(time.Minute)),
		ReleaseMinConfidence: cfg.Indexing.ReleaseMinConfidence,
		ReleaseMinCompletion: cfg.Indexing.ReleaseMinCompletionPct,
		Inspect: inspectpkg.DefaultOptions(inspectpkg.Options{
			WorkDir:            cfg.Indexing.InspectWorkDir,
			MaxBytes:           cfg.Indexing.InspectMaxBytes,
			MaxArchiveDepth:    cfg.Indexing.InspectMaxArchiveDepth,
			ToolTimeout:        time.Duration(cfg.Indexing.InspectToolTimeoutSecs) * time.Second,
			FFProbePath:        cfg.Indexing.FFProbePath,
			SevenZipPath:       cfg.Indexing.SevenZipPath,
			UnrarPath:          cfg.Indexing.UnrarPath,
			PAR2Path:           cfg.Indexing.PAR2Path,
			CandidateBatchSize: 100,
		}),
		EnrichTMDB: tmdb.DefaultOptions(tmdb.Options{
			Limit:           100,
			TMDBAPIKey:      cfg.Indexing.TMDBAPIKey,
			TMDBAccessToken: cfg.Indexing.TMDBAccessToken,
			TMDBBaseURL:     cfg.Indexing.TMDBBaseURL,
			TVDBAPIKey:      cfg.Indexing.TVDBAPIKey,
			TVDBPIN:         cfg.Indexing.TVDBPIN,
			TVDBBaseURL:     cfg.Indexing.TVDBBaseURL,
		}),
		EnableInspectPAR2:     cfg.Indexing.EnableInspectPAR2,
		EnableInspectNFO:      cfg.Indexing.EnableInspectNFO,
		EnableInspectArchive:  cfg.Indexing.EnableInspectArchive,
		EnableInspectPassword: cfg.Indexing.EnableInspectPassword,
		EnableInspectMedia:    cfg.Indexing.EnableInspectMedia,
		EnableEnrichPreDB:     cfg.Indexing.EnableEnrichPreDB,
		EnableEnrichTMDB:      cfg.Indexing.EnableEnrichTMDB,
	}

	if len(cfg.Servers) > 0 {
		server := cfg.Servers[0]
		out.ScrapeServer = &server
	}

	return out, nil
}
