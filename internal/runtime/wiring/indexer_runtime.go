package wiring

import (
	"fmt"
	"io"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing"
	"github.com/datallboy/gonzb/internal/indexing/assemble"
	"github.com/datallboy/gonzb/internal/indexing/match"
	"github.com/datallboy/gonzb/internal/indexing/release"
	"github.com/datallboy/gonzb/internal/indexing/scheduler"
	"github.com/datallboy/gonzb/internal/indexing/scrape"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nntp"
)

type usenetIndexerRuntime struct {
	service        app.UsenetIndexerService
	scrapeProvider io.Closer
	interval       time.Duration
}

type usenetIndexerConfig struct {
	Newsgroups       []string
	ScrapeBatchSize  int64
	ScheduleInterval time.Duration
	ScrapeServer     *config.ServerConfig
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

	var (
		scrapeSvc      *scrape.Service
		schedulerSvc   *scheduler.Service
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

		schedulerSvc = scheduler.NewService(scrapeSvc, appCtx.Logger, runtimeCfg.ScheduleInterval)
		scrapeProvider = provider
	}

	service := indexing.NewService(scrapeSvc, assembleSvc, releaseSvc, schedulerSvc)

	return &usenetIndexerRuntime{
		service:        service,
		scrapeProvider: scrapeProvider,
		interval:       runtimeCfg.ScheduleInterval,
	}, nil
}

func deriveUsenetIndexerConfig(cfg *config.Config) (usenetIndexerConfig, error) {
	if cfg == nil {
		return usenetIndexerConfig{}, fmt.Errorf("app config is required")
	}

	out := usenetIndexerConfig{
		Newsgroups:       append([]string(nil), cfg.Indexing.Newsgroups...),
		ScrapeBatchSize:  cfg.Indexing.ScrapeBatchSize,
		ScheduleInterval: time.Duration(cfg.Indexing.ScheduleIntervalMinutes) * time.Minute,
	}

	if len(cfg.Servers) > 0 {
		server := cfg.Servers[0]
		out.ScrapeServer = &server
	}

	return out, nil
}
