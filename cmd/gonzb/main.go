package main

import (
	"fmt"
	"os"

	"github.com/datallboy/gonzb/internal/runtime/commands"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	nzbPath   string
	cfgFile   string = "config.yaml"

	serveWithoutIndexerSupervisor      bool
	disableReleasePurgeArchivedSources bool

	scrapeOnce      bool
	assembleOnce    bool
	recoverYEncOnce bool
	releaseOnce     bool
	releaseReform   bool
	releaseIDs      []string
	pipelineOnce    bool
	inspectOnce     bool
	enrichOnce      bool

	indexerReclaimFull                 bool
	indexerReclaimCheck                bool
	indexerMaintenanceTaskDryRun       bool
	indexerMaintenanceTaskBatchSize    int
	indexerIntegrityEnsureExtension    bool
	indexerCrosspostBackfillBatchSize  int
	indexerCrosspostBackfillMaxBatches int
	indexerPosterMaterializeBatchSize  int
	indexerCrosspostRefreshBatchSize   int
)

var rootCmd = &cobra.Command{
	Use:     "gonzb",
	Short:   "GONZB is a simple Usenet downloader",
	Long:    `A lightweight, concurrent NNTP downloaer written in Go.`,
	Version: Version,
	Run: func(cmd *cobra.Command, args []string) {
		if nzbPath == "" {
			fmt.Println("Error: --file or -f is required")
			cmd.Help()
			return
		}

		commands.New(cfgFile).ExecuteDownload(nzbPath)
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of GoNZB",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("GoNZB Version: %s\nBuild Time: %s\n", Version, BuildTime)
	},
}

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Starts GoNZB in server mode. Start HTTP server.",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteServerWithOptions(commands.ServerOptions{
			DisableIndexerSupervisor:           serveWithoutIndexerSupervisor,
			DisableReleasePurgeArchivedSources: disableReleasePurgeArchivedSources,
		})
	},
}

// gonzb indexer scrape --once
var indexerCmd = &cobra.Command{
	Use:   "indexer",
	Short: "Usenet/NZB indexer operations",
}

// compatibility command; defaults to latest.
var indexerScrapeCmd = &cobra.Command{
	Use:   "scrape",
	Short: "Scrape article headers into PostgreSQL (default mode: latest)",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerScrape(scrapeOnce)
	},
}

var indexerScrapeLatestCmd = &cobra.Command{
	Use:   "latest",
	Short: "Scrape the most recent article ranges first, then continue forward",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerScrapeLatest(scrapeOnce)
	},
}

var indexerScrapeBackfillCmd = &cobra.Command{
	Use:   "backfill",
	Short: "Scrape older article ranges continuously; use --once for a single backfill pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerScrapeBackfill(scrapeOnce)
	},
}

var indexerAssembleCmd = &cobra.Command{
	Use:   "assemble",
	Short: "Assemble binaries from queued article headers",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerAssemble(assembleOnce)
	},
}

var indexerReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Form releases continuously; use --once for a single pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerRelease(releaseOnce, releaseReform, releaseIDs)
	},
}

var indexerReleaseRefreshSummariesCmd = &cobra.Command{
	Use:   "refresh-summaries",
	Short: "Refresh deferred release readiness summaries continuously; use --once for a single pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerReleaseSummaryRefresh(releaseOnce)
	},
}

var indexerReleaseGenerateNZBCmd = &cobra.Command{
	Use:   "generate-nzb",
	Short: "Pre-generate NZBs for public-ready releases continuously; use --once for a single pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerReleaseGenerateNZB(releaseOnce)
	},
}

var indexerReleaseArchiveNZBCmd = &cobra.Command{
	Use:   "archive-nzb",
	Short: "Archive ready release NZBs continuously; use --once for a single pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerReleaseArchiveNZB(releaseOnce)
	},
}

