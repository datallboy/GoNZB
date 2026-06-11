package commands

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/scheduler"
	"github.com/datallboy/gonzb/internal/runtime/wiring"
	"github.com/datallboy/gonzb/internal/store/pgindex"
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

	if once {
		if err := appCtx.UsenetIndexer.ScrapeBackfillOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer scrape backfill --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer scrape backfill --once completed")
		return
	}

	if err := wiring.RunIndexerScrapeBackfillScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer backfill scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerAssembleLaneA(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.AssembleLaneAOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer assemble lane-a --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer assemble lane-a --once completed")
		return
	}

	if err := wiring.RunIndexerAssembleLaneAScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer assemble lane-a scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerAssembleLaneB(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.AssembleLaneBOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer assemble lane-b --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer assemble lane-b --once completed")
		return
	}

	if err := wiring.RunIndexerAssembleLaneBScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer assemble lane-b scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerRecoverYEnc(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn and an indexer NNTP server.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.RecoverYEncOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer recover-yenc --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer recover-yenc --once completed")
		return
	}

	if err := wiring.RunIndexerRecoverYEncScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer recover-yenc scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerRelease(once bool, reform bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if reform && !once {
		appCtx.Logger.Fatal("indexer release --reform currently requires --once")
	}

	if once {
		var err error
		if reform {
			err = appCtx.UsenetIndexer.ReformReleasesOnce(ctx)
		} else {
			err = appCtx.UsenetIndexer.ReleaseOnce(ctx)
		}
		if err != nil {
			if reform {
				appCtx.Logger.Fatal("indexer release --once --reform failed: %v", err)
			}
			appCtx.Logger.Fatal("indexer release --once failed: %v", err)
		}
		if reform {
			appCtx.Logger.Info("indexer release --once --reform completed")
			return
		}
		appCtx.Logger.Info("indexer release --once completed")
		return
	}

	if err := wiring.RunIndexerReleaseScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer release scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerReleaseSummaryRefresh(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.ReleaseSummaryRefreshOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer release refresh-summaries --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer release refresh-summaries --once completed")
		return
	}

	if err := wiring.RunIndexerReleaseSummaryRefreshScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer release refresh-summaries scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerReleaseGenerateNZB(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.ReleaseGenerateNZBOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer release generate-nzb --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer release generate-nzb --once completed")
		return
	}

	if err := wiring.RunIndexerReleaseGenerateNZBScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer release generate-nzb scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerReleaseArchiveNZB(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.ReleaseArchiveNZBOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer release archive-nzb --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer release archive-nzb --once completed")
		return
	}

	if err := wiring.RunIndexerReleaseArchiveNZBScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer release archive-nzb scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerReleasePurgeArchivedSources(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.ReleasePurgeArchivedSourcesOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer release purge-archived-sources --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer release purge-archived-sources --once completed")
		return
	}

	if err := wiring.RunIndexerReleasePurgeArchivedSourcesScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer release purge-archived-sources scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerInspect(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if once {
		if err := appCtx.UsenetIndexer.InspectOnce(ctx); err != nil {
			appCtx.Logger.Fatal("indexer inspect --once failed: %v", err)
		}
		appCtx.Logger.Info("indexer inspect --once completed")
		return
	}

	if err := wiring.RunIndexerInspectScheduler(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("indexer inspect scheduler failed: %v", err)
	}
}

func (r *Runner) ExecuteIndexerInspectDiscovery(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		if err := wiring.RunIndexerInspectDiscoveryScheduler(ctx, appCtx); err != nil {
			appCtx.Logger.Fatal("indexer inspect discovery scheduler failed: %v", err)
		}
		return
	}
	if err := appCtx.UsenetIndexer.InspectDiscoveryOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer inspect discovery --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer inspect discovery --once completed")
}

