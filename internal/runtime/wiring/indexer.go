package wiring

import (
	"context"
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

// Build current effective Usenet/NZB Indexer runtime from app context.
func BuildUsenetIndexer(appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.UsenetIndexer = nil
		return nil
	}

	rt, err := buildUsenetIndexerRuntime(appCtx)
	if err != nil {
		return err
	}

	appCtx.UsenetIndexer = rt.service
	if rt.scrapeProvider != nil {
		appCtx.AddCloser(rt.scrapeProvider)
	}

	return nil
}

// Long-running scrape mode restart loop lives outside cmd/main.
func RunIndexerScrapeScheduler(ctx context.Context, appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		return fmt.Errorf("usenet_indexer module is disabled")
	}

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	type runtimeState struct {
		cancel func()
		closer io.Closer
	}

	var state runtimeState

	startRuntime := func(parent context.Context) error {
		rt, err := buildUsenetIndexerRuntime(appCtx)
		if err != nil {
			return err
		}
		if rt.service == nil {
			return fmt.Errorf("usenet indexer runtime is not configured")
		}

		appCtx.UsenetIndexer = rt.service

		childCtx, childCancel := context.WithCancel(parent)
		state.cancel = childCancel
		state.closer = rt.scrapeProvider

		go func() {
			if err := appCtx.UsenetIndexer.Start(childCtx, rt.interval); err != nil && childCtx.Err() == nil {
				appCtx.Logger.Error("indexer scheduler failed: %v", err)
			}
		}()

		return nil
	}

	stopRuntime := func() {
		if state.cancel != nil {
			state.cancel()
			state.cancel = nil
		}
		if state.closer != nil {
			if err := state.closer.Close(); err != nil {
				appCtx.Logger.Warn("failed to close previous scrape provider: %v", err)
			}
			state.closer = nil
		}
	}

	if err := startRuntime(runCtx); err != nil {
		return err
	}
	defer stopRuntime()

	if appCtx.SettingsStore == nil {
		<-ctx.Done()
		return nil
	}

	ch, err := appCtx.SettingsStore.WatchSettingsChanges(ctx)
	if err != nil {
		return fmt.Errorf("failed to start settings watcher: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case _, ok := <-ch:
			if !ok {
				return nil
			}

			if err := app.LoadAndApplyEffectiveConfig(ctx, appCtx); err != nil {
				appCtx.Logger.Error("failed to apply runtime settings update: %v", err)
				continue
			}

			stopRuntime()
			if err := startRuntime(runCtx); err != nil {
				appCtx.Logger.Error("failed to rebuild indexer scheduler runtime: %v", err)
				continue
			}

			appCtx.Logger.Info("Applied runtime settings update to indexer scheduler runtime")
		}
	}
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
