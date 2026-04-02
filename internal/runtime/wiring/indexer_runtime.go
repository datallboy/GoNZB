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
)

type usenetIndexerRuntime struct {
	service        app.UsenetIndexerService
	supervisor     *supervisor.Supervisor
	scrapeProvider io.Closer
}

type usenetIndexerConfig struct {
	Newsgroups      []string
	ScrapeBatchSize int64
	StageInterval   time.Duration
	ScrapeServer    *config.ServerConfig
}

func buildUsenetIndexerRuntime(appCtx *app.Context) (*usenetIndexerRuntime, error) {
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
			BatchSize: 1000,
		},
	)
	inspectPAR2Svc := par2.NewService(appCtx.Logger)
	inspectNFOSvc := nfo.NewService(appCtx.Logger)
	inspectArchiveSvc := archive.NewService(appCtx.Logger)
	inspectPasswordSvc := password.NewService(appCtx.Logger)
	inspectMediaSvc := media.NewService(appCtx.Logger)
	enrichPreDBSvc := predb.NewService(appCtx.Logger)
	enrichTMDBSvc := tmdb.NewService(appCtx.Logger)

	var (
		scrapeSvc      *scrape.Service
		scrapeProvider io.Closer
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
	}

	supervisorSvc := supervisor.New(appCtx.Logger, []supervisor.Stage{
		{
			Name:     supervisor.StageScrapeLatest,
			Interval: runtimeCfg.StageInterval,
			Enabled:  scrapeSvc != nil,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return scrapeSvc.RunLatestOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageScrapeBackfill,
			Interval: runtimeCfg.StageInterval,
			Enabled:  scrapeSvc != nil,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return scrapeSvc.RunBackfillOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageAssemble,
			Interval: runtimeCfg.StageInterval,
			Enabled:  assembleSvc != nil,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return assembleSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageRelease,
			Interval: runtimeCfg.StageInterval,
			Enabled:  releaseSvc != nil,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return releaseSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectPAR2,
			Interval: runtimeCfg.StageInterval,
			Enabled:  true,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectPAR2Svc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectNFO,
			Interval: runtimeCfg.StageInterval,
			Enabled:  true,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectNFOSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectArchive,
			Interval: runtimeCfg.StageInterval,
			Enabled:  true,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectArchiveSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectPassword,
			Interval: runtimeCfg.StageInterval,
			Enabled:  true,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectPasswordSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageInspectMedia,
			Interval: runtimeCfg.StageInterval,
			Enabled:  true,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return inspectMediaSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageEnrichPreDB,
			Interval: runtimeCfg.StageInterval,
			Enabled:  true,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return enrichPreDBSvc.RunOnce(ctx)
			}),
		},
		{
			Name:     supervisor.StageEnrichTMDB,
			Interval: runtimeCfg.StageInterval,
			Enabled:  true,
			Runner: supervisor.RunnerFunc(func(ctx context.Context) error {
				return enrichTMDBSvc.RunOnce(ctx)
			}),
		},
	})

	service := indexing.NewService(supervisorSvc)

	return &usenetIndexerRuntime{
		service:        service,
		supervisor:     supervisorSvc,
		scrapeProvider: scrapeProvider,
	}, nil
}

func deriveUsenetIndexerConfig(cfg *config.Config) (usenetIndexerConfig, error) {
	if cfg == nil {
		return usenetIndexerConfig{}, fmt.Errorf("app config is required")
	}

	out := usenetIndexerConfig{
		Newsgroups:      append([]string(nil), cfg.Indexing.Newsgroups...),
		ScrapeBatchSize: cfg.Indexing.ScrapeBatchSize,
		StageInterval:   time.Duration(cfg.Indexing.ScheduleIntervalMinutes) * time.Minute,
	}

	if len(cfg.Servers) > 0 {
		server := cfg.Servers[0]
		out.ScrapeServer = &server
	}

	return out, nil
}
