package wiring

import (
	"context"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/app"
)

// Server-mode settings watcher moved out of cmd/main.
func WatchSettings(ctx context.Context, appCtx *app.Context) {
	ch, err := appCtx.SettingsStore.WatchSettingsChanges(ctx)
	if err != nil {
		appCtx.Logger.Error("Failed to start settings watcher: %v", err)
		return
	}

	retryTicker := time.NewTicker(2 * time.Second)
	defer retryTicker.Stop()

	pendingDownloaderReload := false

	for {
		select {
		case <-ctx.Done():
			return

		case _, ok := <-ch:
			if !ok {
				return
			}

			if err := app.LoadAndApplyEffectiveConfig(ctx, appCtx); err != nil {
				appCtx.Logger.Error("Failed to apply runtime settings update: %v", err)
				continue
			}

			if err := ReloadDownloaderIfIdle(appCtx); err != nil {
				if IsDownloaderReloadDeferred(err) {
					pendingDownloaderReload = true
					appCtx.Logger.Warn("Runtime settings applied; downloader runtime reload deferred until queue is idle")
				} else {
					appCtx.Logger.Warn("Runtime settings applied, but downloader runtime reload failed: %v", err)
				}
			} else {
				pendingDownloaderReload = false
			}

			appCtx.Logger.Info("Applied runtime settings update")

		case <-retryTicker.C:
			if !pendingDownloaderReload {
				continue
			}

			if err := ReloadDownloaderIfIdle(appCtx); err != nil {
				if !IsDownloaderReloadDeferred(err) {
					appCtx.Logger.Warn("Deferred downloader runtime reload failed: %v", err)
				}
				continue
			}

			pendingDownloaderReload = false
			appCtx.Logger.Info("Applied deferred downloader runtime reload")
		}
	}
}

// Optional helper if you want a shared error string in callers later.
func ValidateRuntimeForServer(appCtx *app.Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if !appCtx.Config.Modules.API.Enabled && !appCtx.Config.Modules.WebUI.Enabled {
		return fmt.Errorf("serve requires modules.api.enabled or modules.web_ui.enabled")
	}
	return nil
}
