package commands

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
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

func (r *Runner) ExecuteIndexerInspectPAR2(once bool) {
	r.executeIndexerStageOnceOnly(once, supervisor.StageInspectPAR2, "indexer inspect par2")
}

func (r *Runner) ExecuteIndexerInspectNFO(once bool) {
	r.executeIndexerStageOnceOnly(once, supervisor.StageInspectNFO, "indexer inspect nfo")
}

func (r *Runner) ExecuteIndexerInspectArchive(once bool) {
	r.executeIndexerStageOnceOnly(once, supervisor.StageInspectArchive, "indexer inspect archive")
}

func (r *Runner) ExecuteIndexerInspectPassword(once bool) {
	r.executeIndexerStageOnceOnly(once, supervisor.StageInspectPassword, "indexer inspect password")
}

func (r *Runner) ExecuteIndexerInspectMedia(once bool) {
	r.executeIndexerStageOnceOnly(once, supervisor.StageInspectMedia, "indexer inspect media")
}

func (r *Runner) ExecuteIndexerEnrichPreDB(once bool) {
	r.executeIndexerStageOnceOnly(once, supervisor.StageEnrichPreDB, "indexer enrich predb")
}

func (r *Runner) ExecuteIndexerEnrichTMDB(once bool) {
	r.executeIndexerStageOnceOnly(once, supervisor.StageEnrichTMDB, "indexer enrich tmdb")
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

func (r *Runner) executeIndexerStageOnceOnly(once bool, stageName supervisor.StageName, commandLabel string) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		appCtx.Logger.Fatal("%s currently supports --once only", commandLabel)
	}

	if err := appCtx.UsenetIndexer.RunStageOnce(ctx, string(stageName)); err != nil {
		appCtx.Logger.Fatal("%s --once failed: %v", commandLabel, err)
	}
	appCtx.Logger.Info("%s --once completed", commandLabel)
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
