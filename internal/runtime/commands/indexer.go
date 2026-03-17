package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/datallboy/gonzb/internal/runtime/wiring"
)

func (r *Runner) ExecuteIndexerScrape(once bool) {
	appCtx := r.setupApp(context.Background())
	defer appCtx.Close()

	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.Logger.Fatal("usenet_indexer module is disabled")
	}
	if appCtx.UsenetIndexer == nil {
		appCtx.Logger.Fatal("Usenet/NZB Indexer is not configured. Set store.pg_dsn and indexing.newsgroups.")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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

func (r *Runner) ExecuteIndexerAssemble(once bool) {
	appCtx := r.setupApp(context.Background())
	defer appCtx.Close()

	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.Logger.Fatal("usenet_indexer module is disabled")
	}
	if appCtx.UsenetIndexer == nil {
		appCtx.Logger.Fatal("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !once {
		appCtx.Logger.Fatal("indexer assemble currently supports --once only")
	}

	if err := appCtx.UsenetIndexer.AssembleOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer assemble --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer assemble --once completed")
}

func (r *Runner) ExecuteIndexerRelease(once bool) {
	appCtx := r.setupApp(context.Background())
	defer appCtx.Close()

	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.Logger.Fatal("usenet_indexer module is disabled")
	}
	if appCtx.UsenetIndexer == nil {
		appCtx.Logger.Fatal("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !once {
		appCtx.Logger.Fatal("indexer release currently supports --once only")
	}

	if err := appCtx.UsenetIndexer.ReleaseOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer release --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer release --once completed")
}

func (r *Runner) ExecuteIndexerPipeline(once bool) {
	appCtx := r.setupApp(context.Background())
	defer appCtx.Close()

	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.Logger.Fatal("usenet_indexer module is disabled")
	}
	if appCtx.UsenetIndexer == nil {
		appCtx.Logger.Fatal("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if !once {
		appCtx.Logger.Fatal("indexer pipeline currently supports --once only")
	}

	if err := appCtx.UsenetIndexer.RunPipelineOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer pipeline --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer pipeline --once completed")
}
