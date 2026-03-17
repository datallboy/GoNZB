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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if appCtx.SettingsStore != nil {
		go wiring.WatchSettings(ctx, appCtx)
	}

	if appCtx.Queue != nil {
		go appCtx.Queue.Start(ctx)
	}

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
