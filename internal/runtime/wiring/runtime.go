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

	registerRuntimeModules(appCtx)
	for _, module := range appCtx.RuntimeModules() {
		if err := module.Build(context.Background()); err != nil {
			return fmt.Errorf("build %s module: %w", module.Name(), err)
		}
	}

	BindApplicationModules(appCtx)

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

	for _, module := range appCtx.RuntimeModules() {
		if err := module.Start(ctx); err != nil {
			return fmt.Errorf("start %s module: %w", module.Name(), err)
		}
	}

	return nil
}
