package wiring

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/engine"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/nntp"
	"github.com/datallboy/gonzb/internal/nzb"
	"github.com/datallboy/gonzb/internal/processor"
)

var errDownloaderReloadDeferred = errors.New("downloader runtime reload deferred until queue is idle")

func IsDownloaderReloadDeferred(err error) bool {
	return errors.Is(err, errDownloaderReloadDeferred)
}

// Build downloader-owned runtime from app context config.
func BuildDownloader(appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if !appCtx.Config.Modules.Downloader.Enabled {
		appCtx.NNTP = nil
		appCtx.Processor = nil
		appCtx.Downloader = nil
		appCtx.NZBParser = nil
		return nil
	}

	writer := engine.NewFileWriter()

	downloadCtx := appCtx
	if servers := scopedDownloaderServers(appCtx); len(servers) > 0 {
		cfg := *appCtx.Config
		cfg.Servers = servers
		copied := *appCtx
		copied.Config = &cfg
		downloadCtx = &copied
	}

	runtime := downloaderRuntimeSettings(appCtx)
	manager, err := nntp.NewManagerWithOptions(downloadCtx, managerOptionsFromRuntime(runtime, nntp.CapacityReturnBusy))
	if err != nil {
		return fmt.Errorf("provider initialization failed: %w", err)
	}

	appCtx.NNTP = manager
	appCtx.Processor = processor.New(appCtx, writer)
	appCtx.Downloader = engine.NewDownloader(appCtx, writer)
	appCtx.NZBParser = nzb.NewParser()

	return nil
}

func scopedDownloaderServers(appCtx *app.Context) []config.ServerConfig {
	runtime := downloaderRuntimeSettings(appCtx)
	if runtime == nil {
		return nil
	}
	return app.ToConfigServers(app.DownloaderNNTPServers(runtime))
}

func downloaderRuntimeSettings(appCtx *app.Context) *app.RuntimeSettings {
	if appCtx == nil || appCtx.SettingsStore == nil {
		return nil
	}
	runtime, err := appCtx.SettingsStore.GetRuntimeSettings(context.Background(), appCtx.BootstrapConfig)
	if err != nil {
		appCtx.Logger.Warn("Failed to load downloader NNTP runtime settings: %v", err)
		return nil
	}
	return runtime
}

func managerOptionsFromRuntime(runtime *app.RuntimeSettings, policy nntp.CapacityPolicy) nntp.ManagerOptions {
	pool := app.DefaultNNTPPoolRuntimeSettings()
	if runtime != nil && runtime.NNTPPool != nil {
		pool = runtime.NNTPPool
	}
	return nntp.ManagerOptions{
		CapacityPolicy:            policy,
		ModuleReservationsEnabled: true,
		IdleBorrowEnabled:         pool.IdleBorrowEnabled,
		IndexerMaxPercent:         pool.IndexerMaxPercent,
		DownloaderReservePercent:  pool.DownloaderReservePercent,
		DownloaderDemandWindow:    time.Duration(pool.DemandWindowSeconds) * time.Second,
	}
}

// Safely reload downloader runtime when queue is idle.
func ReloadDownloaderIfIdle(appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if !appCtx.Config.Modules.Downloader.Enabled {
		return nil
	}
	if appCtx.Queue == nil {
		return nil
	}
	if active := appCtx.Queue.GetActiveItem(); active != nil {
		return errDownloaderReloadDeferred
	}

	oldNNTP := appCtx.NNTP

	if err := BuildDownloader(appCtx); err != nil {
		return err
	}

	appCtx.Queue.ReloadRuntime(appCtx)

	if oldNNTP != nil {
		if err := oldNNTP.Close(); err != nil {
			appCtx.Logger.Warn("Failed to close previous NNTP manager: %v", err)
		}
	}

	return nil
}
