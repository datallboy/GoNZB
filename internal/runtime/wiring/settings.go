package wiring

import (
	"context"
	"fmt"

	"github.com/datallboy/gonzb/internal/app"
)

// Server-mode settings watcher moved out of cmd/main.
func WatchSettings(ctx context.Context, appCtx *app.Context) {
	ch, err := appCtx.SettingsStore.WatchSettingsChanges(ctx)
	if err != nil {
		appCtx.Logger.Error("Failed to start settings watcher: %v", err)
		return
	}

	for {
		select {
		case <-ctx.Done():
			return

		case _, ok := <-ch:
			if !ok {
				return
			}

			if err := LoadAndApplyEffectiveConfig(ctx, appCtx); err != nil {
				appCtx.Logger.Error("Failed to apply runtime settings update: %v", err)
				continue
			}

			for _, module := range appCtx.RuntimeModules() {
				if err := module.Reload(ctx); err != nil {
					appCtx.Logger.Warn("Runtime settings applied, but %s reload failed: %v", module.Name(), err)
					continue
				}
			}

			BindApplicationModules(appCtx)
			appCtx.Logger.Info("Applied runtime settings update")

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
