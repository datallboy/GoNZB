package app

import (
	"context"
	"fmt"

	aggregatorpkg "github.com/datallboy/gonzb/internal/aggregator"
	"github.com/datallboy/gonzb/internal/aggregator/sources/newznab"
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

// CHANGED: load effective config from SQLite settings overlay and apply
// safe in-memory config/runtime swaps that do not create package cycles.
func LoadAndApplyEffectiveConfig(ctx context.Context, appCtx *Context) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if appCtx.SettingsStore == nil || appCtx.BootstrapConfig == nil {
		return nil
	}

	effective, err := appCtx.SettingsStore.LoadEffectiveSettings(ctx, appCtx.BootstrapConfig)
	if err != nil {
		return fmt.Errorf("load effective settings: %w", err)
	}

	return ApplyEffectiveConfig(appCtx, effective)
}

// CHANGED: safe runtime application for effective config.
// This only rebuilds Aggregator + Resolver from effective config.
// Downloader and Usenet/NZB Indexer runtime-specific rebuilds stay in cmd wiring.
func ApplyEffectiveConfig(appCtx *Context, effective *config.Config) error {
	if appCtx == nil {
		return fmt.Errorf("app context is required")
	}
	if effective == nil {
		return fmt.Errorf("effective config is nil")
	}

	appCtx.Config = effective

	var aggregator IndexerAggregator
	if effective.Modules.Aggregator.Enabled {
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

		aggregator = manager
	}

	appCtx.Aggregator = aggregator
	appCtx.Resolver = resolver.NewDefaultReleaseResolver(
		resolver.NewManualResolver(appCtx.PayloadCacheStore),
		resolver.NewAggregatorResolver(aggregator),
		resolver.NewUsenetIndexResolver(appCtx.PGIndexStore),
	)
	appCtx.PayloadFetcher = appCtx.Resolver

	return nil
}
