package wiring

import (
	"context"
	"fmt"

	aggregatorpkg "github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/aggregator/sources/newznab"
	"github.com/datallboy/gonzb/internal/app"
	"github.com/datallboy/gonzb/internal/domain"
	"github.com/datallboy/gonzb/internal/infra/config"
	"github.com/datallboy/gonzb/internal/resolver"
	"github.com/datallboy/gonzb/internal/store/adapters"
)

type aggregatorCacheStore interface {
	UpsertAggregatorReleaseCache(ctx context.Context, releases []*domain.Release) error
	SearchAggregatorReleaseCache(ctx context.Context, query string, limit int) ([]*domain.Release, error)
	GetAggregatorReleaseCacheByID(ctx context.Context, id string) (*domain.Release, error)
}

func LoadAndApplyEffectiveConfig(ctx context.Context, appCtx *app.Context) error {
	effective, err := LoadEffectiveConfig(ctx, appCtx)
	if err != nil {
		return err
	}
	return ApplyEffectiveConfig(appCtx, effective)
}

func LoadEffectiveConfig(ctx context.Context, appCtx *app.Context) (*config.Config, error) {
	if appCtx == nil {
		return nil, fmt.Errorf("app context is required")
	}
	if appCtx.BootstrapConfig == nil {
		return nil, fmt.Errorf("bootstrap config is required")
	}
	if appCtx.SettingsStore == nil {
		return appCtx.BootstrapConfig, nil
	}

	effective, err := appCtx.SettingsStore.LoadEffectiveSettings(ctx, appCtx.BootstrapConfig)
	if err != nil {
		return nil, fmt.Errorf("load effective settings: %w", err)
	}
	if effective == nil {
		return appCtx.BootstrapConfig, nil
	}

	return effective, nil
}

func ApplyEffectiveConfig(appCtx *app.Context, effective *config.Config) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if effective == nil {
		return fmt.Errorf("effective config is nil")
	}

	appCtx.Config = effective

	aggregator := buildAggregator(appCtx, effective)
	releaseResolver := buildReleaseResolver(appCtx, aggregator)

	appCtx.Aggregator = aggregator
	appCtx.Resolver = releaseResolver
	appCtx.PayloadFetcher = releaseResolver

	return nil
}

func buildAggregator(appCtx *app.Context, effective *config.Config) app.IndexerAggregator {
	if appCtx == nil || effective == nil || !effective.Modules.Aggregator.Enabled {
		return nil
	}

	var cacheStore aggregatorCacheStore
	if appCtx.JobStore != nil {
		if cache, ok := appCtx.JobStore.(aggregatorCacheStore); ok {
			cacheStore = cache
		}
	}

	aggregatorStore := adapters.NewAggregatorStore(appCtx.PayloadCacheStore, cacheStore)
	manager := aggregatorpkg.NewManager(
		aggregatorStore,
		appCtx.Logger,
		effective.Store.PayloadCacheEnabled,
		effective.Store.SearchPersistenceEnabled && appCtx.JobStore != nil,
	)

	for _, idxCfg := range effective.Indexers {
		client := newznab.New(idxCfg.ID, idxCfg.BaseUrl, idxCfg.ApiPath, idxCfg.ApiKey, idxCfg.Redirect)
		manager.AddSource(client)
	}

	return manager
}

func buildReleaseResolver(appCtx *app.Context, aggregator app.IndexerAggregator) app.ReleaseResolver {
	return resolver.NewDefaultReleaseResolver(
		resolver.NewManualResolver(appCtx.PayloadCacheStore),
		resolver.NewAggregatorResolver(aggregator),
		resolver.NewUsenetIndexResolver(appCtx.PGIndexStore),
	)
}