var indexerReleasePurgeArchivedSourcesCmd = &cobra.Command{
	Use:   "purge-archived-sources",
	Short: "Purge source rows for archived releases continuously; use --once for a single pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerReleasePurgeArchivedSources(releaseOnce)
	},
}

var indexerRecoverYEncCmd = &cobra.Command{
	Use:   "recover-yenc",
	Short: "Recover obfuscated binary names from yEnc body headers; use --once for a single pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerRecoverYEnc(recoverYEncOnce)
	},
}

var indexerPipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run scrape, assemble, and release passes in sequence",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerPipeline(pipelineOnce)
	},
}

var indexerMaintenanceCmd = &cobra.Command{
	Use:   "maintenance",
	Short: "Run one indexer maintenance pass to clean stale runtime state and aged rows",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerMaintenance()
	},
}

var indexerMaintenanceRepairRuntimeCmd = &cobra.Command{
	Use:   "repair-runtime",
	Short: "Repair stale indexer stage leases and running stage rows",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerRepairRuntime()
	},
}

var indexerMaintenanceCheckIntegrityCmd = &cobra.Command{
	Use:   "check-integrity",
	Short: "Check critical PostgreSQL article-header indexes for corruption",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerCheckIntegrity(indexerIntegrityEnsureExtension)
	},
}

var indexerMaintenanceReindexCriticalCmd = &cobra.Command{
	Use:   "reindex-critical",
	Short: "Reindex the critical article-header indexes used by scrape ingest",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerReindexCritical()
	},
}

var indexerMaintenancePurgeHeaderPayloadsCmd = &cobra.Command{
	Use:   "purge-header-payloads",
	Short: "Disabled legacy article-header payload purge",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerPurgeHeaderPayloads()
	},
}

var indexerMaintenanceBackfillCrosspostGroupsCmd = &cobra.Command{
	Use:   "backfill-crosspost-groups",
	Short: "Backfill cross-post telemetry from existing article_header_ingest_payloads xref rows",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerBackfillCrosspostGroups(indexerCrosspostBackfillBatchSize, indexerCrosspostBackfillMaxBatches)
	},
}

var indexerMaintenanceMaterializePostersCmd = &cobra.Command{
	Use:   "materialize-posters",
	Short: "Materialize queued article-header posters into poster dimension rows",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerMaterializePosters(indexerPosterMaterializeBatchSize)
	},
}

var indexerMaintenanceRefreshCrosspostPopularityCmd = &cobra.Command{
	Use:   "refresh-crosspost-popularity",
	Short: "Refresh queued cross-post popularity summaries from raw Xref observations",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerRefreshCrosspostPopularity(indexerCrosspostRefreshBatchSize)
	},
}

var indexerReclaimStorageCmd = &cobra.Command{
	Use:   "reclaim-storage [table...]",
	Short: "Run allowlisted PostgreSQL vacuum maintenance for the growth-trim tables",
	Long: "Run allowlisted PostgreSQL vacuum maintenance for the growth-trim tables.\n" +
		"Without table arguments it uses the recommended order:\n" +
		"release_family_readiness_summaries, binary_grouping_evidence, article_header_ingest_payloads.\n" +
		"Use --check to inspect current table sizes without vacuuming.\n" +
		"Use --full only when you need bytes returned to the Docker volume and host filesystem.",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerStorageReclaim(args, indexerReclaimFull, indexerReclaimCheck)
	},
}

var indexerMaintenanceTaskCmd = &cobra.Command{
	Use:   "task <task-key>",
	Short: "Run or dry-run one supported indexer maintenance task",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerMaintenanceTask(args[0], indexerMaintenanceTaskDryRun, indexerMaintenanceTaskBatchSize)
	},
}

var indexerInspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Run indexer inspect submodules",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerInspect(inspectOnce)
	},
}

var indexerInspectDiscoveryCmd = &cobra.Command{
	Use:   "discovery",
	Short: "Run the opaque-file discovery inspection submodule once",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerInspectDiscovery(inspectOnce)
	},
}

