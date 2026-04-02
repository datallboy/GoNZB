package wiring

import (
	"context"
	"fmt"
	"io"

	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/indexing/supervisor"
)

type indexerRuntimeState struct {
	cancel func()
	closer io.Closer
}

// Long-running scrape mode restart loop lives outside cmd/main.
func RunIndexerScrapeScheduler(ctx context.Context, appCtx *app.Context) error {
	return runIndexerStages(ctx, appCtx, supervisor.StageScrapeLatest)
}

func runIndexerStages(ctx context.Context, appCtx *app.Context, stages ...supervisor.StageName) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if !appCtx.Config.Modules.UsenetIndexer.Enabled {
		return fmt.Errorf("usenet_indexer module is disabled")
	}

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	var state indexerRuntimeState

	if err := startIndexerStageRuntime(runCtx, appCtx, &state, stages...); err != nil {
		return err
	}
	defer stopIndexerStageRuntime(appCtx, &state)

	if appCtx.SettingsStore == nil {
		<-ctx.Done()
		return nil
	}

	ch, err := appCtx.SettingsStore.WatchSettingsChanges(ctx)
	if err != nil {
		return fmt.Errorf("failed to start settings watcher: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			return nil

		case _, ok := <-ch:
			if !ok {
				return nil
			}

			if err := LoadAndApplyEffectiveConfig(ctx, appCtx); err != nil {
				appCtx.Logger.Error("failed to apply runtime settings update: %v", err)
				continue
			}

			stopIndexerStageRuntime(appCtx, &state)
			if err := startIndexerStageRuntime(runCtx, appCtx, &state, stages...); err != nil {
				appCtx.Logger.Error("failed to rebuild indexer stage runtime: %v", err)
				continue
			}

			appCtx.Logger.Info("Applied runtime settings update to indexer stage runtime")
		}
	}
}

func startIndexerStageRuntime(parent context.Context, appCtx *app.Context, state *indexerRuntimeState, stages ...supervisor.StageName) error {
	rt, err := buildUsenetIndexerRuntime(appCtx)
	if err != nil {
		return err
	}
	if rt.service == nil || rt.supervisor == nil {
		return fmt.Errorf("usenet indexer runtime is not configured")
	}

	appCtx.UsenetIndexer = rt.service

	childCtx, childCancel := context.WithCancel(parent)
	state.cancel = childCancel
	state.closer = rt.scrapeProvider

	go func() {
		if err := rt.supervisor.RunSelected(childCtx, stages...); err != nil && childCtx.Err() == nil {
			appCtx.Logger.Error("indexer stage runtime failed: %v", err)
		}
	}()

	return nil
}

func stopIndexerStageRuntime(appCtx *app.Context, state *indexerRuntimeState) {
	if state == nil {
		return
	}

	if state.cancel != nil {
		state.cancel()
		state.cancel = nil
	}
	if state.closer != nil {
		if err := state.closer.Close(); err != nil && appCtx != nil {
			appCtx.Logger.Warn("failed to close previous scrape provider: %v", err)
		}
		state.closer = nil
	}
}
