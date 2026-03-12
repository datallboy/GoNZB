package main

import (
	"context"
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
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/indexing"
	"github.com/datallboy/gonzb/internal/indexing/assemble"
	"github.com/datallboy/gonzb/internal/indexing/match"
	"github.com/datallboy/gonzb/internal/indexing/release"
	"github.com/datallboy/gonzb/internal/indexing/scheduler"
	"github.com/datallboy/gonzb/internal/indexing/scrape"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/infra/logger"
	"github.com/datallboy/gonzb/internal/infra/platform"
	"github.com/datallboy/gonzb/internal/nzb"
	queuesvc "github.com/datallboy/gonzb/internal/queue"
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

// gonzb indexer scrape --once
var indexerCmd = &cobra.Command{
	Use:   "indexer",
	Short: "Usenet/NZB indexer operations",
}

var indexerScrapeCmd = &cobra.Command{
	Use:   "scrape",
	Short: "Scrape article headers into PostgreSQL",
	Run: func(cmd *cobra.Command, args []string) {
		executeIndexerScrape(scrapeOnce)
	},
}

var indexerAssembleCmd = &cobra.Command{
	Use:   "assemble",
	Short: "Assemble binaries and parts from article headers",
	Run: func(cmd *cobra.Command, args []string) {
		executeIndexerAssemble(assembleOnce)
	},
}

var indexerReleaseCmd = &cobra.Command{
	Use:   "release",
	Short: "Form release catalog rows from assembled binaries",
	Run: func(cmd *cobra.Command, args []string) {
		executeIndexerRelease(releaseOnce)
	},
}

var indexerPipelineCmd = &cobra.Command{
	Use:   "pipeline",
	Short: "Run scrape, assemble, and release passes in sequence",
	Run: func(cmd *cobra.Command, args []string) {
		executeIndexerPipeline(pipelineOnce)
	},
}

func init() {
	// Define flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "config.yaml", "config file (default is ./config.yaml)")
	rootCmd.Flags().StringVarP(&nzbPath, "file", "f", "", "Path to the NZB file (required)")

	rootCmd.SetVersionTemplate(fmt.Sprintf("GoNZB Version: %s\nBuild Time: %s\n", Version, BuildTime))
	rootCmd.Flags().BoolP("version", "v", false, "display version information")

	indexerScrapeCmd.Flags().BoolVar(&scrapeOnce, "once", false, "Run one scrape pass and exit")
	indexerAssembleCmd.Flags().BoolVar(&assembleOnce, "once", false, "Run one assemble pass and exit")
	indexerReleaseCmd.Flags().BoolVar(&releaseOnce, "once", false, "Run one release pass and exit")
	indexerPipelineCmd.Flags().BoolVar(&pipelineOnce, "once", false, "Run one full pipeline pass and exit")

	indexerCmd.AddCommand(indexerScrapeCmd)
	indexerCmd.AddCommand(indexerAssembleCmd)
	indexerCmd.AddCommand(indexerReleaseCmd)
	indexerCmd.AddCommand(indexerPipelineCmd)
}

func setupApp() *app.Context {
	cfg, appLogger := loadRuntimeConfig()

	// downloader-only external dependency validation should not block
	// aggregator-only or usenet-indexer-only startup.
	if cfg.Modules.Downloader.Enabled {
		if err := platform.ValidateDependencies(); err != nil {
			log.Fatalf("Missing dependencies. Please check your Dockerfile or local installation: %v", err)
		}
	}

	// Initialize app context
	appCtx, err := app.NewContext(cfg, appLogger)
	if err != nil {
		appLogger.Fatal("Failed to initialize application context %v", err)
	}

	// downloader runtime pieces are only built when downloader is enabled.
	if cfg.Modules.Downloader.Enabled {
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
	}

	// wire optional Usenet/NZB Indexer runtime through app context.
	// Uses first configured provider for scrape foundation milestone.
	if cfg.Modules.UsenetIndexer.Enabled {
		if appCtx.PGIndexStore == nil {
			appLogger.Fatal("Usenet/NZB Indexer is enabled but PGIndexStore is not initialized")
		}

		matcherSvc := match.NewService()
		assembleSvc := assemble.NewService(
			appCtx.PGIndexStore,
			matcherSvc,
			appLogger,
			assemble.Options{
				BatchSize: int(cfg.Indexing.ScrapeBatchSize),
			},
		)

		releaseSvc := release.NewService(
			appCtx.PGIndexStore,
			appLogger,
			release.Options{
				BatchSize: 1000,
			},
		)

		var scrapeSvc *scrape.Service
		var schedulerSvc *scheduler.Service

		if len(cfg.Indexing.Newsgroups) > 0 {
			scrapeProvider := nntp.NewNNTPProvider(cfg.Servers[0])

			if err := scrapeProvider.TestConnection(); err != nil {
				appLogger.Fatal("Scrape provider initialization failed: %v", err)
			}
			appCtx.AddCloser(scrapeProvider)

			scrapeAdapter := scrape.NewNNTPAdapter(scrapeProvider)
			scrapeSvc = scrape.NewService(
				appCtx.PGIndexStore,
				scrapeAdapter,
				appLogger,
				scrape.Options{
					Newsgroups: cfg.Indexing.Newsgroups,
					BatchSize:  cfg.Indexing.ScrapeBatchSize,
				},
			)

			interval := time.Duration(cfg.Indexing.ScheduleIntervalMinutes) * time.Minute
			schedulerSvc = scheduler.NewService(scrapeSvc, appLogger, interval)
		}

		appCtx.UsenetIndexer = indexing.NewService(scrapeSvc, assembleSvc, releaseSvc, schedulerSvc)

	}

	return appCtx
}

// central config/logger load so command handlers can validate module mode cleanly.
func loadRuntimeConfig() (*config.Config, *logger.Logger) {
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

	return cfg, appLogger
}

func executeServer() {
	appCtx := setupApp()
	e := echo.New()

	// serve should expose an HTTP surface, otherwise it is meaningless.
	if !appCtx.Config.Modules.API.Enabled && !appCtx.Config.Modules.WebUI.Enabled {
		appCtx.Logger.Fatal("serve requires modules.api.enabled or modules.web_ui.enabled")
	}

	// downloader queue runtime only exists when downloader is enabled.
	if appCtx.Config.Modules.Downloader.Enabled {
		appCtx.Queue = engine.NewQueueManager(appCtx, true)
	}

	// Create a "Global" context that we can cancel to trigger shutdown
	// This context manages the entire application lifecycle
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Start background worker for the queue
	if appCtx.Queue != nil {
		go appCtx.Queue.Start(ctx)
	}

	// Register routes via the router
	// router internally gates downloader/aggreagator/web UI ownership
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
	defer appCtx.Close()

	// CHANGED: one-shot download command is downloader-owned.
	if !appCtx.Config.Modules.Downloader.Enabled {
		appCtx.Logger.Fatal("downloader module is disabled")
	}
	if appCtx.NNTP == nil || appCtx.Processor == nil || appCtx.Downloader == nil || appCtx.NZBParser == nil {
		appCtx.Logger.Fatal("downloader runtime is not initialized")
	}

	// Initialize queue manager (false = don't load pending queue items from db for one-shot CLI mode)
	appCtx.Queue = engine.NewQueueManager(appCtx, false)
	queueService := queuesvc.NewService(appCtx)

	filename := filepath.Base(nzbPath)
	setupCtx := context.Background()

	// Open source file
	nzbFile, err := os.Open(nzbPath)
	if err != nil {
		appCtx.Logger.Fatal("Failed to read NZB file: %v", err)
	}
	defer nzbFile.Close()

	item, err := queueService.EnqueueNZB(setupCtx, filename, nzbFile)
	if err != nil {
		appCtx.Logger.Fatal("Failed to queue NZB: %v", err)
	}

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

// indexer scrape command executor.
func executeIndexerScrape(once bool) {
	appCtx := setupApp()
	defer appCtx.Close()

	// indexer commands are owned by the Usenet/NZB Indexer module.
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

	interval := time.Duration(appCtx.Config.Indexing.ScheduleIntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 10 * time.Minute
	}

	if err := appCtx.UsenetIndexer.Start(ctx, interval); err != nil {
		appCtx.Logger.Fatal("indexer scheduler failed: %v", err)
	}
}

// one-shot assembly executor.
func executeIndexerAssemble(once bool) {
	appCtx := setupApp()
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

// one-shot release executor.
func executeIndexerRelease(once bool) {
	appCtx := setupApp()
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

// one-shot full pipeline executor.
func executeIndexerPipeline(once bool) {
	appCtx := setupApp()
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

func main() {

	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(serveCmd)
	rootCmd.AddCommand(indexerCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