func (r *Runner) ExecuteIndexerInspectPAR2(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		if err := wiring.RunIndexerInspectPAR2Scheduler(ctx, appCtx); err != nil {
			appCtx.Logger.Fatal("indexer inspect par2 scheduler failed: %v", err)
		}
		return
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
		if err := wiring.RunIndexerInspectNFOScheduler(ctx, appCtx); err != nil {
			appCtx.Logger.Fatal("indexer inspect nfo scheduler failed: %v", err)
		}
		return
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
		if err := wiring.RunIndexerInspectArchiveScheduler(ctx, appCtx); err != nil {
			appCtx.Logger.Fatal("indexer inspect archive scheduler failed: %v", err)
		}
		return
	}
	if err := appCtx.UsenetIndexer.InspectArchiveOnce(ctx); err != nil {
		if errors.Is(err, context.Canceled) {
			appCtx.Logger.Info("indexer inspect archive canceled")
			return
		}
		appCtx.Logger.Fatal("indexer inspect archive --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer inspect archive --once completed")
}

func (r *Runner) ExecuteIndexerInspectPassword(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		if err := wiring.RunIndexerInspectPasswordScheduler(ctx, appCtx); err != nil {
			appCtx.Logger.Fatal("indexer inspect password scheduler failed: %v", err)
		}
		return
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
		if err := wiring.RunIndexerInspectMediaScheduler(ctx, appCtx); err != nil {
			appCtx.Logger.Fatal("indexer inspect media scheduler failed: %v", err)
		}
		return
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
		if err := wiring.RunIndexerEnrichPredbScheduler(ctx, appCtx); err != nil {
			appCtx.Logger.Fatal("indexer enrich predb scheduler failed: %v", err)
		}
		return
	}
	if err := appCtx.UsenetIndexer.EnrichPredbOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer enrich predb --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer enrich predb --once completed")
}

func (r *Runner) ExecuteIndexerEnrichPreDBSceneNameRecovery(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		if err := r.runIndexerTaskScheduler(ctx, appCtx, app.IndexingRuntimeFromConfig(appCtx.Config.Indexing).EnrichPreDB.IntervalMinutes, func(runCtx context.Context) error {
			return appCtx.UsenetIndexer.EnrichPredbSceneNameRecoveryOnce(runCtx)
		}); err != nil {
			appCtx.Logger.Fatal("indexer enrich predb scene-name-recovery scheduler failed: %v", err)
		}
		return
	}
	if err := appCtx.UsenetIndexer.EnrichPredbSceneNameRecoveryOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer enrich predb scene-name-recovery --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer enrich predb scene-name-recovery --once completed")
}

func (r *Runner) ExecuteIndexerEnrichPreDBMetadataOnlyFallback(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		if err := r.runIndexerTaskScheduler(ctx, appCtx, app.IndexingRuntimeFromConfig(appCtx.Config.Indexing).EnrichPreDB.IntervalMinutes, func(runCtx context.Context) error {
			return appCtx.UsenetIndexer.EnrichPredbMetadataFallbackOnce(runCtx)
		}); err != nil {
			appCtx.Logger.Fatal("indexer enrich predb metadata-only-fallback scheduler failed: %v", err)
		}
		return
	}
	if err := appCtx.UsenetIndexer.EnrichPredbMetadataFallbackOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer enrich predb metadata-only-fallback --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer enrich predb metadata-only-fallback --once completed")
}

func (r *Runner) ExecuteIndexerEnrichPreDBSyncFeed(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		if err := r.runIndexerTaskScheduler(ctx, appCtx, app.IndexingRuntimeFromConfig(appCtx.Config.Indexing).EnrichPreDB.IntervalMinutes, func(runCtx context.Context) error {
			return appCtx.UsenetIndexer.EnrichPredbSyncFeedOnce(runCtx)
		}); err != nil {
			appCtx.Logger.Fatal("indexer enrich predb sync-feed scheduler failed: %v", err)
		}
		return
	}
	if err := appCtx.UsenetIndexer.EnrichPredbSyncFeedOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer enrich predb sync-feed --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer enrich predb sync-feed --once completed")
}

func (r *Runner) ExecuteIndexerEnrichPreDBSyncBackfill(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		if err := r.runIndexerTaskScheduler(ctx, appCtx, app.IndexingRuntimeFromConfig(appCtx.Config.Indexing).EnrichPreDB.IntervalMinutes, func(runCtx context.Context) error {
			return appCtx.UsenetIndexer.EnrichPredbSyncBackfillOnce(runCtx)
		}); err != nil {
			appCtx.Logger.Fatal("indexer enrich predb sync-backfill scheduler failed: %v", err)
		}
		return
	}
	if err := appCtx.UsenetIndexer.EnrichPredbSyncBackfillOnce(ctx); err != nil {
		appCtx.Logger.Fatal("indexer enrich predb sync-backfill --once failed: %v", err)
	}
	appCtx.Logger.Info("indexer enrich predb sync-backfill --once completed")
}

