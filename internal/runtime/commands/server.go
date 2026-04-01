package commands

import (
	"context"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/datallboy/gonzb/internal/api"
	"github.com/datallboy/gonzb/internal/engine"
	"github.com/datallboy/gonzb/internal/runtime/wiring"
	"github.com/datallboy/gonzb/internal/telemetry"
	"github.com/labstack/echo/v5"
)

func (r *Runner) ExecuteServer() {
	appCtx := r.setupApp(context.Background())
	e := echo.New()

	if err := wiring.ValidateRuntimeForServer(appCtx); err != nil {
		appCtx.Logger.Fatal("%v", err)
	}

	if appCtx.Config.Modules.Downloader.Enabled {
		appCtx.Queue = engine.NewQueueManager(appCtx, true)
	}
	wiring.BindApplicationModules(appCtx)

	// fail startup before serving traffic if enabled modules
	// are already not ready or have schema/dependency issues.
	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()

	if err := telemetry.ValidateStartupReadiness(startupCtx, appCtx); err != nil {
		appCtx.Logger.Fatal("%v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := wiring.StartServerBackgroundLoops(ctx, appCtx); err != nil {
		appCtx.Logger.Fatal("%v", err)
	}

	api.RegisterRoutes(e, appCtx)

	srv := &http.Server{
		Addr:              ":" + appCtx.Config.Port,
		Handler:           e,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      10 * time.Minute,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}

	appCtx.Logger.Info(
		"starting server addr=%s downloader=%t aggregator=%t usenet_indexer=%t api=%t web_ui=%t",
		srv.Addr,
		appCtx.Config.Modules.Downloader.Enabled,
		appCtx.Config.Modules.Aggregator.Enabled,
		appCtx.Config.Modules.UsenetIndexer.Enabled,
		appCtx.Config.Modules.API.Enabled,
		appCtx.Config.Modules.WebUI.Enabled,
	)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		appCtx.Logger.Info("shutdown signal received, stopping HTTP server")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			appCtx.Logger.Error("graceful shutdown failed: %v", err)
		}

	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			appCtx.Logger.Error("server exited with error: %v", err)
		}
	}

	appCtx.Logger.Info("finalizing application resources")
	appCtx.Close()
	appCtx.Logger.Info("server shutdown complete")
}
