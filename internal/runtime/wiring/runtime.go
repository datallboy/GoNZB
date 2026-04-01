package wiring

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/app"
)

// Central runtime assembly entrypoint so cmd/main stays thin
func BuildInitialRuntime(appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}

	if err := BootstrapStores(appCtx); err != nil {
		return err
	}

	if err := LoadAndApplyEffectiveConfig(context.Background(), appCtx); err != nil {
		return err
	}

	if err := BuildArrNotifier(context.Background(), appCtx); err != nil {
		return fmt.Errorf("build arr notifier: %w", err)
	}

	if err := BuildDownloader(appCtx); err != nil {
		return err
	}

	if err := BuildUsenetIndexer(appCtx); err != nil {
		return err
	}

	return nil
}

// StartServerBackgroundLoops starts only the long-running loops that should
// exist in API/server mode after runtime validation has passed.
func StartServerBackgroundLoops(ctx context.Context, appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}

	if appCtx.SettingsStore != nil {
		appCtx.Logger.Info("starting runtime settings watcher")
		go WatchSettings(ctx, appCtx)
	}

	if appCtx.Config.Modules.Downloader.Enabled {
		if appCtx.Queue == nil {
			return fmt.Errorf("downloader module is enabled but queue manager is not initialized")
		}

		appCtx.Logger.Info("starting downloader queue manager")
		go appCtx.Queue.Start(ctx)
	}

	return nil
}
