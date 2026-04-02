package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/runtime/wiring"
)

func (r *Runner) ExecuteIndexerScrape(once bool) {
	// compatibility path remains "latest".
	r.ExecuteIndexerScrapeLatest(once)
}

func (r *Runner) ExecuteIndexerScrapeLatest(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn and indexing.newsgroups.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.ScrapeOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer scrape --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer scrape --once completed")
		return
	}

	if err := wiring.RunIndexerScrapeScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerScrapeBackfill(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn and indexing.newsgroups.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer scrape backfill currently supports --once only")
	}

	if err := appCtx.UsenetIndexer.ScrapeBackfillOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer scrape backfill --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer scrape backfill --once completed")
}

func (r *Runner) ExecuteIndexerAssemble(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer assemble currently supports --once only")
	}

	if err := appCtx.UsenetIndexer.AssembleOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer assemble --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer assemble --once completed")
}

func (r *Runner) ExecuteIndexerRelease(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer release currently supports --once only")
	}

	if err := appCtx.UsenetIndexer.ReleaseOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer release --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer release --once completed")
}

func (r *Runner) ExecuteIndexerInspect(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer inspect currently supports --once only")
	}

	if err := appCtx.UsenetIndexer.InspectOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer inspect --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer inspect --once completed")
}

func (r *Runner) ExecuteIndexerInspectPAR2(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer inspect par2 currently supports --once only")
	}
	if err := appCtx.UsenetIndexer.InspectPAR2Once(ctx); err != nil {
		appCtx.Logger.Fatal("indexer inspect par2 --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer inspect par2 --once completed")
}

func (r *Runner) ExecuteIndexerInspectNFO(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer inspect nfo currently supports --once only")
	}
	if err := appCtx.UsenetIndexer.InspectNFOOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer inspect nfo --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer inspect nfo --once completed")
}

func (r *Runner) ExecuteIndexerInspectArchive(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer inspect archive currently supports --once only")
	}
	if err := appCtx.UsenetIndexer.InspectArchiveOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer inspect archive --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer inspect archive --once completed")
}

func (r *Runner) ExecuteIndexerInspectPassword(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer inspect password currently supports --once only")
	}
	if err := appCtx.UsenetIndexer.InspectPasswordOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer inspect password --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer inspect password --once completed")
}

func (r *Runner) ExecuteIndexerInspectMedia(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer inspect media currently supports --once only")
	}
	if err := appCtx.UsenetIndexer.InspectMediaOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer inspect media --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer inspect media --once completed")
}

func (r *Runner) ExecuteIndexerEnrichPreDB(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer enrich predb currently supports --once only")
	}
	if err := appCtx.UsenetIndexer.EnrichPredbOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer enrich predb --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer enrich predb --once completed")
}

func (r *Runner) ExecuteIndexerEnrichTMDB(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer enrich tmdb currently supports --once only")
	}
	if err := appCtx.UsenetIndexer.EnrichTMDBOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer enrich tmdb --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer enrich tmdb --once completed")
}

func (r *Runner) ExecuteIndexerPipeline(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("indexer pipeline currently supports --once only")
	}

	if err := appCtx.UsenetIndexer.RunPipelineOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer pipeline --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer pipeline --once completed")
}

func (r *Runner) setupIndexerCommand(notConfiguredMessage string) (*app.Context, context.Context, func()) {
	appCtx := r.setupApp(context.Background())

	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.Logger.Fatal("usenet_indexer module is disabled")
	}
	if appCtx.UsenetIndexer == nil {
		appCtx.Logger.Fatal("%s", notConfiguredMessage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return appCtx, ctx, func() {
		stop()
		appCtx.Close()
	}
}