func (r *Runner) ExecuteIndexerEnrichTMDB(once bool) {
	appCtx, ctx, cleanup := r.setupIndexerCommand("Usenet/NZB Indexer is not configured. Set store.pg_dsn.")
	defer cleanup()

	if !once {
		if err := wiring.RunIndexerEnrichTMDBScheduler(ctx, appCtx); err != nil {
			appCtx.Logger.Fatal("indexer enrich tmdb scheduler failed: %v", err)
		}
		return
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

func (r *Runner) ExecuteIndexerMaintenance() {
	appCtx, ctx, cleanup := r.setupIndexerStoreCommand("Usenet/NZB Indexer maintenance requires store.pg_dsn.")
	defer cleanup()

	out, err := appCtx.PGIndexStore.RunIndexerMaintenance(ctx)
	if err != nil {
		appCtx.Logger.Fatal("indexer maintenance failed: %v", err)
	}
	if out != nil {
		appCtx.Logger.Info(
			"indexer maintenance: abandoned_stage_runs=%d cleared_stage_leases=%d abandoned_scrape_runs=%d abandoned_binary_inspections=%d backfilled_catalog_files=%d purged_stage_runs=%d purged_scrape_runs=%d purged_binary_inspections=%d purged_header_payloads=%d purged_grouping_evidence=%d purged_readiness_summaries=%d purged_orphan_releases=%d skipped_readiness_cleanup=%t refresh_queue_backlog=%d",
			out.AbandonedStageRuns,
			out.ClearedStageLeases,
			out.AbandonedScrapeRuns,
			out.AbandonedBinaryInspections,
			out.BackfilledCatalogFiles,
			out.PurgedStageRuns,
			out.PurgedScrapeRuns,
			out.PurgedBinaryInspections,
			out.PurgedHeaderPayloads,
			out.PurgedGroupingEvidence,
			out.PurgedReadinessSummaries,
			out.PurgedOrphanReleases,
			out.SkippedReadinessCleanup,
			out.RefreshQueueBacklog,
		)
	}
	appCtx.Logger.Info("indexer maintenance completed")
}

func (r *Runner) ExecuteIndexerPurgeHeaderPayloads() {
	appCtx, ctx, cleanup := r.setupIndexerStoreCommand("Usenet/NZB Indexer payload purge requires store.pg_dsn.")
	defer cleanup()

	purged, err := appCtx.PGIndexStore.PurgeArticleHeaderPayloads(ctx)
	if err != nil {
		appCtx.Logger.Fatal("indexer maintenance purge-header-payloads failed: %v", err)
	}
	appCtx.Logger.Info("indexer maintenance purge-header-payloads: purged_header_payloads=%d", purged)
}

func (r *Runner) ExecuteIndexerStorageReclaim(tables []string, full bool, checkOnly bool) {
	appCtx, ctx, cleanup := r.setupIndexerStoreCommand("Usenet/NZB Indexer storage reclaim requires store.pg_dsn.")
	defer cleanup()

	out, err := appCtx.PGIndexStore.RunIndexerStorageReclaim(ctx, pgindex.IndexerStorageReclaimOptions{
		Tables:    tables,
		Full:      full,
		CheckOnly: checkOnly,
	})
	if err != nil {
		appCtx.Logger.Fatal("indexer storage reclaim failed: %v", err)
	}
	if out != nil {
		for _, table := range out.Tables {
			appCtx.Logger.Info(
				"indexer storage reclaim: mode=%s table=%s before_bytes=%d after_bytes=%d delta_bytes=%d",
				out.Mode,
				table.Table,
				table.BeforeBytes,
				table.AfterBytes,
				table.AfterBytes-table.BeforeBytes,
			)
		}
	}
	appCtx.Logger.Info("indexer storage reclaim completed")
}

func (r *Runner) ExecuteIndexerRepairRuntime() {
	appCtx, ctx, cleanup := r.setupIndexerStoreCommand("Usenet/NZB Indexer maintenance requires store.pg_dsn.")
	defer cleanup()

	out, err := appCtx.PGIndexStore.RepairIndexerStageRuntime(ctx)
	if err != nil {
		appCtx.Logger.Fatal("indexer maintenance repair-runtime failed: %v", err)
	}
	if out != nil {
		appCtx.Logger.Info(
			"indexer stage runtime repair: abandoned_runs=%d cleared_stale_leases=%d",
			out.AbandonedRuns,
			out.ClearedStaleLeases,
		)
	}
	appCtx.Logger.Info("indexer maintenance repair-runtime completed")
}

func (r *Runner) ExecuteIndexerCheckIntegrity(ensureExtension bool) {
	appCtx, ctx, cleanup := r.setupIndexerStoreCommand("Usenet/NZB Indexer integrity check requires store.pg_dsn.")
	defer cleanup()

	report, err := appCtx.PGIndexStore.CheckCriticalIndexerIntegrity(ctx, ensureExtension)
	if err != nil {
		appCtx.Logger.Fatal("indexer maintenance check-integrity failed: %v", err)
	}
	if report == nil {
		appCtx.Logger.Fatal("indexer maintenance check-integrity failed: no report returned")
	}
	for _, check := range report.Checks {
		appCtx.Logger.Info(
			"indexer integrity: relation=%s access_method=%s metadata_ok=%t amcheck_ran=%t ok=%t detail=%s",
			check.Relation,
			check.AccessMethod,
			check.MetadataOK,
			check.AmcheckRan,
			check.OK,
			check.Detail,
		)
	}
	if report.HasFailures() {
		appCtx.Logger.Fatal("indexer integrity failed: %s", report.FailureSummary())
	}
	appCtx.Logger.Info("indexer integrity check completed")
}

func (r *Runner) ExecuteIndexerReindexCritical() {
	appCtx, ctx, cleanup := r.setupIndexerStoreCommand("Usenet/NZB Indexer critical reindex requires store.pg_dsn.")
	defer cleanup()

	out, err := appCtx.PGIndexStore.ReindexCriticalIndexerIndexes(ctx)
	if err != nil {
		appCtx.Logger.Fatal("indexer maintenance reindex-critical failed: %v", err)
	}
	if out != nil {
		for _, relation := range out.Reindexed {
			appCtx.Logger.Info("indexer integrity repair: reindexed=%s", relation)
		}
	}
	appCtx.Logger.Info("indexer critical reindex completed")
}

func (r *Runner) setupIndexerCommand(notConfiguredMessage string) (*app.Context, context.Context, func()) {
	appCtx := r.setupApp(context.Background())

	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.Logger.Fatal("usenet_indexer module is disabled")
	}
	if appCtx.UsenetIndexer == nil {
		appCtx.Logger.Fatal("%s", notConfiguredMessage)
	}
	if appCtx.PGIndexStore != nil {
		if repair, err := appCtx.PGIndexStore.RepairIndexerStageRuntime(context.Background()); err != nil {
			appCtx.Logger.Warn("indexer stage runtime repair failed: %v", err)
		} else if repair != nil && (repair.AbandonedRuns > 0 || repair.ClearedStaleLeases > 0) {
			appCtx.Logger.Info(
				"indexer stage runtime repair: abandoned_runs=%d cleared_stale_leases=%d",
				repair.AbandonedRuns,
				repair.ClearedStaleLeases,
			)
		}
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return appCtx, ctx, func() {
		stop()
		appCtx.Close()
	}
}

func (r *Runner) setupIndexerStoreCommand(notConfiguredMessage string) (*app.Context, context.Context, func()) {
	appCtx := r.setupApp(context.Background())

	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		appCtx.Logger.Fatal("usenet_indexer module is disabled")
	}
	if appCtx.PGIndexStore == nil {
		appCtx.Logger.Fatal("%s", notConfiguredMessage)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	return appCtx, ctx, func() {
		stop()
		appCtx.Close()
	}
}

type taskRunnerFunc func(ctx context.Context) error

func (fn taskRunnerFunc) RunOnce(ctx context.Context) error {
	return fn(ctx)
}

func (r *Runner) runIndexerTaskScheduler(ctx context.Context, appCtx *app.Context, intervalMinutes float64, runOnce func(context.Context) error) error {
	interval := time.Duration(intervalMinutes * float64(time.Minute))
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	return scheduler.NewService(taskRunnerFunc(runOnce), appCtx.Logger, interval).Run(ctx)
}
