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
	"github.com/datallboy/gonzb/internal/nntp"
)

type usenetIndexerRuntime struct {
	service        app.UsenetIndexerService
	scrapeProvider io.Closer
	interval       time.Duration
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

	matcherSvc := match.NewService()
	assembleSvc := assemble.NewService(
		appCtx.PGIndexStore,
		matcherSvc,
		appCtx.Logger,
		assemble.Options{
			BatchSize: int(appCtx.Config.Indexing.ScrapeBatchSize),
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

	if len(appCtx.Config.Indexing.Newsgroups) > 0 {
		if len(appCtx.Config.Servers) == 0 {
			return nil, fmt.Errorf("usenet indexer scrape runtime requires at least one NNTP server")
		}

		provider := nntp.NewNNTPProvider(appCtx.Config.Servers[0])
		if err := provider.TestConnection(); err != nil {
			return nil, fmt.Errorf("scrape provider initialization failed: %w", err)
		}

		scrapeAdapter := scrape.NewNNTPAdapter(provider)
		scrapeSvc = scrape.NewService(
			appCtx.PGIndexStore,
			scrapeAdapter,
			appCtx.Logger,
			scrape.Options{
				Newsgroups: appCtx.Config.Indexing.Newsgroups,
				BatchSize:  appCtx.Config.Indexing.ScrapeBatchSize,
			},
		)

		interval := time.Duration(appCtx.Config.Indexing.ScheduleIntervalMinutes) * time.Minute
		schedulerSvc = scheduler.NewService(scrapeSvc, appCtx.Logger, interval)
		scrapeProvider = provider
	}

	service := indexing.NewService(scrapeSvc, assembleSvc, releaseSvc, schedulerSvc)

	return &usenetIndexerRuntime{
		service:        service,
		scrapeProvider: scrapeProvider,
		interval:       time.Duration(appCtx.Config.Indexing.ScheduleIntervalMinutes) * time.Minute,
	}, nil
}