var indexerInspectPAR2Cmd = &cobra.Command{
	Use:   "par2",
	Short: "Run the PAR2 inspection submodule once",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerInspectPAR2(inspectOnce)
	},
}

var indexerInspectNFOCmd = &cobra.Command{
	Use:   "nfo",
	Short: "Run the NFO inspection submodule once",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerInspectNFO(inspectOnce)
	},
}

var indexerInspectArchiveCmd = &cobra.Command{
	Use:   "archive",
	Short: "Run the archive inspection submodule once",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerInspectArchive(inspectOnce)
	},
}

var indexerInspectPasswordCmd = &cobra.Command{
	Use:   "password",
	Short: "Run the password inspection submodule once",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerInspectPassword(inspectOnce)
	},
}

var indexerInspectMediaCmd = &cobra.Command{
	Use:   "media",
	Short: "Run the media inspection submodule once",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerInspectMedia(inspectOnce)
	},
}

var indexerEnrichCmd = &cobra.Command{
	Use:   "enrich",
	Short: "Run indexer enrichment submodules",
}

var indexerEnrichPreDBCmd = &cobra.Command{
	Use:   "predb",
	Short: "Run PreDB enrichment workflows",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerEnrichPreDB(enrichOnce)
	},
}

var indexerEnrichPreDBSceneNameCmd = &cobra.Command{
	Use:   "scene-name-recovery",
	Short: "Recover scene-style release names using PreDB search",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerEnrichPreDBSceneNameRecovery(enrichOnce)
	},
}

var indexerEnrichPreDBMetadataFallbackCmd = &cobra.Command{
	Use:   "metadata-only-fallback",
	Short: "Use synced PreDB metadata as a local fallback for weakly identified releases",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerEnrichPreDBMetadataOnlyFallback(enrichOnce)
	},
}

var indexerEnrichPreDBSyncFeedCmd = &cobra.Command{
	Use:   "sync-feed",
	Short: "Sync recent PreDB feed entries into the local database",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerEnrichPreDBSyncFeed(enrichOnce)
	},
}

var indexerEnrichPreDBSyncBackfillCmd = &cobra.Command{
	Use:   "sync-backfill",
	Short: "Backfill historical PreDB entries into the local database for the indexed article window",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerEnrichPreDBSyncBackfill(enrichOnce)
	},
}

var indexerEnrichTMDBCmd = &cobra.Command{
	Use:   "tmdb",
	Short: "Run the TMDB enrichment submodule once",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerEnrichTMDB(enrichOnce)
	},
}

