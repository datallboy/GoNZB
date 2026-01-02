package main

import (
	"context"
	"errors"
	"fmt"
	"gonzb/internal/config"
	"gonzb/internal/downloader"
	"gonzb/internal/logger"
	"gonzb/internal/nzb"
	"gonzb/internal/provider"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

var (
	nzbPath string
	cfgFile string = "config.yaml"
)

var rootCmd = &cobra.Command{
	Use:   "gonzb",
	Short: "GONZB is a simple Usenet downloader",
	Long:  `A lightweight, concurrent NNTP downloaer written in Go.`,
	Run: func(cmd *cobra.Command, args []string) {
		if nzbPath == "" {
			fmt.Println("Error: --file or -f is required")
			cmd.Help()
			return
		}

		executeDownload()
	},
}

func init() {
	// Define flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file (default is ./config.yaml)")
	rootCmd.Flags().StringVarP(&nzbPath, "file", "f", "", "Path to the NZB file (required)")
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
		} else {
			appLogger.Error("Download failed: %v", err)
		}
	}

	appLogger.Info("Download completed successfully!")

}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
