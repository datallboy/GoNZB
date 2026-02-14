package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/datallboy/gonzb/internal/api"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/infra/platform"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/labstack/echo/v5"

	"github.com/datallboy/gonzb/internal/engine"

	"github.com/datallboy/gonzb/internal/nntp"

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

func setupApp() *app.Context {
	// 1. Initialize  application context
	// Load core app infrastructure (dependency check, config & logger)
	if err := platform.ValidateDependencies(); err != nil {
		log.Fatalf("Missing dependencies. Please check your Dockerfile or local installation: %v", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		log.Fatalf("Config error: %v", err)
	}

	appLogger, err := logger.New(cfg.Log.Path, logger.ParseLevel(cfg.Log.Level), cfg.Log.IncludeStdout)
	if err != nil {
		log.Fatalf("Could not initialize logger %v\n", err)
	}

	if cfg.Log.Level == "debug" {
		appLogger.Debug("Debug logging enabled")
	}

	// Initialize app context
	appCtx, err := app.NewContext(cfg, appLogger)
	if err != nil {
		appLogger.Fatal("Failed to initialize application context %v", err)
	}

	// Initialize shared writer
	writer := engine.NewFileWriter()

	// Initialize the NNTP Manager (The provider load balancer)
	appCtx.NNTP, err = nntp.NewManager(appCtx)
	if err != nil {
		appLogger.Fatal("Provider initializiation failed: %v", err)
	}

	appCtx.Processor = processor.New(appCtx, writer)
	appCtx.Downloader = engine.NewDownloader(appCtx, writer)
	appCtx.NZBParser = nzb.NewParser()

	return appCtx
}

func executeServer() {
	appCtx := setupApp()
	e := echo.New()

	appCtx.Queue = engine.NewQueueManager(appCtx, true)
	// Create a "Global" context that we can cancel to trigger shutdown
	// This context manages the entire application lifecycle
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start background worker for the queue
	go appCtx.Queue.Start(ctx)

	// Register routes via the router
	api.RegisterRoutes(e, appCtx)

	sc := echo.StartConfig{
		Address:         ":" + appCtx.Config.Port,
		GracefulTimeout: 10 * time.Second,
		HidePort:        true,
		HideBanner:      true,
	}
	appCtx.Logger.Info("GoNZB listening on port %s...", appCtx.Config.Port)

	if err := sc.Start(ctx, e); err != nil && err != http.ErrServerClosed {
		appCtx.Logger.Error("failed to start server %v", err)
	}

	appCtx.Logger.Info("Server stopped. Finalizing store...")
	appCtx.Close()

	appCtx.Logger.Info("GoNZB shutdown gracefully")
}

func executeDownload() {
	appCtx := setupApp()
	filename := filepath.Base(nzbPath)
	setupCtx := context.Background()

	// Open source file
	nzbFile, err := os.Open(nzbPath)
	if err != nil {
		appCtx.Logger.Fatal("Failed to read NZB file: %v", err)
	}
	defer nzbFile.Close()

	// Generate ID based on file contents
	releaseID, err := domain.CalculateFileHash(nzbFile)
	if err != nil {
		appCtx.Logger.Fatal("Hashing failed: %v", err)
	}

	// Reset file pointer to the beginning so we can read it again for the copy
	if _, err := nzbFile.Seek(0, 0); err != nil {
		appCtx.Logger.Fatal("Failed to reset NZB file pointer: %v", err)
	}

	// Minimal seed: just metadata and the blob
	release := &domain.Release{
		ID:       releaseID,
		FileHash: releaseID,
		GUID:     releaseID,
		Title:    filename,
		Source:   "manual",
		Category: "Uncategorized",
	}

	// Save NZB bits to Blob Store
	// This "primes" the cache so GetNZB doesn't try to call a web indexer
	writer, err := appCtx.Store.CreateNZBWriter(releaseID)
	if err != nil {
		appCtx.Logger.Fatal("Failed to initialize blob writer: %v", err)
	}

	_, err = io.Copy(writer, nzbFile)
	if err != nil {
		writer.Close()
		appCtx.Logger.Fatal("Failed to copy NZB to blob store: %v", err)
	}

	if err := writer.Close(); err != nil {
		appCtx.Logger.Fatal("Failed to finalize NZB file on disk: %v", err)
	}

	// Initialize the queue manager (false = don't load pending queue items from db)
	appCtx.Queue = engine.NewQueueManager(appCtx, false)
	item, err := appCtx.Queue.Add(setupCtx, releaseID, filename)
	if err != nil {
		appCtx.Logger.Fatal("Failed to queue NZB: %v", err)
	}

	item.Release = release

	// Manually call Hydrate
	if err := appCtx.Queue.HydrateItem(setupCtx, item); err != nil {
		appCtx.Logger.Fatal("Hydration failed: %v", err)
	}

	appCtx.Queue.UpdateStatus(setupCtx, item, domain.StatusDownloading)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	appCtx.Downloader.SetProgressHandler(func(item *domain.QueueItem) {
		// This runs exactly when Download() finishes, inside the same goroutine.
		// It "seals" the line before the status ever changes to Processing.
		appCtx.Downloader.RenderCLIProgress(item, 0, true)
		fmt.Println()
	})

	// Start the queue
	go appCtx.Queue.Start(ctx)

	// Blocking wait loop for the CLI
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastBytes int64
	var barRendered = false

	for {
		select {
		case <-ctx.Done():
			// This triggers if the user hits Ctrl+C
			ticker.Stop()
			fmt.Print("\n")
			appCtx.Logger.Info("Cancellation received. Cleaning up...")
			return

		case <-ticker.C:
			// Check if the manager has finished the job
			itm, ok := appCtx.Queue.GetItem(ctx, item.ID)

			if !ok {
				continue
			}

			switch itm.Status {
			case domain.StatusDownloading:
				current := itm.BytesWritten.Load()
				delta := current - lastBytes
				lastBytes = current

				// Will print a newline on first call to RenderCLIProgress
				if !barRendered {
					fmt.Print("\n\n")
				}

				fmt.Printf("\r [DEBUG] Raw Bytes: %d | Delta: %d ", current, delta)

				// Calculate instantaneous speed
				speedMbps := float64(delta) * 8 / (1024 * 1024 * 0.5)

				appCtx.Downloader.RenderCLIProgress(itm, speedMbps, false)
				barRendered = true

			case domain.StatusProcessing:
				if barRendered {
					barRendered = false
				}

			case domain.StatusCompleted, domain.StatusFailed:
				if itm.Status == domain.StatusFailed {
					errText := "Unknown error"
					if itm.Error != nil {
						errText = *itm.Error
					}
					appCtx.Logger.Error("Download failed: %s", errText)
				} else {
					appCtx.Logger.Info("Download completed successfully!")
				}
				stop()
				return
			}
		}
	}
}

func main() {

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(serveCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
