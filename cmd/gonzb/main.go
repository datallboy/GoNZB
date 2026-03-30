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

	scrapeOnce   bool
	assembleOnce bool
	releaseOnce  bool
	pipelineOnce bool
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
	Short: "Scrape older article ranges by walking backward from the latest frontier",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerScrapeBackfill(scrapeOnce)
	},
}

var indexerAssembleCmd = &cobra.Command{
	Use:   "assemble",
	Short: "Assemble binaries and parts from article headers",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerAssemble(assembleOnce)
	},
}

var indexerReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Form release catalog rows from assembled binaries",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerRelease(releaseOnce)
	},
}

var indexerPipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run scrape, assemble, and release passes in sequence",
	Run: func(cmd *cobra.Command, args []string) {
		commands.New(cfgFile).ExecuteIndexerPipeline(pipelineOnce)
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
	indexerScrapeBackfillCmd.Flags().BoolVar(&scrapeOnce, "once", false, "Run one backfill scrape pass and exit")

	indexerAssembleCmd.Flags().BoolVar(&assembleOnce, "once", false, "Run one assemble pass and exit")
	indexerReleaseCmd.Flags().BoolVar(&releaseOnce, "once", false, "Run one release pass and exit")
	indexerPipelineCmd.Flags().BoolVar(&pipelineOnce, "once", false, "Run one full pipeline pass and exit")

	indexerCmd.AddCommand(indexerScrapeCmd)
	indexerScrapeCmd.AddCommand(indexerScrapeLatestCmd)
	indexerScrapeCmd.AddCommand(indexerScrapeBackfillCmd)

	indexerCmd.AddCommand(indexerAssembleCmd)
	indexerCmd.AddCommand(indexerReleaseCmd)
	indexerCmd.AddCommand(indexerPipelineCmd)
}

func main() {

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(indexerCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
