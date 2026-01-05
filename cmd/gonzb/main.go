package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/datallboy/gonzb/internal/config"
	"github.com/datallboy/gonzb/internal/downloader"
	"github.com/datallboy/gonzb/internal/logger"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/provider"

	"github.com/spf13/cobra"
)

var (
	Version   = "dev"
	BuildTime = "unknown"
	nzbPath   string
	cfgFile   string = "config.yaml"
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

		executeDownload()
	},
}

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of GoNZB",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("GoNZB Version: %s\nBuild Time: %s\n", Version, BuildTime)
	},
}

func init() {
	// Define flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file (default is ./config.yaml)")
	rootCmd.Flags().StringVarP(&nzbPath, "file", "f", "", "Path to the NZB file (required)")

	rootCmd.SetVersionTemplate(fmt.Sprintf("GoNZB Version: %s\nBuild Time: %s\n", Version, BuildTime))
	rootCmd.Flags().BoolP("version", "v", false, "display version information")
}

func executeDownload() {
	// Load core app infrastructure (config & logger)
	cfg, err := config.Load(cfgFile)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	appLogger, err := logger.New(cfg.Log.Path, logger.ParseLevel(cfg.Log.Level), cfg.Log.IncludeStdout)
	if err != nil {
		log.Fatalf("Fatal: Could not initialize logger %v\n", err)
	}
	appLogger.Info("GONZB starting up...")

	if cfg.Log.Level == "debug" {
		appLogger.Debug("Debug logging enabled")
	}

	// Initialize the Manager (The provider load balancer)
	mgr, err := provider.NewManager(cfg.Servers, appLogger)
	if err != nil {
		appLogger.Fatal("Provider initializiation failed: %v", err)
	}

	// Initialize nzb parser
	nzbParser := nzb.NewParser()
	nzbDomain, err := nzbParser.ParseFile(nzbPath)
	if err != nil {
		appLogger.Fatal("Failed to parse NZB: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Initialize the Downloader Service
	svc := downloader.NewService(cfg, mgr, appLogger)

	if err := svc.Download(ctx, nzbDomain); err != nil {
		if errors.Is(err, context.Canceled) {
			appLogger.Info("Download cancelled by user.")
			return
		} else {
			appLogger.Error("Download failed: %v", err)
			return
		}
	}

	appLogger.Info("Download completed successfully!")

}

func main() {

	rootCmd.AddCommand(versionCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
