package commands

import (
	"context"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/datallboy/gonzb/internal/api"
	"github.com/datallboy/gonzb/internal/engine"
	"github.com/datallboy/gonzb/internal/runtime/wiring"
	"github.com/datallboy/gonzb/internal/telemetry"
	"github.com/labstack/echo/v5"
)

func (r *Runner) ExecuteServer() {
	r.ExecuteServerWithOptions(ServerOptions{})
}

func (r *Runner) ExecuteServerWithOptions(opts ServerOptions) {
	appCtx := r.setupApp(context.Background())
	e := echo.New()

	if err := wiring.ValidateRuntimeForServer(appCtx); err != nil {
		appCtx.Logger.Fatal("%v", err)
	}

	if appCtx.Config.Modules.Downloader.Enabled {
		appCtx.Queue = engine.NewQueueManager(appCtx, true)
	}
	wiring.BindApplicationModules(appCtx)

	// Server mode must stay reachable for the control plane even when
	// operational modules are enabled but not configured yet. /readyz still
	// reports module readiness for probes and operators.
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

	if err := telemetry.ValidateStartupReadiness(startupCtx, appCtx); err != nil {
		appCtx.Logger.Warn("%v", err)
	}
	if opts.DisableReleasePurgeArchivedSources {
		appCtx.DisableReleasePurgeArchivedSources = true
		appCtx.Logger.Info("server mode will not run release_purge_archived_sources stage")
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
	defer stop()

	startOpts := wiring.ServerStartOptions{
		SkipModuleStarts: map[string]bool{},
	}
	if opts.DisableIndexerSupervisor {
		startOpts.SkipModuleStarts["usenet_indexer"] = true
		appCtx.Logger.Info("server mode will not start the built-in usenet indexer supervisor")
	}

	api.RegisterRoutes(e, appCtx)

	srv := &http.Server{
		Addr:              httpListenAddress(appCtx.Config.BindAddress, appCtx.Config.Port),
		Handler:           e,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	appCtx.Logger.Info(
		"starting server addr=%s downloader=%t aggregator=%t usenet_indexer=%t api=%t web_ui=%t indexer_supervisor=%t",
		srv.Addr,
		appCtx.Config.Modules.Downloader.Enabled,
		appCtx.Config.Modules.Aggregator.Enabled,
		appCtx.Config.Modules.UsenetIndexer.Enabled,
		appCtx.Config.Modules.API.Enabled,
		appCtx.Config.Modules.WebUI.Enabled,
		!opts.DisableIndexerSupervisor,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	runtimeErrCh := make(chan error, 1)
	go func() {
		if err := wiring.StartServerBackgroundLoops(ctx, appCtx, startOpts); err != nil {
			runtimeErrCh <- err
		}
	}()

	shutdownServer := false
	select {
	case <-ctx.Done():
		appCtx.Logger.Info("shutdown signal received, stopping HTTP server")
		shutdownServer = true

	case err := <-runtimeErrCh:
		appCtx.Logger.Error("server background runtime failed to start: %v", err)
		shutdownServer = true

	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			appCtx.Logger.Error("server exited with error: %v", err)
		}
	}
	stop()
	if shutdownServer {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			appCtx.Logger.Error("graceful shutdown failed: %v", err)
		}
	}

	appCtx.Logger.Info("finalizing application resources")
	appCtx.Close()
	appCtx.Logger.Info("server shutdown complete")
}

func httpListenAddress(bindAddress, port string) string {
	bindAddress = strings.TrimSpace(bindAddress)
	if bindAddress == "" {
		return ":" + port
	}
	return net.JoinHostPort(bindAddress, port)
}