func init() {
	// Define flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file (default is ./config.yaml)")
	rootCmd.Flags().StringVarP(&nzbPath, "file", "f", "", "Path to the NZB file (required)")

	rootCmd.SetVersionTemplate(fmt.Sprintf("GoNZB Version: %s\nBuild Time: %s\n", Version, BuildTime))
	rootCmd.Flags().BoolP("version", "v", false, "display version information")
	serveCmd.Flags().BoolVar(&serveWithoutIndexerSupervisor, "no-indexer-supervisor", false, "serve API/UI without starting the built-in indexer supervisor")
	serveCmd.Flags().BoolVar(&disableReleasePurgeArchivedSources, "disable-release-purge-archived-sources", false, "disable the release_purge_archived_sources indexer stage")

	indexerScrapeCmd.Flags().BoolVar(&scrapeOnce, "once", false, "Run one scrape pass and exit")
	indexerScrapeLatestCmd.Flags().BoolVar(&scrapeOnce, "once", false, "Run one latest scrape pass and exit")
	indexerScrapeBackfillCmd.Flags().BoolVar(&scrapeOnce, "once", false, "Run one backfill scrape pass and exit instead of continuous backfill mode")

	indexerAssembleCmd.Flags().BoolVar(&assembleOnce, "once", false, "Run one assemble pass and exit instead of continuous mode")
	indexerRecoverYEncCmd.Flags().BoolVar(&recoverYEncOnce, "once", false, "Run one yEnc recovery pass and exit instead of continuous mode")
	indexerReleaseCmd.Flags().BoolVar(&releaseOnce, "once", false, "Run one release pass and exit instead of continuous mode")
	indexerReleaseCmd.Flags().BoolVar(&releaseReform, "reform", false, "Re-form existing releases from current binaries; requires --once")
	indexerReleaseCmd.Flags().StringArrayVar(&releaseIDs, "release-id", nil, "Limit --reform to a specific release id; repeatable")
	indexerReleaseRefreshSummariesCmd.Flags().BoolVar(&releaseOnce, "once", false, "Run one release summary refresh pass and exit instead of continuous mode")
	indexerReleaseGenerateNZBCmd.Flags().BoolVar(&releaseOnce, "once", false, "Run one NZB pre-generation pass and exit instead of continuous mode")
	indexerReleaseArchiveNZBCmd.Flags().BoolVar(&releaseOnce, "once", false, "Run one archive NZB pass and exit instead of continuous mode")
	indexerReleasePurgeArchivedSourcesCmd.Flags().BoolVar(&releaseOnce, "once", false, "Run one archived-source purge pass and exit instead of continuous mode")
	indexerPipelineCmd.Flags().BoolVar(&pipelineOnce, "once", false, "Run one full pipeline pass and exit")
	indexerInspectCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run all inspect submodules once and exit")
	indexerInspectDiscoveryCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one discovery inspection pass and exit")
	indexerInspectPAR2Cmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one PAR2 inspection pass and exit")
	indexerInspectNFOCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one NFO inspection pass and exit")
	indexerInspectArchiveCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one archive inspection pass and exit")
	indexerInspectPasswordCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one password inspection pass and exit")
	indexerInspectMediaCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one media inspection pass and exit")
	indexerEnrichPreDBCmd.Flags().BoolVar(&enrichOnce, "once", false, "Run one PreDB enrichment pass and exit")
	indexerEnrichPreDBSceneNameCmd.Flags().BoolVar(&enrichOnce, "once", false, "Run one scene-name recovery pass and exit")
	indexerEnrichPreDBMetadataFallbackCmd.Flags().BoolVar(&enrichOnce, "once", false, "Run one metadata-only fallback pass and exit")
	indexerEnrichPreDBSyncFeedCmd.Flags().BoolVar(&enrichOnce, "once", false, "Run one PreDB feed sync pass and exit")
	indexerEnrichPreDBSyncBackfillCmd.Flags().BoolVar(&enrichOnce, "once", false, "Run one PreDB backfill sync pass and exit")
	indexerEnrichTMDBCmd.Flags().BoolVar(&enrichOnce, "once", false, "Run one TMDB enrichment pass and exit")
	indexerReclaimStorageCmd.Flags().BoolVar(&indexerReclaimCheck, "check", false, "Report current bytes for the allowlisted reclaim tables without running VACUUM")
	indexerReclaimStorageCmd.Flags().BoolVar(&indexerReclaimFull, "full", false, "Use VACUUM FULL instead of VACUUM ANALYZE; requires enough free disk and exclusive table locks")
	indexerMaintenanceTaskCmd.Flags().BoolVar(&indexerMaintenanceTaskDryRun, "dry-run", true, "Estimate the task without committing deletes")
	indexerMaintenanceTaskCmd.Flags().IntVar(&indexerMaintenanceTaskBatchSize, "batch-size", 1000, "Maximum rows or units processed by the task")
	indexerMaintenanceCheckIntegrityCmd.Flags().BoolVar(&indexerIntegrityEnsureExtension, "ensure-extension", false, "Install the amcheck extension before running the integrity check")
	indexerMaintenanceBackfillCrosspostGroupsCmd.Flags().IntVar(&indexerCrosspostBackfillBatchSize, "batch-size", 5000, "Number of article headers to process per batch")
	indexerMaintenanceBackfillCrosspostGroupsCmd.Flags().IntVar(&indexerCrosspostBackfillMaxBatches, "max-batches", 1, "Maximum number of backfill batches to process in one run")
	indexerMaintenanceMaterializePostersCmd.Flags().IntVar(&indexerPosterMaterializeBatchSize, "batch-size", 10000, "Maximum queued poster rows to materialize")
	indexerMaintenanceRefreshCrosspostPopularityCmd.Flags().IntVar(&indexerCrosspostRefreshBatchSize, "batch-size", 1000, "Maximum queued observed cross-post groups to refresh")

	indexerCmd.AddCommand(indexerScrapeCmd)
	indexerScrapeCmd.AddCommand(indexerScrapeLatestCmd)
	indexerScrapeCmd.AddCommand(indexerScrapeBackfillCmd)

	indexerCmd.AddCommand(indexerAssembleCmd)
	indexerCmd.AddCommand(indexerRecoverYEncCmd)
	indexerCmd.AddCommand(indexerReleaseCmd)
	indexerReleaseCmd.AddCommand(indexerReleaseRefreshSummariesCmd)
	indexerReleaseCmd.AddCommand(indexerReleaseGenerateNZBCmd)
	indexerReleaseCmd.AddCommand(indexerReleaseArchiveNZBCmd)
	indexerReleaseCmd.AddCommand(indexerReleasePurgeArchivedSourcesCmd)
	indexerCmd.AddCommand(indexerPipelineCmd)
	indexerCmd.AddCommand(indexerMaintenanceCmd)
	indexerMaintenanceCmd.AddCommand(indexerMaintenanceRepairRuntimeCmd)
	indexerMaintenanceCmd.AddCommand(indexerMaintenanceCheckIntegrityCmd)
	indexerMaintenanceCmd.AddCommand(indexerMaintenanceReindexCriticalCmd)
	indexerMaintenanceCmd.AddCommand(indexerMaintenancePurgeHeaderPayloadsCmd)
	indexerMaintenanceCmd.AddCommand(indexerMaintenanceBackfillCrosspostGroupsCmd)
	indexerMaintenanceCmd.AddCommand(indexerMaintenanceMaterializePostersCmd)
	indexerMaintenanceCmd.AddCommand(indexerMaintenanceRefreshCrosspostPopularityCmd)
	indexerMaintenanceCmd.AddCommand(indexerMaintenanceTaskCmd)
	indexerMaintenanceCmd.AddCommand(indexerReclaimStorageCmd)
	indexerCmd.AddCommand(indexerInspectCmd)
	indexerInspectCmd.AddCommand(indexerInspectDiscoveryCmd)
	indexerInspectCmd.AddCommand(indexerInspectPAR2Cmd)
	indexerInspectCmd.AddCommand(indexerInspectNFOCmd)
	indexerInspectCmd.AddCommand(indexerInspectArchiveCmd)
	indexerInspectCmd.AddCommand(indexerInspectPasswordCmd)
	indexerInspectCmd.AddCommand(indexerInspectMediaCmd)
	indexerCmd.AddCommand(indexerEnrichCmd)
	indexerEnrichCmd.AddCommand(indexerEnrichPreDBCmd)
	indexerEnrichPreDBCmd.AddCommand(indexerEnrichPreDBSceneNameCmd)
	indexerEnrichPreDBCmd.AddCommand(indexerEnrichPreDBMetadataFallbackCmd)
	indexerEnrichPreDBCmd.AddCommand(indexerEnrichPreDBSyncFeedCmd)
	indexerEnrichPreDBCmd.AddCommand(indexerEnrichPreDBSyncBackfillCmd)
	indexerEnrichCmd.AddCommand(indexerEnrichTMDBCmd)
}

func main() {

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(indexerCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
