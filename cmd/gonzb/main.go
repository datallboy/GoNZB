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

	scrapeOnce    bool
	assembleOnce  bool
	releaseOnce   bool
	releaseReform bool
	pipelineOnce  bool
	inspectOnce   bool
	enrichOnce    bool
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
		commands.New(cfgFile).ExecuteServer()
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
	Short: "Assemble binaries and parts continuously; use --once for a single pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerAssemble(assembleOnce)
	},
}

var indexerReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Form releases continuously; use --once for a single pass",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerRelease(releaseOnce, releaseReform)
	},
}

var indexerPipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run scrape, assemble, and release passes in sequence",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerPipeline(pipelineOnce)
	},
}

var indexerInspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Run indexer inspect submodules",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerInspect(inspectOnce)
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
	Short: "Run the PreDB enrichment submodule once",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerEnrichPreDB(enrichOnce)
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

	indexerScrapeCmd.Flags().BoolVar(&scrapeOnce, "once", false, "Run one scrape pass and exit")
	indexerScrapeLatestCmd.Flags().BoolVar(&scrapeOnce, "once", false, "Run one latest scrape pass and exit")
	indexerScrapeBackfillCmd.Flags().BoolVar(&scrapeOnce, "once", false, "Run one backfill scrape pass and exit instead of continuous backfill mode")

	indexerAssembleCmd.Flags().BoolVar(&assembleOnce, "once", false, "Run one assemble pass and exit instead of continuous mode")
	indexerReleaseCmd.Flags().BoolVar(&releaseOnce, "once", false, "Run one release pass and exit instead of continuous mode")
	indexerReleaseCmd.Flags().BoolVar(&releaseReform, "reform", false, "Re-form existing releases from current binaries; requires --once")
	indexerPipelineCmd.Flags().BoolVar(&pipelineOnce, "once", false, "Run one full pipeline pass and exit")
	indexerInspectCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run all inspect submodules once and exit")
	indexerInspectPAR2Cmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one PAR2 inspection pass and exit")
	indexerInspectNFOCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one NFO inspection pass and exit")
	indexerInspectArchiveCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one archive inspection pass and exit")
	indexerInspectPasswordCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one password inspection pass and exit")
	indexerInspectMediaCmd.Flags().BoolVar(&inspectOnce, "once", false, "Run one media inspection pass and exit")
	indexerEnrichPreDBCmd.Flags().BoolVar(&enrichOnce, "once", false, "Run one PreDB enrichment pass and exit")
	indexerEnrichTMDBCmd.Flags().BoolVar(&enrichOnce, "once", false, "Run one TMDB enrichment pass and exit")

	indexerCmd.AddCommand(indexerScrapeCmd)
	indexerScrapeCmd.AddCommand(indexerScrapeLatestCmd)
	indexerScrapeCmd.AddCommand(indexerScrapeBackfillCmd)

	indexerCmd.AddCommand(indexerAssembleCmd)
	indexerCmd.AddCommand(indexerReleaseCmd)
	indexerCmd.AddCommand(indexerPipelineCmd)
	indexerCmd.AddCommand(indexerInspectCmd)
	indexerInspectCmd.AddCommand(indexerInspectPAR2Cmd)
	indexerInspectCmd.AddCommand(indexerInspectNFOCmd)
	indexerInspectCmd.AddCommand(indexerInspectArchiveCmd)
	indexerInspectCmd.AddCommand(indexerInspectPasswordCmd)
	indexerInspectCmd.AddCommand(indexerInspectMediaCmd)
	indexerCmd.AddCommand(indexerEnrichCmd)
	indexerEnrichCmd.AddCommand(indexerEnrichPreDBCmd)
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
