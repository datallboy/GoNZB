package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/datallboy/gonzb/internal/api"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/infra/platform"
	"github.com/labstack/echo/v5"

	"github.com/datallboy/gonzb/internal/engine"

	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/nzb"

	"github.com/datallboy/gonzb/internal/processor"

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

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Starts GoNZB in server mode. Start HTTP server.",
	Run: func(cmd *cobra.Command, args []string) {
		executeServer()
	},
}

func init() {
	// Define flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file (default is ./config.yaml)")
	rootCmd.Flags().StringVarP(&nzbPath, "file", "f", "", "Path to the NZB file (required)")

	rootCmd.SetVersionTemplate(fmt.Sprintf("GoNZB Version: %s\nBuild Time: %s\n", Version, BuildTime))
	rootCmd.Flags().BoolP("version", "v", false, "display version information")
}

func executeServer() {
	e := echo.New()

	// 1. Initialize  application context
	// Load core app infrastructure (dependency check, config & logger)
	if err := platform.ValidateDependencies(); err != nil {
		log.Fatalf("FATAL: %v. Please check your Dockerfile or local installation.", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	appLogger, err := logger.New(cfg.Log.Path, logger.ParseLevel(cfg.Log.Level), cfg.Log.IncludeStdout)
	if err != nil {
		log.Fatalf("Fatal: Could not initialize logger %v\n", err)
	}

	if cfg.Log.Level == "debug" {
		appLogger.Debug("Debug logging enabled")
	}

	// Initialize app context
	appCtx, err := app.NewContext(cfg, appLogger)
	if err != nil {
		appLogger.Fatal("Failed to initialize application context %v", err)
	}

	// Register routes via the router
	api.RegisterRoutes(e, appCtx)

	// Create a "Global" context that we can cancel to trigger shutdown
	// This context manages the entire application lifecycle
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sc := echo.StartConfig{
		Address:         ":" + cfg.Port,
		GracefulTimeout: 10 * time.Second,
	}

	appLogger.Info("GoNZB starting up on port %s", cfg.Port)

	if err := sc.Start(ctx, e); err != nil && err != http.ErrServerClosed {
		appLogger.Error("failed to start server %v", err)
	}

	appLogger.Info("Server stopped. Finalizing store...")
	appCtx.Close()

	appLogger.Info("GoNZB shutdown gracefully")
}

func executeDownload() {
	// Load core app infrastructure (dependency check, config & logger)
	if err := platform.ValidateDependencies(); err != nil {
		log.Fatalf("FATAL: %v. Please check your Dockerfile or local installation.", err)
	}

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

	// Initialize app context
	appCtx, err := app.NewContext(cfg, appLogger)
	if err != nil {
		log.Fatalf("Failed to initialize application context %v\n", err)
	}

	// Initialize shared writer
	writer := engine.NewFileWriter()

	// Initialize the Manager (The provider load balancer)
	appCtx.NNTP, err = nntp.NewManager(appCtx)
	if err != nil {
		appLogger.Fatal("Provider initializiation failed: %v", err)
	}

	appCtx.Processor = processor.New(appCtx, writer)

	// Initialize nzb parser
	nzbParser := nzb.NewParser()
	nzbDomain, err := nzbParser.ParseFile(nzbPath)
	if err != nil {
		appLogger.Fatal("Failed to parse NZB: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Initialize the Downloader Service
	svc := engine.NewService(appCtx, writer)
	filename := filepath.Base(nzbPath)

	if err := svc.Download(ctx, nzbDomain, filename); err != nil {
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
	rootCmd.AddCommand(serveCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
